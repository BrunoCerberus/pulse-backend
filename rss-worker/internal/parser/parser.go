// Package parser provides RSS/Atom feed parsing and article enrichment.
//
// The parser uses gofeed for feed parsing and enriches articles with:
//   - og:image extraction for high-resolution header images (5 concurrent workers)
//   - Content extraction via go-readability for full article text (3 concurrent workers)
//
// The enrichment is performed in parallel with worker pools to balance
// throughput against server load.
package parser

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/pulsefeed/rss-worker/internal/httputil"
	"github.com/pulsefeed/rss-worker/internal/logger"
	"github.com/pulsefeed/rss-worker/internal/models"
)

// Parser handles RSS/Atom feed parsing and article enrichment.
// It combines gofeed for parsing with custom extractors for images and content.
type Parser struct {
	// fp is set per ParseFeed call so concurrent goroutines don't share the
	// gofeed.Parser's lazily-initialized translator fields. The shared HTTP
	// client is reused via the fpClient field.
	fpClient         *http.Client
	ogExtractor      *OGImageExtractor // Extracts og:image from article pages
	contentExtractor *ContentExtractor // Extracts article text via readability
}

// DefaultHostRPS and DefaultHostBurst are the initial per-host rate limits.
// Override via SetHostRateLimit before constructing parsers/extractors.
const (
	DefaultHostRPS   = 2.0
	DefaultHostBurst = 5

	// MaxFeedBodyBytes caps the RSS feed response body to prevent a hostile
	// publisher from streaming gigabytes of XML and exhausting memory. 50 MB
	// is generous — real feeds top out below 1 MB.
	MaxFeedBodyBytes = 50 << 20

	// Per-field length caps (in runes). Anything longer is silently
	// truncated. Caps the DB-bloat amplification factor a single hostile
	// item can produce.
	maxTitleLen   = 500
	maxSummaryLen = 4096
	maxContentLen = 200_000
	maxAuthorLen  = 200
	maxURLLen     = 2048
)

// Package-level rate limits used by all parser/extractor HTTP clients built
// after init. Callers should SetHostRateLimit once at startup (before any
// goroutine spawns) to apply env-driven config. Plain vars are safe because
// the write happens-before any concurrent reads.
var (
	hostRPS   = float64(DefaultHostRPS)
	hostBurst = DefaultHostBurst
)

// SetHostRateLimit overrides the per-host RPS and burst used by subsequent
// parser.New, NewOGImageExtractor, and NewContentExtractor calls.
// Must be called before any concurrent use of the package.
func SetHostRateLimit(rps float64, burst int) {
	hostRPS = rps
	hostBurst = burst
}

// EffectiveContentCap returns the effective per-article content cap (in runes)
// given an optional per-source override. A nil or non-positive perSource value
// falls through to the global maxContentLen ceiling. A positive perSource is
// clamped to MIN(perSource, maxContentLen) so a misconfigured large value
// (e.g. a typo'd UPDATE setting a source to 50_000_000) cannot escape the
// global ceiling that defends against memory exhaustion from a hostile feed.
//
// Exported so the backfill path (main.processContentBackfill) can apply the
// same clamp the initial-parse path applies; sharing this function keeps the
// two cutoffs from drifting.
func EffectiveContentCap(perSource *int) int {
	if perSource == nil || *perSource <= 0 {
		return maxContentLen
	}
	if *perSource < maxContentLen {
		return *perSource
	}
	return maxContentLen
}

// SanitizeContent is the exported entry point used by callers outside the
// parser package (notably main.processContentBackfill) so backfill writes
// receive the same control-char / bidi-codepoint stripping + rune-count
// truncation as the initial-parse writes. Pass maxRunes = 0 to skip
// truncation. Param name matches sanitizeText's `max` convention and avoids
// shadowing the Go builtin cap().
func SanitizeContent(s string, maxRunes int) string {
	return sanitizeText(s, maxRunes)
}

// New creates a new Parser. Uses the current package rate-limit settings;
// call SetHostRateLimit first to override defaults.
func New() *Parser {
	return &Parser{
		fpClient:         httputil.NewRateLimitedClient(30*time.Second, hostRPS, hostBurst, 5),
		ogExtractor:      NewOGImageExtractor(),
		contentExtractor: NewContentExtractor(),
	}
}

