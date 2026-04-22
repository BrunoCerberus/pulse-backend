package parser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractOGImage(t *testing.T) {
	tests := []struct {
		name          string
		htmlContent   string
		statusCode    int
		expectedImage string
		expectError   bool
	}{
		{
			name: "standard og:image property then content",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="https://example.com/og-image.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/og-image.jpg",
		},
		{
			name: "og:image content then property",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta content="https://example.com/reversed.jpg" property="og:image">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/reversed.jpg",
		},
		{
			name: "og:image:url variant",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image:url" content="https://example.com/og-url.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/og-url.jpg",
		},
		{
			name: "twitter:image fallback",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta name="twitter:image" content="https://example.com/twitter.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/twitter.jpg",
		},
		{
			name: "twitter:image:src variant",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta name="twitter:image:src" content="https://example.com/twitter-src.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/twitter-src.jpg",
		},
		{
			name: "og:image preferred over twitter:image",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="https://example.com/og.jpg">
<meta name="twitter:image" content="https://example.com/twitter.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/og.jpg",
		},
		{
			name: "no og:image found",
			htmlContent: `<!DOCTYPE html>
<html><head><title>Test</title></head><body></body></html>`,
			statusCode:    200,
			expectedImage: "",
		},
		{
			name:          "404 response returns empty",
			htmlContent:   "Not Found",
			statusCode:    404,
			expectedImage: "",
		},
		{
			name:          "500 response returns empty",
			htmlContent:   "Internal Server Error",
			statusCode:    500,
			expectedImage: "",
		},
		{
			name: "relative URL ignored",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="/images/local.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "",
		},
		{
			name: "protocol-relative URL ignored",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="//example.com/image.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "",
		},
		{
			name: "http URL accepted",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="http://example.com/image.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "http://example.com/image.jpg",
		},
		{
			name: "single quotes in meta tag",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property='og:image' content='https://example.com/single.jpg'>
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/single.jpg",
		},
		{
			name: "extra attributes in meta tag",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta name="og:image" property="og:image" content="https://example.com/extra.jpg" data-foo="bar">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/extra.jpg",
		},
		{
			name: "whitespace around URL",
			htmlContent: `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="  https://example.com/spaces.jpg  ">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/spaces.jpg",
		},
		{
			name: "case insensitive property",
			htmlContent: `<!DOCTYPE html>
<html><head>
<META PROPERTY="OG:IMAGE" CONTENT="https://example.com/upper.jpg">
</head><body></body></html>`,
			statusCode:    200,
			expectedImage: "https://example.com/upper.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.htmlContent))
			}))
			defer server.Close()

			extractor := NewOGImageExtractor()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			got, err := extractor.ExtractOGImage(ctx, server.URL)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tt.expectedImage {
				t.Errorf("ExtractOGImage() = %q, want %q", got, tt.expectedImage)
			}
		})
	}
}

func TestExtractOGImage_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Slow response
		w.Write([]byte("delayed"))
	}))
	defer server.Close()

	extractor := NewOGImageExtractor()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := extractor.ExtractOGImage(ctx, server.URL)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestExtractOGImage_InvalidURL(t *testing.T) {
	extractor := NewOGImageExtractor()
	ctx := context.Background()

	_, err := extractor.ExtractOGImage(ctx, "not-a-valid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestExtractOGImage_UserAgent(t *testing.T) {
	var receivedUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.Write([]byte(`<html><head></head></html>`))
	}))
	defer server.Close()

	extractor := NewOGImageExtractor()
	ctx := context.Background()

	extractor.ExtractOGImage(ctx, server.URL)

	if !strings.Contains(receivedUserAgent, "PulseFeed") {
		t.Errorf("User-Agent = %q, want to contain 'PulseFeed'", receivedUserAgent)
	}
}

func TestExtractOGImage_AcceptHeader(t *testing.T) {
	var receivedAccept string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAccept = r.Header.Get("Accept")
		w.Write([]byte(`<html><head></head></html>`))
	}))
	defer server.Close()

	extractor := NewOGImageExtractor()
	ctx := context.Background()

	extractor.ExtractOGImage(ctx, server.URL)

	if receivedAccept != "text/html" {
		t.Errorf("Accept header = %q, want 'text/html'", receivedAccept)
	}
}

func TestExtractOGImage_LargeContent(t *testing.T) {
	// Create content larger than 100KB with og:image at the beginning
	htmlStart := `<!DOCTYPE html><html><head><meta property="og:image" content="https://example.com/found.jpg">`
	padding := strings.Repeat("x", 200*1024) // 200KB of padding
	htmlEnd := `</head><body>` + padding + `</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlStart + htmlEnd))
	}))
	defer server.Close()

	extractor := NewOGImageExtractor()
	ctx := context.Background()

	got, err := extractor.ExtractOGImage(ctx, server.URL)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// og:image is in the first 100KB, should be found
	if got != "https://example.com/found.jpg" {
		t.Errorf("ExtractOGImage() = %q, want %q", got, "https://example.com/found.jpg")
	}
}

func TestOGImagePatterns_Count(t *testing.T) {
	// Ensure we have the expected number of patterns
	if len(ogImagePatterns) != 8 {
		t.Errorf("ogImagePatterns count = %d, want 8", len(ogImagePatterns))
	}
}

// TestExtractOGImage_BadURLEscape covers the `http.NewRequestWithContext`
// error branch — a URL with an invalid percent escape fails URL parsing.
func TestExtractOGImage_BadURLEscape(t *testing.T) {
	extractor := NewOGImageExtractor()
	_, err := extractor.ExtractOGImage(context.Background(), "http://example.com/%ZZ")
	if err == nil {
		t.Error("expected URL parse error, got nil")
	}
}

// TestExtractOGImage_BodyReadError covers the io.ReadAll error branch — a
// server that announces Content-Length > actual bytes causes ReadAll on the
// limited reader to see an unexpected EOF.
func TestExtractOGImage_BodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support Hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("Hijack: %v", err)
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5000\r\n\r\npartial"))
		_ = conn.Close()
	}))
	defer server.Close()

	extractor := NewOGImageExtractor()
	_, err := extractor.ExtractOGImage(context.Background(), server.URL)
	if err == nil {
		t.Error("expected body read error on truncated response, got nil")
	}
}

func TestNewOGImageExtractor(t *testing.T) {
	extractor := NewOGImageExtractor()

	if extractor == nil {
		t.Fatal("NewOGImageExtractor returned nil")
	}

	if extractor.client == nil {
		t.Error("extractor.client is nil")
	}

	if extractor.client.Timeout != 10*time.Second {
		t.Errorf("client timeout = %v, want 10s", extractor.client.Timeout)
	}
}
