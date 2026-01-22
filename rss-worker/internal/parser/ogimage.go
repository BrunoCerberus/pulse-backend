package parser

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// OGImageExtractor fetches og:image meta tags from article pages
type OGImageExtractor struct {
	client *http.Client
}

// NewOGImageExtractor creates a new extractor with a configured HTTP client
func NewOGImageExtractor() *OGImageExtractor {
	return &OGImageExtractor{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// ogImagePatterns are regex patterns to extract og:image from HTML
var ogImagePatterns = []*regexp.Regexp{
	// Standard og:image
	regexp.MustCompile(`<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`),
	regexp.MustCompile(`<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:image["']`),
	// Twitter image as fallback
	regexp.MustCompile(`<meta[^>]+name=["']twitter:image["'][^>]+content=["']([^"']+)["']`),
	regexp.MustCompile(`<meta[^>]+content=["']([^"']+)["'][^>]+name=["']twitter:image["']`),
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
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil // Not an error, just no image found
	}

	// Read only the first 100KB to find meta tags (they're in <head>)
	limitedReader := io.LimitReader(resp.Body, 100*1024)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", err
	}

	html := string(body)

	// Try each pattern to find og:image
	for _, pattern := range ogImagePatterns {
		matches := pattern.FindStringSubmatch(html)
		if len(matches) > 1 {
			imageURL := strings.TrimSpace(matches[1])
			// Validate it looks like a URL
			if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
				return imageURL, nil
			}
		}
	}

	return "", nil // No og:image found
}