// newGofeedParser returns a gofeed.Parser with all translator fields
// pre-populated to avoid the lazy-init data race when the parser is shared
// across goroutines. Each ParseFeed call instantiates its own gofeed.Parser
// since gofeed's internal state is not safe for concurrent use beyond this.
func newGofeedParser(client *http.Client) *gofeed.Parser {
	fp := gofeed.NewParser()
	fp.AtomTranslator = &gofeed.DefaultAtomTranslator{}
	fp.RSSTranslator = &gofeed.DefaultRSSTranslator{}
	fp.JSONTranslator = &gofeed.DefaultJSONTranslator{}
	fp.Client = client
	return fp
}

// ParseResult carries the articles extracted from a feed plus the conditional-GET
// validators the worker should persist for the next fetch. NotModified is true
// when the origin returned 304 — in that case Articles is empty and ETag /
// LastModified hold the validator to keep sending (the parser falls back to the
// source's existing validator if the 304 response doesn't echo one).
type ParseResult struct {
	Articles     []*models.Article
	ETag         string
	LastModified string
	NotModified  bool
}

// ParseFeed fetches and parses an RSS feed with conditional-GET support.
//
// Unlike gofeed's built-in ParseURLWithContext, this does the HTTP call
// explicitly so it can:
//   - attach If-None-Match / If-Modified-Since from the source's stored
//     validators, and
//   - inspect the status code to short-circuit on 304 Not Modified, returning
//     NotModified=true without touching the body, and
//   - wrap the body in io.LimitReader so a hostile feed can't exhaust memory
//     by streaming gigabytes of XML inside the request timeout.
//
// The shared rate-limited client (set in New()) is reused so per-host
// throttling still applies. A fresh gofeed.Parser is allocated per call to
// avoid the lazy-translator data race.
func (p *Parser) ParseFeed(ctx context.Context, source models.Source) (*ParseResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.FeedURL, nil)
	if err != nil {
		return nil, err
	}
	if source.ETag != nil && *source.ETag != "" {
		req.Header.Set("If-None-Match", *source.ETag)
	}
	if source.LastModified != nil && *source.LastModified != "" {
		req.Header.Set("If-Modified-Since", *source.LastModified)
	}
	// Some publishers reject no-User-Agent requests; match what gofeed
	// would have sent by default.
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "pulse-rss-worker/1.0 (+gofeed)")
	}

	resp, err := p.fpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		// RFC 7232: the server MAY omit ETag/Last-Modified on 304. When it
		// does, preserve what we sent so the next run still benefits from
		// the conditional GET.
		etag := resp.Header.Get("ETag")
		if etag == "" && source.ETag != nil {
			etag = *source.ETag
		}
		lm := resp.Header.Get("Last-Modified")
		if lm == "" && source.LastModified != nil {
			lm = *source.LastModified
		}
		return &ParseResult{NotModified: true, ETag: etag, LastModified: lm}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("feed fetch failed: %s", resp.Status)
	}

	// Cap body size to defend against memory exhaustion from a hostile feed.
	body := io.LimitReader(resp.Body, MaxFeedBodyBytes)
	fp := newGofeedParser(p.fpClient)
	feed, err := fp.Parse(body)
	if err != nil {
		return nil, err
	}

	// Cap is invariant per ParseFeed call — computed once and threaded into
	// both the per-item path (itemToArticle) and the enrichment workers.
	contentCap := EffectiveContentCap(source.MaxContentLength)

	articles := make([]*models.Article, 0, len(feed.Items))

	for _, item := range feed.Items {
		article := p.itemToArticle(item, source, contentCap)
		if article != nil {
			articles = append(articles, article)
		}
	}

	// Fetch og:images in parallel for high-resolution header images
	p.enrichWithOGImages(ctx, articles)

	// Extract content for articles that don't have it from RSS.
	p.enrichWithContent(ctx, articles, contentCap)

	return &ParseResult{
		Articles:     articles,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}

// enrichStats holds success/failure counts from an enrichment pass.
type enrichStats struct {
	Success int
	Failed  int
}

