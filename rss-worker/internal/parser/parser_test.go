package parser

import (
	"testing"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
)

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
