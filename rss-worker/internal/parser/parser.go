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
	fp               *gofeed.Parser     // gofeed parser for RSS/Atom
	ogExtractor      *OGImageExtractor  // Extracts og:image from article pages
	contentExtractor *ContentExtractor  // Extracts article text via readability
}

// New creates a new Parser instance
func New() *Parser {
	fp := gofeed.NewParser()
	fp.Client = httputil.NewClientWithRedirectLimit(30*time.Second, 5)
	return &Parser{
		fp:               fp,
		ogExtractor:      NewOGImageExtractor(),
		contentExtractor: NewContentExtractor(),
	}
}

// ParseFeed fetches and parses an RSS feed, returning articles
func (p *Parser) ParseFeed(ctx context.Context, source models.Source) ([]*models.Article, error) {
	feed, err := p.fp.ParseURLWithContext(source.FeedURL, ctx)
	if err != nil {
		return nil, err
	}

	articles := make([]*models.Article, 0, len(feed.Items))

	for _, item := range feed.Items {
		article := p.itemToArticle(item, source)
		if article != nil {
			articles = append(articles, article)
		}
	}

	// Fetch og:images in parallel for high-resolution header images
	p.enrichWithOGImages(ctx, articles)

	// Extract content for articles that don't have it from RSS
	p.enrichWithContent(ctx, articles)

	return articles, nil
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
		work <- article
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
func (p *Parser) enrichWithContent(ctx context.Context, articles []*models.Article) enrichStats {
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
					p.fetchContentForArticle(ctx, article)
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
		work <- article
	}
	close(work)

	wg.Wait()
	stats := enrichStats{Success: int(success.Load()), Failed: int(failed.Load())}
	logger.Infof("[CONTENT] Completed content extraction: success=%d, failed=%d", stats.Success, stats.Failed)
	return stats
}

// fetchContentForArticle extracts content for a single article
func (p *Parser) fetchContentForArticle(ctx context.Context, article *models.Article) {
	content, err := p.contentExtractor.ExtractTextContent(ctx, article.URL)
	if err != nil {
		logger.Debugf("[CONTENT] ERROR fetching %s: %v", article.URL, err)
		return
	}

	if content != "" {
		article.Content = &content
		logger.Debugf("[CONTENT] SUCCESS %s (%d chars)", article.URL, len(content))
	} else {
		logger.Debugf("[CONTENT] NOT FOUND for %s", article.URL)
	}
}

// itemToArticle converts a gofeed.Item to our Article model
func (p *Parser) itemToArticle(item *gofeed.Item, source models.Source) *models.Article {
	if item.Link == "" || item.Title == "" {
		return nil
	}

	article := models.NewArticle(
		strings.TrimSpace(item.Title),
		strings.TrimSpace(item.Link),
		source.ID,
		source.CategoryID,
		source.Language,
		parsePublishedDate(item),
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
		article.Summary = &desc
	}

	// Content (if available)
	if item.Content != "" {
		content := cleanHTML(item.Content)
		article.Content = &content
	}

	article.Author = extractAuthor(item)

	// Image: Use RSS image as thumbnail, og:image will be fetched later for full-size
	thumbnailURL := extractImageURL(item)
	if thumbnailURL != "" {
		article.ThumbnailURL = &thumbnailURL
		// Also set ImageURL as fallback in case og:image fetch fails
		article.ImageURL = &thumbnailURL
	}

	extractMediaInfo(item, article)

	return article
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

// extractAuthor returns the author name from a feed item, or nil if unavailable.
func extractAuthor(item *gofeed.Item) *string {
	if item.Author != nil && item.Author.Name != "" {
		return &item.Author.Name
	}
	if len(item.Authors) > 0 && item.Authors[0].Name != "" {
		return &item.Authors[0].Name
	}
	return nil
}

// extractMediaInfo populates the article's media fields from RSS enclosures.
func extractMediaInfo(item *gofeed.Item, article *models.Article) {
	media := extractMediaEnclosure(item)
	if media == nil {
		return
	}
	mediaType := determineMediaType(media.MIMEType)
	article.MediaType = &mediaType
	article.MediaURL = &media.URL
	article.MediaMIMEType = &media.MIMEType
	if media.Duration > 0 {
		article.MediaDuration = &media.Duration
	}
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

// htmlReplacer is a package-level Replacer for HTML entity/tag substitution.
// It is safe for concurrent use and avoids rebuilding the lookup table on every call.
var htmlReplacer = strings.NewReplacer(
	"<br>", "\n",
	"<br/>", "\n",
	"<br />", "\n",
	"<p>", "\n",
	"</p>", "\n",
	"&nbsp;", " ",
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", "\"",
	"&#39;", "'",
)

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
		if afterTag < len(s) && s[afterTag] != '>' && s[afterTag] != ' ' && s[afterTag] != '\t' && s[afterTag] != '\n' {
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

// cleanHTML removes HTML tags and cleans up text
func cleanHTML(s string) string {
	// Strip script and style tags with their contents before tag removal
	s = removeTagWithContent(s, "script")
	s = removeTagWithContent(s, "style")

	result := htmlReplacer.Replace(s)

	// Remove remaining HTML tags (simple regex-like removal)
	var cleaned strings.Builder
	cleaned.Grow(len(result))
	inTag := false
	for _, r := range result {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			cleaned.WriteRune(r)
		}
	}

	// Clean up whitespace
	result = strings.TrimSpace(cleaned.String())

	// Collapse multiple newlines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return result
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

// parseDuration converts iTunes duration string to seconds
// Accepts: "3600" (seconds), "60:00" (MM:SS), "1:00:00" (HH:MM:SS)
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		// Plain seconds
		var seconds int
		for _, c := range parts[0] {
			if c >= '0' && c <= '9' {
				seconds = seconds*10 + int(c-'0')
			}
		}
		return seconds
	case 2:
		// MM:SS
		minutes := parseIntFromString(parts[0])
		seconds := parseIntFromString(parts[1])
		return minutes*60 + seconds
	case 3:
		// HH:MM:SS
		hours := parseIntFromString(parts[0])
		minutes := parseIntFromString(parts[1])
		seconds := parseIntFromString(parts[2])
		return hours*3600 + minutes*60 + seconds
	}
	return 0
}

// parseIntFromString parses digits from a string, ignoring non-digits
func parseIntFromString(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
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