// enrichWithOGImages fetches og:image for each article in parallel.
// Uses a worker pool to avoid overwhelming servers.
func (p *Parser) enrichWithOGImages(ctx context.Context, articles []*models.Article) enrichStats {
	logger.Infof("[OG] Starting og:image enrichment for %d articles", len(articles))

	if len(articles) == 0 {
		logger.Debugf("[OG] No articles to process")
		return enrichStats{}
	}

	const maxWorkers = 5
	numWorkers := min(maxWorkers, len(articles))

	work := make(chan *models.Article, maxWorkers*2)
	var wg sync.WaitGroup
	var success, failed atomic.Int32

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range work {
				select {
				case <-ctx.Done():
					return
				default:
					before := article.ImageURL
					p.fetchOGImageForArticle(ctx, article)
					if article.ImageURL != before {
						success.Add(1)
					} else {
						failed.Add(1)
					}
				}
			}
		}()
	}

	for _, article := range articles {
		select {
		case work <- article:
		case <-ctx.Done():
		}
	}
	close(work)

	wg.Wait()
	stats := enrichStats{Success: int(success.Load()), Failed: int(failed.Load())}
	logger.Infof("[OG] Completed og:image enrichment: success=%d, failed=%d", stats.Success, stats.Failed)
	return stats
}

// fetchOGImageForArticle fetches the og:image for a single article
func (p *Parser) fetchOGImageForArticle(ctx context.Context, article *models.Article) {
	ogImage, err := p.ogExtractor.ExtractOGImage(ctx, article.URL)
	if err != nil {
		logger.Debugf("[OG] ERROR fetching %s: %v", article.URL, err)
		return
	}

	if ogImage != "" {
		// Only update if og:image is different from the RSS thumbnail
		if article.ThumbnailURL == nil || ogImage != *article.ThumbnailURL {
			article.ImageURL = &ogImage
			logger.Debugf("[OG] SUCCESS %s -> %s", article.URL, ogImage)
		} else {
			logger.Debugf("[OG] SAME as thumbnail for %s", article.URL)
		}
	} else {
		logger.Debugf("[OG] NOT FOUND for %s", article.URL)
	}
}

// enrichWithContent extracts full article content for articles that don't have it.
// contentCap is the effective per-source content length cap (runes) applied
// after extraction; passed in from ParseFeed so all workers use the same value.
func (p *Parser) enrichWithContent(ctx context.Context, articles []*models.Article, contentCap int) enrichStats {
	var needsContent []*models.Article
	for _, article := range articles {
		if article.Content == nil || *article.Content == "" {
			needsContent = append(needsContent, article)
		}
	}

	if len(needsContent) == 0 {
		logger.Debugf("[CONTENT] All articles have content from RSS")
		return enrichStats{}
	}

	logger.Infof("[CONTENT] Starting content extraction for %d articles", len(needsContent))

	const maxWorkers = 3 // Lower than og:image since content extraction is heavier
	numWorkers := min(maxWorkers, len(needsContent))

	work := make(chan *models.Article, maxWorkers*2)
	var wg sync.WaitGroup
	var success, failed atomic.Int32

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range work {
				select {
				case <-ctx.Done():
					return
				default:
					before := article.Content
					p.fetchContentForArticle(ctx, article, contentCap)
					if article.Content != before {
						success.Add(1)
					} else {
						failed.Add(1)
					}
				}
			}
		}()
	}

	for _, article := range needsContent {
		select {
		case work <- article:
		case <-ctx.Done():
		}
	}
	close(work)

	wg.Wait()
	stats := enrichStats{Success: int(success.Load()), Failed: int(failed.Load())}
	logger.Infof("[CONTENT] Completed content extraction: success=%d, failed=%d", stats.Success, stats.Failed)
	return stats
}

// fetchContentForArticle extracts content for a single article. contentCap is
// the per-source-aware rune cap (already clamped to the global ceiling by
// EffectiveContentCap upstream).
func (p *Parser) fetchContentForArticle(ctx context.Context, article *models.Article, contentCap int) {
	content, err := p.contentExtractor.ExtractTextContent(ctx, article.URL)
	if err != nil {
		logger.Debugf("[CONTENT] ERROR fetching %s: %v", article.URL, err)
		return
	}

	if content != "" {
		content = sanitizeText(content, contentCap)
		article.Content = &content
		logger.Debugf("[CONTENT] SUCCESS %s (%d chars)", article.URL, len(content))
	} else {
		logger.Debugf("[CONTENT] NOT FOUND for %s", article.URL)
	}
}

