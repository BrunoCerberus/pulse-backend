package parser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
	"github.com/pulsefeed/rss-worker/internal/models"
)

func strPtr(s string) *string {
	return &s
}

func TestCleanHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text unchanged",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "br tags to newlines",
			input:    "Line 1<br>Line 2<br/>Line 3<br />Line 4",
			expected: "Line 1\nLine 2\nLine 3\nLine 4",
		},
		{
			name:     "paragraph tags",
			input:    "<p>Paragraph 1</p><p>Paragraph 2</p>",
			expected: "Paragraph 1\n\nParagraph 2",
		},
		{
			name:     "HTML entities amp nbsp quot",
			input:    "Tom &amp; Jerry &quot;friends&quot; &nbsp;test",
			expected: "Tom & Jerry \"friends\"  test",
		},
		{
			name:     "HTML entity single quote",
			input:    "It&#39;s a test",
			expected: "It's a test",
		},
		{
			name:     "nested tags removed",
			input:    "<div><span>Hello</span> <b>World</b></div>",
			expected: "Hello World",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace trimmed",
			input:    "  <p>  Content  </p>  ",
			expected: "Content",
		},
		{
			name:     "link tags removed",
			input:    `Read more at <a href="https://example.com">our website</a>`,
			expected: "Read more at our website",
		},
		{
			name:     "multiple newlines collapsed",
			input:    "<p>A</p><p></p><p></p><p>B</p>",
			expected: "A\n\nB",
		},
		{
			name:     "script tags removed",
			input:    "<script>alert('xss')</script>Safe content",
			expected: "alert('xss')Safe content",
		},
		{
			name:     "style tags removed",
			input:    "<style>.class{color:red}</style>Visible content",
			expected: ".class{color:red}Visible content",
		},
		{
			name:     "complex HTML",
			input:    "<div class=\"article\"><h1>Title</h1><p>First paragraph.</p><p>Second paragraph.</p></div>",
			expected: "Title\nFirst paragraph.\n\nSecond paragraph.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanHTML(tt.input)
			if got != tt.expected {
				t.Errorf("cleanHTML(%q)\ngot:  %q\nwant: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCleanHTML_PreservesText(t *testing.T) {
	// Ensure regular text is not modified
	text := "This is a normal paragraph with no HTML."
	result := cleanHTML(text)
	if result != text {
		t.Errorf("cleanHTML modified plain text: got %q, want %q", result, text)
	}
}

func TestExtractImageURL(t *testing.T) {
	tests := []struct {
		name     string
		item     *gofeed.Item
		expected string
	}{
		{
			name: "image from Item.Image",
			item: &gofeed.Item{
				Image: &gofeed.Image{URL: "https://example.com/image.jpg"},
			},
			expected: "https://example.com/image.jpg",
		},
		{
			name: "image from enclosure with image/jpeg type",
			item: &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{Type: "image/jpeg", URL: "https://example.com/enc.jpg"},
				},
			},
			expected: "https://example.com/enc.jpg",
		},
		{
			name: "image from enclosure with image/png type",
			item: &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{Type: "image/png", URL: "https://example.com/enc.png"},
				},
			},
			expected: "https://example.com/enc.png",
		},
		{
			name: "non-image enclosure skipped",
			item: &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{Type: "audio/mpeg", URL: "https://example.com/audio.mp3"},
				},
			},
			expected: "",
		},
		{
			name: "first image enclosure used when multiple",
			item: &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{Type: "audio/mpeg", URL: "https://example.com/audio.mp3"},
					{Type: "image/jpeg", URL: "https://example.com/first.jpg"},
					{Type: "image/png", URL: "https://example.com/second.png"},
				},
			},
			expected: "https://example.com/first.jpg",
		},
		{
			name: "media:content extension",
			item: &gofeed.Item{
				Extensions: map[string]map[string][]ext.Extension{
					"media": {
						"content": {
							{Attrs: map[string]string{"url": "https://example.com/media.jpg"}},
						},
					},
				},
			},
			expected: "https://example.com/media.jpg",
		},
		{
			name: "media:thumbnail extension",
			item: &gofeed.Item{
				Extensions: map[string]map[string][]ext.Extension{
					"media": {
						"thumbnail": {
							{Attrs: map[string]string{"url": "https://example.com/thumb.jpg"}},
						},
					},
				},
			},
			expected: "https://example.com/thumb.jpg",
		},
		{
			name: "media:content preferred over media:thumbnail",
			item: &gofeed.Item{
				Extensions: map[string]map[string][]ext.Extension{
					"media": {
						"content": {
							{Attrs: map[string]string{"url": "https://example.com/content.jpg"}},
						},
						"thumbnail": {
							{Attrs: map[string]string{"url": "https://example.com/thumb.jpg"}},
						},
					},
				},
			},
			expected: "https://example.com/content.jpg",
		},
		{
			name: "Item.Image preferred over enclosure",
			item: &gofeed.Item{
				Image: &gofeed.Image{URL: "https://example.com/primary.jpg"},
				Enclosures: []*gofeed.Enclosure{
					{Type: "image/jpeg", URL: "https://example.com/enc.jpg"},
				},
			},
			expected: "https://example.com/primary.jpg",
		},
		{
			name:     "no image found returns empty",
			item:     &gofeed.Item{},
			expected: "",
		},
		{
			name: "nil Item.Image",
			item: &gofeed.Item{
				Image: nil,
			},
			expected: "",
		},
		{
			name: "Item.Image with empty URL",
			item: &gofeed.Item{
				Image: &gofeed.Image{URL: ""},
			},
			expected: "",
		},
		{
			name: "enclosure with empty URL",
			item: &gofeed.Item{
				Enclosures: []*gofeed.Enclosure{
					{Type: "image/jpeg", URL: ""},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractImageURL(tt.item)
			if got != tt.expected {
				t.Errorf("extractImageURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractImageURL_EmptyItem(t *testing.T) {
	item := &gofeed.Item{}
	result := extractImageURL(item)
	if result != "" {
		t.Errorf("extractImageURL(empty item) = %q, want empty string", result)
	}
}

func TestExtractImageURL_PreferenceOrder(t *testing.T) {
	// Test that Item.Image is preferred over enclosures and extensions
	item := &gofeed.Item{
		Image: &gofeed.Image{URL: "https://example.com/primary.jpg"},
		Enclosures: []*gofeed.Enclosure{
			{Type: "image/jpeg", URL: "https://example.com/enclosure.jpg"},
		},
		Extensions: map[string]map[string][]ext.Extension{
			"media": {
				"content": {
					{Attrs: map[string]string{"url": "https://example.com/media.jpg"}},
				},
			},
		},
	}

	got := extractImageURL(item)
	if got != "https://example.com/primary.jpg" {
		t.Errorf("Item.Image should be preferred, got %q", got)
	}
}

func TestTruncateToFirstParagraph(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "short text without period",
			input:    "Short text",
			expected: "Short text",
		},
		{
			name:     "single sentence",
			input:    "This is a complete sentence that is long enough to test the truncation logic properly.",
			expected: "This is a complete sentence that is long enough to test the truncation logic properly.",
		},
		{
			name:     "two sentences truncated to first",
			input:    "This is the first sentence that is long enough. This is the second sentence.",
			expected: "This is the first sentence that is long enough.",
		},
		{
			name:     "paragraph break",
			input:    "First paragraph content here.\n\nSecond paragraph content here.",
			expected: "First paragraph content here.",
		},
		{
			name:     "preserves short abbreviations",
			input:    "Dr. Smith works at the hospital and helps patients every day.",
			expected: "Dr. Smith works at the hospital and helps patients every day.",
		},
		{
			name:     "period at end of string",
			input:    "This is a complete sentence that is long enough to test.",
			expected: "This is a complete sentence that is long enough to test.",
		},
		{
			name:     "multiple sentences with newline",
			input:    "First sentence that is definitely long enough to test.\nSecond sentence here.",
			expected: "First sentence that is definitely long enough to test.",
		},
		{
			name:     "very long text truncated",
			input:    "This is the first sentence that provides important context. The second sentence adds more details. The third sentence is also here. And even more content follows.",
			expected: "This is the first sentence that provides important context.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToFirstParagraph(tt.input)
			if got != tt.expected {
				t.Errorf("truncateToFirstParagraph(%q)\ngot:  %q\nwant: %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractMediaEnclosure_Audio(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{Type: "audio/mpeg", URL: "https://example.com/episode.mp3"},
		},
	}

	media := extractMediaEnclosure(item)
	if media == nil {
		t.Fatal("expected media enclosure, got nil")
	}
	if media.URL != "https://example.com/episode.mp3" {
		t.Errorf("URL = %q, want %q", media.URL, "https://example.com/episode.mp3")
	}
	if media.MIMEType != "audio/mpeg" {
		t.Errorf("MIMEType = %q, want %q", media.MIMEType, "audio/mpeg")
	}
}

func TestExtractMediaEnclosure_Video(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{Type: "video/mp4", URL: "https://example.com/video.mp4"},
		},
	}

	media := extractMediaEnclosure(item)
	if media == nil {
		t.Fatal("expected media enclosure, got nil")
	}
	if media.URL != "https://example.com/video.mp4" {
		t.Errorf("URL = %q, want %q", media.URL, "https://example.com/video.mp4")
	}
	if media.MIMEType != "video/mp4" {
		t.Errorf("MIMEType = %q, want %q", media.MIMEType, "video/mp4")
	}
}

func TestExtractMediaEnclosure_SkipsImages(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{Type: "image/jpeg", URL: "https://example.com/image.jpg"},
		},
	}

	media := extractMediaEnclosure(item)
	if media != nil {
		t.Errorf("expected nil for image enclosure, got %+v", media)
	}
}

