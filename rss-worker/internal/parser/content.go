package parser

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
)

// ContentExtractor fetches and extracts article content from web pages
type ContentExtractor struct {
	client *http.Client
}

// NewContentExtractor creates a new extractor with a configured HTTP client
func NewContentExtractor() *ContentExtractor {
	return &ContentExtractor{
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

// ExtractedContent holds the extracted article data
type ExtractedContent struct {
	Content     string
	TextContent string // Plain text version
	Excerpt     string // Short summary if available
}

// ExtractContent fetches the article page and extracts the main content
func (e *ContentExtractor) ExtractContent(ctx context.Context, articleURL string) (*ExtractedContent, error) {
	parsedURL, err := url.Parse(articleURL)
	if err != nil {
		return nil, err
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", articleURL, nil)
	if err != nil {
		return nil, err
	}

	// Set headers to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PulseFeed/1.0; +https://pulsefeed.app)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := e.client.Do(req)
	if err != nil {
		log.Printf("[CONTENT-HTTP] Request failed for %s: %v", articleURL, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[CONTENT-HTTP] Non-200 status %d for %s", resp.StatusCode, articleURL)
		return nil, nil
	}

	// Use go-readability to extract the article content
	article, err := readability.FromReader(resp.Body, parsedURL)
	if err != nil {
		log.Printf("[CONTENT] Readability failed for %s: %v", articleURL, err)
		return nil, err
	}

	// Clean up the content
	content := strings.TrimSpace(article.Content)
	textContent := strings.TrimSpace(article.TextContent)
	excerpt := strings.TrimSpace(article.Excerpt)

	// Skip if content is too short (likely extraction failed)
	if len(textContent) < 100 {
		log.Printf("[CONTENT] Content too short for %s (len=%d)", articleURL, len(textContent))
		return nil, nil
	}

	return &ExtractedContent{
		Content:     content,
		TextContent: textContent,
		Excerpt:     excerpt,
	}, nil
}

// ExtractTextContent is a convenience method that returns just the plain text content
func (e *ContentExtractor) ExtractTextContent(ctx context.Context, articleURL string) (string, error) {
	extracted, err := e.ExtractContent(ctx, articleURL)
	if err != nil {
		return "", err
	}
	if extracted == nil {
		return "", nil
	}
	return extracted.TextContent, nil
}