// itemToArticle converts a gofeed.Item to our Article model.
// Returns nil if the item is missing a required field or has an unsafe URL.
// contentCap is the per-source-aware content rune ceiling, hoisted out of
// the per-item loop in ParseFeed since it's invariant across items.
func (p *Parser) itemToArticle(item *gofeed.Item, source models.Source, contentCap int) *models.Article {
	if item.Title == "" {
		return nil
	}
	link := strings.TrimSpace(item.Link)
	if link == "" || !isSafeArticleURL(link) {
		return nil
	}
	link = canonicalizeURL(link)
	if len(link) > maxURLLen {
		return nil
	}

	article := models.NewArticle(
		sanitizeText(strings.TrimSpace(item.Title), maxTitleLen),
		link,
		source.ID,
		source.CategoryID,
		source.Language,
		clampPublishedDate(parsePublishedDate(item)),
	)

	// Set denormalized source/category fields
	article.SourceName = &source.Name
	article.SourceSlug = &source.Slug
	if catName := source.CategoryName(); catName != "" {
		article.CategoryName = &catName
	}
	if catSlug := source.CategorySlug(); catSlug != "" {
		article.CategorySlug = &catSlug
	}

	// Summary/Description (truncated to first paragraph)
	if item.Description != "" {
		desc := cleanHTML(item.Description)
		desc = truncateToFirstParagraph(desc)
		desc = sanitizeText(desc, maxSummaryLen)
		article.Summary = &desc
	}

	// Content (if available) — clamped via the hoisted contentCap.
	if item.Content != "" {
		content := cleanHTML(item.Content)
		content = sanitizeText(content, contentCap)
		article.Content = &content
	}

	article.Author = extractAuthor(item)

	// Image: Use RSS image as thumbnail, og:image will be fetched later for full-size
	thumbnailURL := extractImageURL(item)
	if thumbnailURL != "" && isSafeMediaURL(thumbnailURL) {
		article.ThumbnailURL = &thumbnailURL
		// Also set ImageURL as fallback in case og:image fetch fails
		article.ImageURL = &thumbnailURL
	}

	extractMediaInfo(item, article)

	return article
}

// isSafeArticleURL accepts only http/https URLs with a non-empty host, no
// embedded userinfo, no control/bidi-override codepoints, and no forbidden or
// obfuscated IP-literal host. This is a *storage* guard: unlike the worker's
// own fetch (protected at the dial layer by httputil.SafeTransport, which
// re-resolves and rejects forbidden IPs), these URLs are persisted and shipped
// to iOS clients, which then dereference them. So the same value must be safe
// to hand a client, not just safe for us to fetch.
//
// It is shared by every publisher-supplied URL sink — article link
// (itemToArticle), thumbnail / RSS-image / media (isSafeMediaURL), and og:image
// (isAcceptableOGImage) — so all four enforce one consistent policy.
func isSafeArticleURL(raw string) bool {
	if urlHasUnsafeRune(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	// Reject embedded userinfo: https://www.reuters.com@evil.example/ parses
	// with Host=evil.example but renders trusted-looking — a phishing / host
	// spoof once stored and shown to iOS.
	if u.User != nil {
		return false
	}
	// Reject IP-literal hosts in forbidden ranges, plus any obfuscated
	// (decimal / hex / octal) IPv4 literal a client resolver would accept —
	// a client-side SSRF / internal-network-probe vector once served to iOS.
	if hostIsForbiddenIPLiteral(u.Hostname()) {
		return false
	}
	return true
}

// hostIsForbiddenIPLiteral reports whether host (a url.Hostname() result) is an
// IP literal an SSRF-aware storage guard must refuse. It catches two cases the
// bare scheme/host lexical check misses:
//   - a canonical IP literal (dotted-quad IPv4 or IPv6) in a forbidden range
//     (httputil.IsForbiddenIP); and
//   - an obfuscated inet_aton-style IPv4 literal (decimal "2852039166", hex
//     "0x7f000001", octal "0177.0.0.1", short "a.b.c" forms) that net.ParseIP
//     does NOT recognize but iOS's getaddrinfo will. These are never legitimate
//     in a publisher URL, so any such literal is rejected outright.
func hostIsForbiddenIPLiteral(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return httputil.IsForbiddenIP(ip)
	}
	return obfuscatedIPv4(host) != nil
}