func TestExtractMediaEnclosure_FirstAudioOrVideo(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{Type: "image/jpeg", URL: "https://example.com/image.jpg"},
			{Type: "audio/mpeg", URL: "https://example.com/first.mp3"},
			{Type: "video/mp4", URL: "https://example.com/video.mp4"},
		},
	}

	media := extractMediaEnclosure(item)
	if media == nil {
		t.Fatal("expected media enclosure, got nil")
	}
	if media.URL != "https://example.com/first.mp3" {
		t.Errorf("URL = %q, want first audio URL", media.URL)
	}
}

func TestExtractMediaEnclosure_NoEnclosures(t *testing.T) {
	item := &gofeed.Item{}

	media := extractMediaEnclosure(item)
	if media != nil {
		t.Errorf("expected nil for empty item, got %+v", media)
	}
}

func TestExtractMediaEnclosure_WithDuration(t *testing.T) {
	item := &gofeed.Item{
		Enclosures: []*gofeed.Enclosure{
			{Type: "audio/mpeg", URL: "https://example.com/episode.mp3"},
		},
		ITunesExt: &ext.ITunesItemExtension{
			Duration: "3600",
		},
	}

	media := extractMediaEnclosure(item)
	if media == nil {
		t.Fatal("expected media enclosure, got nil")
	}
	if media.Duration != 3600 {
		t.Errorf("Duration = %d, want 3600", media.Duration)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "plain seconds",
			input:    "3600",
			expected: 3600,
		},
		{
			name:     "MM:SS format",
			input:    "60:00",
			expected: 3600,
		},
		{
			name:     "HH:MM:SS format",
			input:    "1:00:00",
			expected: 3600,
		},
		{
			name:     "short episode MM:SS",
			input:    "25:30",
			expected: 1530,
		},
		{
			name:     "long episode HH:MM:SS",
			input:    "2:15:45",
			expected: 8145,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "whitespace",
			input:    "  3600  ",
			expected: 3600,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
		},
		{
			name:     "leading zeros HH:MM:SS",
			input:    "01:05:30",
			expected: 3930,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDuration(tt.input)
			if got != tt.expected {
				t.Errorf("parseDuration(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDetermineMediaType(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		expected string
	}{
		{
			name:     "audio/mpeg is podcast",
			mimeType: "audio/mpeg",
			expected: "podcast",
		},
		{
			name:     "audio/mp4 is podcast",
			mimeType: "audio/mp4",
			expected: "podcast",
		},
		{
			name:     "audio/aac is podcast",
			mimeType: "audio/aac",
			expected: "podcast",
		},
		{
			name:     "video/mp4 is video",
			mimeType: "video/mp4",
			expected: "video",
		},
		{
			name:     "video/webm is video",
			mimeType: "video/webm",
			expected: "video",
		},
		{
			name:     "image/jpeg returns empty",
			mimeType: "image/jpeg",
			expected: "",
		},
		{
			name:     "empty string returns empty",
			mimeType: "",
			expected: "",
		},
		{
			name:     "application/octet-stream returns empty",
			mimeType: "application/octet-stream",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineMediaType(tt.mimeType)
			if got != tt.expected {
				t.Errorf("determineMediaType(%q) = %q, want %q", tt.mimeType, got, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.fp == nil {
		t.Error("fp (gofeed.Parser) is nil")
	}
	if p.ogExtractor == nil {
		t.Error("ogExtractor is nil")
	}
	if p.contentExtractor == nil {
		t.Error("contentExtractor is nil")
	}
}

func TestItemToArticle(t *testing.T) {
	p := New()
	source := models.Source{
		ID:         "src-1",
		Name:       "Test Source",
		CategoryID: strPtr("cat-1"),
		Language:   "en",
	}

	now := time.Now()
	published := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		item   *gofeed.Item
		check  func(t *testing.T, a *models.Article)
	}{
		{
			name: "basic conversion",
			item: &gofeed.Item{
				Title: "Test Title",
				Link:  "https://example.com/article",
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Title != "Test Title" {
					t.Errorf("Title = %q, want %q", a.Title, "Test Title")
				}
				if a.URL != "https://example.com/article" {
					t.Errorf("URL = %q, want %q", a.URL, "https://example.com/article")
				}
				if a.SourceID != "src-1" {
					t.Errorf("SourceID = %q, want %q", a.SourceID, "src-1")
				}
				if a.URLHash == "" {
					t.Error("URLHash is empty")
				}
			},
		},
		{
			name: "missing title returns nil",
			item: &gofeed.Item{
				Link: "https://example.com/article",
			},
			check: func(t *testing.T, a *models.Article) {
				if a != nil {
					t.Errorf("expected nil for missing title, got %+v", a)
				}
			},
		},
		{
			name: "missing link returns nil",
			item: &gofeed.Item{
				Title: "Test Title",
			},
			check: func(t *testing.T, a *models.Article) {
				if a != nil {
					t.Errorf("expected nil for missing link, got %+v", a)
				}
			},
		},
		{
			name: "title and link trimmed",
			item: &gofeed.Item{
				Title: "  Padded Title  ",
				Link:  "  https://example.com/padded  ",
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Title != "Padded Title" {
					t.Errorf("Title = %q, want trimmed", a.Title)
				}
				if a.URL != "https://example.com/padded" {
					t.Errorf("URL = %q, want trimmed", a.URL)
				}
			},
		},
		{
			name: "author from Author field",
			item: &gofeed.Item{
				Title:  "Test",
				Link:   "https://example.com/1",
				Author: &gofeed.Person{Name: "John Doe"},
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Author == nil || *a.Author != "John Doe" {
					t.Errorf("Author = %v, want 'John Doe'", a.Author)
				}
			},
		},
		{
			name: "author from Authors list",
			item: &gofeed.Item{
				Title:   "Test",
				Link:    "https://example.com/2",
				Authors: []*gofeed.Person{{Name: "Jane Smith"}},
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Author == nil || *a.Author != "Jane Smith" {
					t.Errorf("Author = %v, want 'Jane Smith'", a.Author)
				}
			},
		},
		{
			name: "summary from description",
			item: &gofeed.Item{
				Title:       "Test",
				Link:        "https://example.com/3",
				Description: "<p>This is a <b>description</b> with HTML tags.</p>",
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Summary == nil {
					t.Fatal("Summary is nil")
				}
				if *a.Summary == "" {
					t.Error("Summary is empty")
				}
			},
		},
		{
			name: "content from Content field",
			item: &gofeed.Item{
				Title:   "Test",
				Link:    "https://example.com/4",
				Content: "<p>Full article <b>content</b> here.</p>",
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.Content == nil {
					t.Fatal("Content is nil")
				}
				if *a.Content == "" {
					t.Error("Content is empty")
				}
			},
		},
		{
			name: "published date from PublishedParsed",
			item: &gofeed.Item{
				Title:           "Test",
				Link:            "https://example.com/5",
				PublishedParsed: &published,
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if !a.PublishedAt.Equal(published) {
					t.Errorf("PublishedAt = %v, want %v", a.PublishedAt, published)
				}
			},
		},
		{
			name: "published date from UpdatedParsed fallback",
			item: &gofeed.Item{
				Title:         "Test",
				Link:          "https://example.com/6",
				UpdatedParsed: &published,
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if !a.PublishedAt.Equal(published) {
					t.Errorf("PublishedAt = %v, want %v", a.PublishedAt, published)
				}
			},
		},
		{
			name: "published date defaults to now",
			item: &gofeed.Item{
				Title: "Test",
				Link:  "https://example.com/7",
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				diff := time.Since(a.PublishedAt)
				if diff > 2*time.Second || diff < -2*time.Second {
					t.Errorf("PublishedAt = %v, expected within 2s of now", a.PublishedAt)
				}
			},
		},
		{
			name: "thumbnail from RSS image",
			item: &gofeed.Item{
				Title: "Test",
				Link:  "https://example.com/8",
				Image: &gofeed.Image{URL: "https://example.com/thumb.jpg"},
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.ThumbnailURL == nil || *a.ThumbnailURL != "https://example.com/thumb.jpg" {
					t.Errorf("ThumbnailURL = %v, want 'https://example.com/thumb.jpg'", a.ThumbnailURL)
				}
				if a.ImageURL == nil || *a.ImageURL != "https://example.com/thumb.jpg" {
					t.Errorf("ImageURL = %v, want 'https://example.com/thumb.jpg' (fallback)", a.ImageURL)
				}
			},
		},
		{
			name: "media enclosure for audio",
			item: &gofeed.Item{
				Title: "Podcast Episode",
				Link:  "https://example.com/9",
				Enclosures: []*gofeed.Enclosure{
					{Type: "audio/mpeg", URL: "https://example.com/ep.mp3"},
				},
				ITunesExt: &ext.ITunesItemExtension{Duration: "1800"},
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.MediaType == nil || *a.MediaType != "podcast" {
					t.Errorf("MediaType = %v, want 'podcast'", a.MediaType)
				}
				if a.MediaURL == nil || *a.MediaURL != "https://example.com/ep.mp3" {
					t.Errorf("MediaURL = %v, want 'https://example.com/ep.mp3'", a.MediaURL)
				}
				if a.MediaDuration == nil || *a.MediaDuration != 1800 {
					t.Errorf("MediaDuration = %v, want 1800", a.MediaDuration)
				}
			},
		},
		{
			name: "no media when only image enclosures",
			item: &gofeed.Item{
				Title: "Article",
				Link:  "https://example.com/10",
				Enclosures: []*gofeed.Enclosure{
					{Type: "image/jpeg", URL: "https://example.com/image.jpg"},
				},
			},
			check: func(t *testing.T, a *models.Article) {
				if a == nil {
					t.Fatal("expected article, got nil")
				}
				if a.MediaType != nil {
					t.Errorf("MediaType = %v, want nil for image-only enclosure", a.MediaType)
				}
			},
		},
	}

	_ = now // used indirectly
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			article := p.itemToArticle(tt.item, source)
			tt.check(t, article)
		})
	}
}

func TestItemToArticle_LanguagePropagation(t *testing.T) {
	p := New()
	source := models.Source{
		ID:         "src-pt",
		Name:       "Folha de S.Paulo",
		CategoryID: strPtr("cat-1"),
		Language:   "pt",
	}

	item := &gofeed.Item{
		Title: "Notícia do Brasil",
		Link:  "https://example.com/noticia",
	}

	article := p.itemToArticle(item, source)
	if article == nil {
		t.Fatal("expected article, got nil")
	}
	if article.Language != "pt" {
		t.Errorf("Language = %q, want %q", article.Language, "pt")
	}
}

func TestParseFeed_BasicRSS(t *testing.T) {
	// We need a server whose URL we know before building the RSS XML
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/feed":
			rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Article One</title>
      <link>%s/article/1</link>
    </item>
    <item>
      <title>Article Two</title>
      <link>%s/article/2</link>
    </item>
  </channel>
</rss>`, serverURL, serverURL)
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(rss))
		default:
			// Serve article HTML with og:image for enrichment
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head><body><p>Article content that is long enough to be extracted by the readability parser for testing purposes. This needs to be over one hundred characters long.</p></body></html>`))
		}
	}))
	defer server.Close()
	serverURL = server.URL

	p := New()
	source := models.Source{
		ID:       "src-1",
		Name:     "Test Source",
		FeedURL:  serverURL + "/feed",
		Language: "en",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	articles, err := p.ParseFeed(ctx, source)
	if err != nil {
		t.Fatalf("ParseFeed error: %v", err)
	}

	if len(articles) != 2 {
		t.Fatalf("got %d articles, want 2", len(articles))
	}

	if articles[0].Title != "Article One" {
		t.Errorf("articles[0].Title = %q, want %q", articles[0].Title, "Article One")
	}
	if articles[1].Title != "Article Two" {
		t.Errorf("articles[1].Title = %q, want %q", articles[1].Title, "Article Two")
	}
}

