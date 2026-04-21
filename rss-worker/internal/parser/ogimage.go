package parser

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pulsefeed/rss-worker/internal/httputil"
	"github.com/pulsefeed/rss-worker/internal/logger"
)

// OGImageExtractor fetches og:image meta tags from article pages
type OGImageExtractor struct {
	client *http.Client
}

// NewOGImageExtractor creates a new extractor. Uses current package rate-limit
// settings; call parser.SetHostRateLimit first to override defaults.
func NewOGImageExtractor() *OGImageExtractor {
	return &OGImageExtractor{client: httputil.NewRateLimitedClient(10*time.Second, hostRPS, hostBurst, 3)}
}

// ogImagePatterns are regex patterns to extract og:image from HTML
// Ordered by priority - try og:image first, then twitter:image as fallback
var ogImagePatterns = []*regexp.Regexp{
	// Standard og:image (various attribute orders)
	regexp.MustCompile(`(?i)<meta[^>]+property\s*=\s*["']og:image["'][^>]+content\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta[^>]+content\s*=\s*["']([^"']+)["'][^>]+property\s*=\s*["']og:image["']`),
	// og:image:url variant (some sites use this)
	regexp.MustCompile(`(?i)<meta[^>]+property\s*=\s*["']og:image:url["'][^>]+content\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta[^>]+content\s*=\s*["']([^"']+)["'][^>]+property\s*=\s*["']og:image:url["']`),
	// Twitter image as fallback
	regexp.MustCompile(`(?i)<meta[^>]+name\s*=\s*["']twitter:image["'][^>]+content\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta[^>]+content\s*=\s*["']([^"']+)["'][^>]+name\s*=\s*["']twitter:image["']`),
	// Twitter image:src variant
	regexp.MustCompile(`(?i)<meta[^>]+name\s*=\s*["']twitter:image:src["'][^>]+content\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta[^>]+content\s*=\s*["']([^"']+)["'][^>]+name\s*=\s*["']twitter:image:src["']`),
}

// ExtractOGImage fetches the article page and extracts the og:image URL
func (e *OGImageExtractor) ExtractOGImage(ctx context.Context, articleURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", articleURL, nil)
	if err != nil {
		return "", err
	}

	// Set a reasonable User-Agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PulseFeed/1.0; +https://pulsefeed.app)")
	req.Header.Set("Accept", "text/html")

	resp, err := e.client.Do(req)
	if err != nil {
		logger.Debugf("[OG-HTTP] Request failed for %s: %v", articleURL, err)
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body) // drain body to enable connection reuse
		logger.Debugf("[OG-HTTP] Non-200 status %d for %s", resp.StatusCode, articleURL)
		return "", nil // Not an error, just no image found
	}

	// Read only the first 100KB to find meta tags (they're in <head>)
	limitedReader := io.LimitReader(resp.Body, 100*1024)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", err
	}

	// Try each pattern to find og:image (use byte matching to avoid copying)
	for _, pattern := range ogImagePatterns {
		matches := pattern.FindSubmatch(body)
		if len(matches) > 1 {
			imageURL := strings.TrimSpace(string(matches[1]))
			// Validate it looks like a URL
			if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
				return imageURL, nil
			}
		}
	}

	return "", nil // No og:image found
}