// obfuscatedIPv4 decodes host as a legacy inet_aton-style IPv4 literal, or
// returns nil if host is not one (e.g. a real DNS name, where at least one
// label fails to parse as a numeric part). net.ParseIP already handles the
// canonical dotted-quad form, so this only needs to cover the 1–4 part
// decimal/hex/octal encodings it rejects.
func obfuscatedIPv4(host string) net.IP {
	if host == "" {
		return nil
	}
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return nil
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := parseIPv4Part(p)
		if !ok {
			return nil
		}
		nums[i] = n
	}
	// Accumulate in a uint64 (each term is range-checked first, so the value
	// never exceeds 32 bits) and split into octets with explicit & 0xFF masks.
	// Staying in uint64 + masking avoids narrowing conversions that would
	// otherwise trip gosec G115 (integer-overflow); the masks make each octet
	// provably in range.
	var addr uint64
	switch len(parts) {
	case 1: // a — whole 32-bit value
		if nums[0] > 0xFFFFFFFF {
			return nil
		}
		addr = nums[0]
	case 2: // a.b — a(8) . b(24)
		if nums[0] > 0xFF || nums[1] > 0xFFFFFF {
			return nil
		}
		addr = nums[0]<<24 | nums[1]
	case 3: // a.b.c — a(8).b(8).c(16)
		if nums[0] > 0xFF || nums[1] > 0xFF || nums[2] > 0xFFFF {
			return nil
		}
		addr = nums[0]<<24 | nums[1]<<16 | nums[2]
	default: // a.b.c.d
		for _, n := range nums {
			if n > 0xFF {
				return nil
			}
		}
		addr = nums[0]<<24 | nums[1]<<16 | nums[2]<<8 | nums[3]
	}
	return net.IPv4(
		byte((addr>>24)&0xFF),
		byte((addr>>16)&0xFF),
		byte((addr>>8)&0xFF),
		byte(addr&0xFF),
	)
}

// parseIPv4Part parses one component of an inet_aton-style IPv4 literal,
// honoring the C base prefixes inet_aton accepts: "0x"/"0X" → hex, a leading
// "0" → octal, otherwise decimal. Returns (value, true) on success.
func parseIPv4Part(p string) (uint64, bool) {
	if p == "" {
		return 0, false
	}
	base := 10
	s := p
	switch {
	case len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X'):
		base, s = 16, s[2:]
	case len(s) >= 2 && s[0] == '0':
		base, s = 8, s[1:]
	}
	if s == "" { // bare "0x" / "0X" — treat the value as zero
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// isSafeMediaURL is isSafeArticleURL plus a length cap. Thumbnail / enclosure
// URLs are stored verbatim and served to iOS, so — unlike the article link,
// which is canonicalized and length-checked in itemToArticle — they need their
// own maxURLLen ceiling here. Stops `javascript:`/`data:` URLs and oversized /
// control-char-bearing publisher URLs from propagating to clients.
func isSafeMediaURL(raw string) bool {
	return len(raw) <= maxURLLen && isSafeArticleURL(raw)
}

// urlHasUnsafeRune reports whether a stored URL contains a control character
// (C0/C1, including tab/newline which are illegal in URLs and enable header /
// URL spoofing) or a Unicode bidi-override codepoint (which can visually spoof
// the rendered URL on iOS). title/summary/content/author get the equivalent
// stripping via sanitizeText; URL fields, which must not be mutated, are
// rejected outright instead.
func urlHasUnsafeRune(raw string) bool {
	for _, r := range raw {
		if r == '\t' || r == '\n' || isControlOrBidi(r) {
			return true
		}
	}
	return false
}

// canonicalizeURL strips the fragment, lowercases scheme/host, and sorts the
// query so semantically-equivalent URLs hash to the same SHA-256. Returns the
// input untouched on parse error so dedup never silently widens.
func canonicalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Host != "" {
		u.Host = strings.ToLower(u.Host)
	}
	if u.RawQuery != "" {
		u.RawQuery = canonicalizeRawQuery(u.RawQuery)
	}
	return u.String()
}

// canonicalizeRawQuery sorts the &-separated parameters of a raw query string
// so semantically-equivalent URLs hash identically — WITHOUT the lossy
// url.Values round-trip. url.Values.Encode (via url.Query) silently DROPS the
// entire query when it contains a ';' (Go 1.17+ rejects ';' as a separator),
// which collapsed distinct articles like `?id=1;ref=x` and `?id=2;ref=x` to
// one url_hash and silently suppressed all but the first. Sorting the raw
// segments preserves every byte (including ';') while keeping dedup canonical.
func canonicalizeRawQuery(raw string) string {
	params := strings.Split(raw, "&")
	sort.Strings(params)
	return strings.Join(params, "&")
}

// parsePublishedDate extracts the publication date from a feed item,
// falling back to UpdatedParsed then time.Now().
func parsePublishedDate(item *gofeed.Item) time.Time {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}
	return time.Now()
}