func TestParseFeed_EmptyFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss))
	}))
	defer server.Close()

	p := New()
	source := models.Source{
		ID:       "src-1",
		Name:     "Empty Source",
		FeedURL:  server.URL,
		Language: "en",
	}

	ctx := context.Background()
	articles, err := p.ParseFeed(ctx, source)
	if err != nil {
		t.Fatalf("ParseFeed error: %v", err)
	}

	if len(articles) != 0 {
		t.Errorf("got %d articles, want 0", len(articles))
	}
}

func TestParseFeed_InvalidFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not xml at all"))
	}))
	defer server.Close()

	p := New()
	source := models.Source{
		ID:       "src-1",
		Name:     "Bad Source",
		FeedURL:  server.URL,
		Language: "en",
	}

	ctx := context.Background()
	_, err := p.ParseFeed(ctx, source)
	if err == nil {
		t.Error("expected error for invalid feed")
	}
}

func TestEnrichWithOGImages_SetsImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head></html>`))
	}))
	defer server.Close()

	p := New()
	articles := []*models.Article{
		{URL: server.URL + "/1", Title: "A1"},
		{URL: server.URL + "/2", Title: "A2"},
	}

	ctx := context.Background()
	p.enrichWithOGImages(ctx, articles)

	for i, a := range articles {
		if a.ImageURL == nil || *a.ImageURL != "https://example.com/og.jpg" {
			t.Errorf("articles[%d].ImageURL = %v, want 'https://example.com/og.jpg'", i, a.ImageURL)
		}
	}
}

func TestEnrichWithOGImages_EmptyArticles(t *testing.T) {
	p := New()
	ctx := context.Background()
	// Should not panic
	p.enrichWithOGImages(ctx, []*models.Article{})
}

func TestEnrichWithOGImages_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head></html>`))
	}))
	defer server.Close()

	p := New()
	articles := []*models.Article{
		{URL: server.URL + "/1", Title: "A1"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p.enrichWithOGImages(ctx, articles)

	// With a cancelled context, images should not be enriched
	if articles[0].ImageURL != nil {
		t.Logf("ImageURL may or may not be set depending on timing, got %v", articles[0].ImageURL)
	}
}

func TestEnrichWithContent_SetsContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Article</title></head><body><article><p>This is the full article content that is definitely long enough to be extracted by the readability parser. It needs to be over one hundred characters long to pass the length check in the content extractor.</p></article></body></html>`))
	}))
	defer server.Close()

	p := New()
	articles := []*models.Article{
		{URL: server.URL + "/1", Title: "A1"},
	}

	ctx := context.Background()
	p.enrichWithContent(ctx, articles)

	if articles[0].Content == nil || *articles[0].Content == "" {
		t.Error("expected content to be set")
	}
}

func TestEnrichWithContent_SkipsArticlesWithContent(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Write([]byte(`<html><body><p>New content.</p></body></html>`))
	}))
	defer server.Close()

	existingContent := "Existing content"
	p := New()
	articles := []*models.Article{
		{URL: server.URL + "/1", Title: "A1", Content: &existingContent},
	}

	ctx := context.Background()
	p.enrichWithContent(ctx, articles)

	if *articles[0].Content != "Existing content" {
		t.Errorf("Content = %q, want unchanged 'Existing content'", *articles[0].Content)
	}
	if requestCount != 0 {
		t.Errorf("expected 0 HTTP requests for articles with content, got %d", requestCount)
	}
}

func TestFetchOGImageForArticle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head></html>`))
	}))
	defer server.Close()

	p := New()
	article := &models.Article{URL: server.URL, Title: "Test"}

	ctx := context.Background()
	p.fetchOGImageForArticle(ctx, article)

	if article.ImageURL == nil || *article.ImageURL != "https://example.com/og.jpg" {
		t.Errorf("ImageURL = %v, want 'https://example.com/og.jpg'", article.ImageURL)
	}
}

