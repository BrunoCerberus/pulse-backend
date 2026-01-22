package parser

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/pulsefeed/rss-worker/internal/models"
)

// Parser handles RSS/Atom feed parsing
type Parser struct {
	fp          *gofeed.Parser
	ogExtractor *OGImageExtractor
}

// New creates a new Parser instance
func New() *Parser {
	return &Parser{
		fp:          gofeed.NewParser(),
		ogExtractor: NewOGImageExtractor(),
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

	return articles, nil
}

// enrichWithOGImages fetches og:image for each article in parallel
// Uses a worker pool to avoid overwhelming servers
func (p *Parser) enrichWithOGImages(ctx context.Context, articles []*models.Article) {
	if len(articles) == 0 {
		return
	}

	const maxWorkers = 5
	numWorkers := min(maxWorkers, len(articles))

	// Channel for work items
	work := make(chan *models.Article, len(articles))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range work {
				select {
				case <-ctx.Done():
					return
				default:
					p.fetchOGImageForArticle(ctx, article)
				}
			}
		}()
	}

	// Send work
	for _, article := range articles {
		work <- article
	}
	close(work)

	// Wait for completion
	wg.Wait()
}

// fetchOGImageForArticle fetches the og:image for a single article
func (p *Parser) fetchOGImageForArticle(ctx context.Context, article *models.Article) {
	ogImage, err := p.ogExtractor.ExtractOGImage(ctx, article.URL)
	if err != nil {
		log.Printf("Failed to fetch og:image for %s: %v", article.URL, err)
		return
	}

	if ogImage != "" {
		article.ImageURL = &ogImage
		log.Printf("Found og:image for %s: %s", article.URL, ogImage)
	}
}

// itemToArticle converts a gofeed.Item to our Article model
func (p *Parser) itemToArticle(item *gofeed.Item, source models.Source) *models.Article {
	if item.Link == "" || item.Title == "" {
		return nil
	}

	// Parse published date
	publishedAt := time.Now()
	if item.PublishedParsed != nil {
		publishedAt = *item.PublishedParsed
	} else if item.UpdatedParsed != nil {
		publishedAt = *item.UpdatedParsed
	}

	article := models.NewArticle(
		strings.TrimSpace(item.Title),
		strings.TrimSpace(item.Link),
		source.ID,
		source.CategoryID,
		publishedAt,
	)

	// Summary/Description
	if item.Description != "" {
		desc := cleanHTML(item.Description)
		article.Summary = &desc
	}

	// Content (if available)
	if item.Content != "" {
		content := cleanHTML(item.Content)
		article.Content = &content
	}

	// Author
	if item.Author != nil && item.Author.Name != "" {
		article.Author = &item.Author.Name
	} else if len(item.Authors) > 0 && item.Authors[0].Name != "" {
		article.Author = &item.Authors[0].Name
	}

	// Image: Use RSS image as thumbnail, og:image will be fetched later for full-size
	thumbnailURL := extractImageURL(item)
	if thumbnailURL != "" {
		article.ThumbnailURL = &thumbnailURL
		// Also set ImageURL as fallback in case og:image fetch fails
		article.ImageURL = &thumbnailURL
	}

	return article
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

// cleanHTML removes HTML tags and cleans up text
func cleanHTML(s string) string {
	// Simple HTML tag removal (for basic cases)
	// For production, consider using a proper HTML parser
	result := s

	// Remove common HTML tags
	replacer := strings.NewReplacer(
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
	result = replacer.Replace(result)

	// Remove remaining HTML tags (simple regex-like removal)
	var cleaned strings.Builder
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