// clampPublishedDate bounds the published_at to [10y ago, now+1h]. A
// far-future timestamp from a malicious feed would otherwise pin the article
// at the top of the feed forever; a far-past timestamp would skip cleanup.
func clampPublishedDate(t time.Time) time.Time {
	now := time.Now()
	earliest := now.AddDate(-10, 0, 0)
	latest := now.Add(1 * time.Hour)
	if t.Before(earliest) {
		return earliest
	}
	if t.After(latest) {
		return latest
	}
	return t
}

// extractAuthor returns the author name from a feed item, or nil if unavailable.
func extractAuthor(item *gofeed.Item) *string {
	var name string
	if item.Author != nil && item.Author.Name != "" {
		name = item.Author.Name
	} else if len(item.Authors) > 0 && item.Authors[0].Name != "" {
		name = item.Authors[0].Name
	} else {
		return nil
	}
	sanitized := sanitizeText(name, maxAuthorLen)
	if sanitized == "" {
		return nil
	}
	return &sanitized
}

// extractMediaInfo populates the article's media fields from RSS enclosures.
func extractMediaInfo(item *gofeed.Item, article *models.Article) {
	media := extractMediaEnclosure(item)
	if media == nil {
		return
	}
	if !isSafeMediaURL(media.URL) {
		return
	}
	mimeType := sanitizeMIMEType(media.MIMEType)
	if mimeType == "" {
		return
	}
	// extractMediaEnclosure already filtered to audio/* or video/* prefixes,
	// so determineMediaType can't return "" here.
	mediaType := determineMediaType(mimeType)
	article.MediaType = &mediaType
	article.MediaURL = &media.URL
	article.MediaMIMEType = &mimeType
	if media.Duration > 0 {
		article.MediaDuration = &media.Duration
	}
}

// sanitizeMIMEType returns the MIME type if it matches a tight, header-safe
// pattern; otherwise returns the empty string. Stops CRLF / extra-header
// injection from a feed-supplied enclosure type.
var mimeTypePattern = regexp.MustCompile(`^[a-zA-Z0-9!#$&^_-]+/[a-zA-Z0-9.+!#$&^_-]+$`)

func sanitizeMIMEType(s string) string {
	s = strings.TrimSpace(s)
	if !mimeTypePattern.MatchString(s) {
		return ""
	}
	return s
}

// extractImageURL tries to find an image URL from the feed item
func extractImageURL(item *gofeed.Item) string {
	// Check media content
	if item.Image != nil && item.Image.URL != "" {
		return item.Image.URL
	}

	// Check enclosures (common for images)
	for _, enc := range item.Enclosures {
		if strings.HasPrefix(enc.Type, "image/") {
			return enc.URL
		}
	}

	// Check extensions (media:content, media:thumbnail)
	if ext, ok := item.Extensions["media"]; ok {
		if content, ok := ext["content"]; ok && len(content) > 0 {
			if url, ok := content[0].Attrs["url"]; ok {
				return url
			}
		}
		if thumb, ok := ext["thumbnail"]; ok && len(thumb) > 0 {
			if url, ok := thumb[0].Attrs["url"]; ok {
				return url
			}
		}
	}

	return ""
}