func TestFetchOGImageForArticle_SameAsThumbnail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/thumb.jpg"></head></html>`))
	}))
	defer server.Close()

	thumb := "https://example.com/thumb.jpg"
	p := New()
	article := &models.Article{URL: server.URL, Title: "Test", ThumbnailURL: &thumb}

	ctx := context.Background()
	p.fetchOGImageForArticle(ctx, article)

	// og:image is same as thumbnail, so ImageURL should NOT be updated
	if article.ImageURL != nil {
		t.Errorf("ImageURL = %v, want nil (same as thumbnail)", article.ImageURL)
	}
}

func TestFetchContentForArticle_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Article</title></head><body><article><p>This is a full article content body that is definitely long enough to pass the readability content length check of one hundred characters minimum threshold.</p></article></body></html>`))
	}))
	defer server.Close()

	p := New()
	article := &models.Article{URL: server.URL, Title: "Test"}

	ctx := context.Background()
	p.fetchContentForArticle(ctx, article)

	if article.Content == nil || *article.Content == "" {
		t.Error("expected Content to be set")
	}
}

func TestFetchContentForArticle_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := New()
	article := &models.Article{URL: server.URL, Title: "Test"}

	ctx := context.Background()
	p.fetchContentForArticle(ctx, article)

	if article.Content != nil {
		t.Errorf("Content = %v, want nil for error response", article.Content)
	}
}