// brToNewline converts block-level HTML tags to newlines BEFORE the entity
// decoder runs (so that `&lt;br&gt;` doesn't accidentally become a newline).
// Also converts `&nbsp;` to a regular space — html.UnescapeString would
// otherwise produce U+00A0 (non-breaking space) which renders inconsistently.
var brToNewline = strings.NewReplacer(
	"<br>", "\n",
	"<br/>", "\n",
	"<br />", "\n",
	"<p>", "\n",
	"</p>", "\n",
	"<BR>", "\n",
	"<BR/>", "\n",
	"<BR />", "\n",
	"&nbsp;", " ",
	"&NBSP;", " ",
)

// multiNewline collapses 3+ consecutive newlines down to 2 in a single
// regex pass — replaces the previous O(n²) loop.
var multiNewline = regexp.MustCompile(`\n{3,}`)

// removeTagWithContent strips a given HTML tag and its contents (case-insensitive).
// For example, removeTagWithContent(s, "script") removes <script>...</script> blocks.
func removeTagWithContent(s, tagName string) string {
	lower := strings.ToLower(s)
	tag := strings.ToLower(tagName)
	openTag := "<" + tag
	closeTag := "</" + tag + ">"

	var result strings.Builder
	result.Grow(len(s))
	i := 0
	for i < len(s) {
		lowerRest := lower[i:]
		idx := strings.Index(lowerRest, openTag)
		if idx == -1 {
			result.WriteString(s[i:])
			break
		}
		// Check that the open tag is followed by '>' or whitespace (not a partial match)
		afterTag := i + idx + len(openTag)
		if afterTag < len(s) && s[afterTag] != '>' && s[afterTag] != ' ' && s[afterTag] != '\t' && s[afterTag] != '\n' && s[afterTag] != '/' {
			result.WriteString(s[i : i+idx+len(openTag)])
			i = afterTag
			continue
		}
		result.WriteString(s[i : i+idx])
		// Find closing tag
		closeIdx := strings.Index(lower[i+idx:], closeTag)
		if closeIdx == -1 {
			// No closing tag found, skip to end
			break
		}
		i = i + idx + closeIdx + len(closeTag)
	}
	return result.String()
}

// cleanHTML removes HTML tags, decodes entities, and normalizes whitespace.
//
// Pipeline:
//  1. Drop <script>/<style> blocks with contents.
//  2. Convert <br>/<p> tags to newlines (before entity decode so encoded
//     `&lt;br&gt;` doesn't become a newline).
//  3. Decode entities (named, numeric, hex). Uses html.UnescapeString which
//     catches `&#x3c;`/`&#60;` that the previous Replacer missed.
//  4. Strip remaining tags via a simple state machine.
//  5. Collapse runs of newlines via a single regex pass.
func cleanHTML(s string) string {
	s = removeTagWithContent(s, "script")
	s = removeTagWithContent(s, "style")
	s = brToNewline.Replace(s)
	s = html.UnescapeString(s)
	s = stripTags(s)
	s = multiNewline.ReplaceAllString(strings.TrimSpace(s), "\n\n")
	return s
}

// stripTags removes any remaining HTML angle-bracket spans. Not a real HTML
// parser — entity decoding has already happened so `<` only appears as a tag
// start. Quoted attribute values containing `>` are still mishandled, but
// they only affect content layout, not security (entities are gone).
func stripTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeText strips control characters + bidi-override codepoints (which
// can be used for visual spoofing in iOS feed views), then truncates to max
// runes. Pass max=0 to skip truncation.
func sanitizeText(s string, max int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControlOrBidi(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if max > 0 {
		out = truncRunes(out, max)
	}
	return out
}

// isControlOrBidi reports whether r is a C0/C1 control char (except tab and
// newline, which are kept) or one of Unicode's bidirectional overrides
// (which can flip rendering order to spoof URLs in iOS rendering).
func isControlOrBidi(r rune) bool {
	switch r {
	case '\t', '\n':
		return false
	case 0x061C, // ALM (Arabic Letter Mark) — same implicit-mark family as LRM/RLM
		0x200E, 0x200F, // LRM, RLM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // bidi embedding/override
		0x2066, 0x2067, 0x2068, 0x2069: // bidi isolate
		return true
	}
	if r < 0x20 || r == 0x7f {
		return true
	}
	if r >= 0x80 && r <= 0x9f {
		return true
	}
	return false
}

// truncRunes returns the first max runes of s.
func truncRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// truncateToFirstParagraph limits text to the first paragraph ending with a period.
// This prevents summaries from being too long while ensuring complete sentences.
func truncateToFirstParagraph(s string) string {
	if s == "" {
		return s
	}

	// First check for paragraph break (double newline)
	if idx := strings.Index(s, "\n\n"); idx > 0 {
		s = s[:idx]
	}

	// Find the first sentence-ending period (followed by space or end of string)
	// We look for ". " to avoid cutting at abbreviations like "Dr." in the middle
	minLength := 30 // Minimum chars before we consider truncating at a period
	for i := minLength; i < len(s); i++ {
		if s[i] == '.' {
			// Check if this looks like end of sentence
			if i == len(s)-1 {
				// Period at end of string
				return s
			}
			if i+1 < len(s) && (s[i+1] == ' ' || s[i+1] == '\n') {
				// Period followed by space or newline - likely end of sentence
				return s[:i+1]
			}
		}
	}

	return s
}

// MediaEnclosure holds extracted media information from RSS enclosures
type MediaEnclosure struct {
	URL      string
	MIMEType string
	Duration int // seconds
}

// extractMediaEnclosure extracts the first audio or video enclosure from an RSS item
func extractMediaEnclosure(item *gofeed.Item) *MediaEnclosure {
	for _, enc := range item.Enclosures {
		if strings.HasPrefix(enc.Type, "audio/") || strings.HasPrefix(enc.Type, "video/") {
			return &MediaEnclosure{
				URL:      enc.URL,
				MIMEType: enc.Type,
				Duration: extractDuration(item),
			}
		}
	}
	return nil
}

// extractDuration extracts duration in seconds from iTunes namespace
// Supports both seconds format (3600) and HH:MM:SS format (1:00:00)
func extractDuration(item *gofeed.Item) int {
	if item.ITunesExt != nil && item.ITunesExt.Duration != "" {
		return parseDuration(item.ITunesExt.Duration)
	}
	return 0
}

// parseDuration converts iTunes duration string to seconds. Accepts:
//   - "3600" (plain seconds)
//   - "60:00" (MM:SS)
//   - "1:00:00" (HH:MM:SS)
//
// Returns 0 on parse failure. Caps the result at maxMediaDurationSeconds to
// prevent overflow when a hostile feed claims a gigayear-long podcast.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	var total int64
	switch len(parts) {
	case 1:
		total = parseSafeInt(parts[0])
	case 2:
		minutes := parseSafeInt(parts[0])
		seconds := parseSafeInt(parts[1])
		if minutes > maxMediaDurationSeconds || seconds > maxMediaDurationSeconds {
			return maxMediaDurationSeconds
		}
		total = minutes*60 + seconds
	case 3:
		hours := parseSafeInt(parts[0])
		minutes := parseSafeInt(parts[1])
		seconds := parseSafeInt(parts[2])
		if hours > maxMediaDurationSeconds || minutes > maxMediaDurationSeconds || seconds > maxMediaDurationSeconds {
			return maxMediaDurationSeconds
		}
		total = hours*3600 + minutes*60 + seconds
	default:
		return 0
	}
	// Each part is bounded above by maxMediaDurationSeconds before the
	// multiply/add, so total cannot overflow int64 (max ≈ 86400*3600 ≈ 3.1e8)
	// and is always ≥ 0 — no wrap to a bogus, attacker-chosen positive value.
	if total > maxMediaDurationSeconds {
		return maxMediaDurationSeconds
	}
	return int(total)
}

// maxMediaDurationSeconds caps duration at 24 hours — anything longer is
// almost certainly garbage. Bounds the int32 storage in the DB cleanly.
const maxMediaDurationSeconds = 24 * 3600

// parseSafeInt parses digits from s into int64 with overflow protection.
// Non-digit runes are ignored (matching the previous behavior). Returns 0 on
// overflow or empty input.
func parseSafeInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			continue
		}
		d := int64(c - '0')
		if n > (1<<62-d)/10 {
			return 0 // bail on overflow rather than wrapping
		}
		n = n*10 + d
	}
	return n
}

// determineMediaType returns "podcast" or "video" based on MIME type
func determineMediaType(mimeType string) string {
	if strings.HasPrefix(mimeType, "audio/") {
		return "podcast"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	return ""
}
