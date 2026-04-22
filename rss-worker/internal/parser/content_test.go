package parser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sampleArticleHTML is a realistic HTML page that go-readability can parse
const sampleArticleHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="description" content="This is a sample article excerpt for testing.">
<title>Sample Article Title</title>
</head>
<body>
<header><nav>Navigation</nav></header>
<article>
<h1>Sample Article Title</h1>
<p>This is the first paragraph of the article. It contains enough content to pass the minimum length validation. The article discusses various topics that would typically be found in a news article.</p>
<p>This is the second paragraph with more information. It provides additional context and details about the subject matter. The content continues to expand on the main theme.</p>
<p>Finally, this is the conclusion paragraph. It wraps up the discussion and provides closing thoughts. This ensures we have substantial content for extraction testing.</p>
</article>
<footer>Footer content</footer>
</body>
</html>`

func TestExtractContent_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(sampleArticleHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	result, err := extractor.ExtractContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("ExtractContent error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.TextContent == "" {
		t.Error("TextContent should not be empty")
	}

	if len(result.TextContent) < 100 {
		t.Errorf("TextContent too short: %d chars", len(result.TextContent))
	}

	// Content should include HTML
	if result.Content == "" {
		t.Error("Content (HTML) should not be empty")
	}
}

func TestExtractContent_ShortContent(t *testing.T) {
	shortHTML := `<!DOCTYPE html>
<html><head><title>Test</title></head>
<body><article><p>Short.</p></article></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(shortHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	result, err := extractor.ExtractContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result for short content")
	}
}

func TestExtractContent_404Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	result, err := extractor.ExtractContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result for 404 response")
	}
}

func TestExtractContent_500Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	result, err := extractor.ExtractContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result for 500 response")
	}
}

func TestExtractContent_InvalidURL(t *testing.T) {
	extractor := NewContentExtractor()
	ctx := context.Background()

	_, err := extractor.ExtractContent(ctx, "not-a-valid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// TestExtractContent_URLParseError covers the `url.Parse` error branch — a
// URL with an invalid percent escape triggers it before the HTTP call is made.
func TestExtractContent_URLParseError(t *testing.T) {
	extractor := NewContentExtractor()
	_, err := extractor.ExtractContent(context.Background(), "http://example.com/%ZZ")
	if err == nil {
		t.Error("expected URL parse error, got nil")
	}
}

// TestExtractContent_ReadabilityError covers the readability-failure branch:
// a server that announces Content-Length > actual bytes written causes
// io.ReadAll (inside readability.FromReader) to return unexpected EOF.
func TestExtractContent_ReadabilityError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support Hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("Hijack: %v", err)
		}
		// Promise 5000 bytes then close mid-body; readability will see
		// an unexpected EOF during ReadAll.
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5000\r\n\r\npartial body"))
		_ = conn.Close()
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	_, err := extractor.ExtractContent(context.Background(), server.URL)
	if err == nil {
		t.Error("expected readability error on truncated body, got nil")
	}
}

func TestExtractContent_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(sampleArticleHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := extractor.ExtractContent(ctx, server.URL)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestExtractContent_Headers(t *testing.T) {
	var receivedUserAgent string
	var receivedAccept string
	var receivedAcceptLanguage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		receivedAccept = r.Header.Get("Accept")
		receivedAcceptLanguage = r.Header.Get("Accept-Language")
		w.Write([]byte(sampleArticleHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	extractor.ExtractContent(ctx, server.URL)

	if !strings.Contains(receivedUserAgent, "PulseFeed") {
		t.Errorf("User-Agent = %q, want to contain 'PulseFeed'", receivedUserAgent)
	}

	if !strings.Contains(receivedAccept, "text/html") {
		t.Errorf("Accept = %q, want to contain 'text/html'", receivedAccept)
	}

	if !strings.Contains(receivedAcceptLanguage, "en") {
		t.Errorf("Accept-Language = %q, want to contain 'en'", receivedAcceptLanguage)
	}
}

func TestExtractTextContent_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleArticleHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	text, err := extractor.ExtractTextContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("ExtractTextContent error: %v", err)
	}

	if text == "" {
		t.Error("expected non-empty text content")
	}

	if len(text) < 100 {
		t.Errorf("text content too short: %d chars", len(text))
	}
}

func TestExtractTextContent_ShortContent(t *testing.T) {
	shortHTML := `<!DOCTYPE html>
<html><head><title>Test</title></head>
<body><article><p>Short.</p></article></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(shortHTML))
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	text, err := extractor.ExtractTextContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "" {
		t.Errorf("expected empty text for short content, got %q", text)
	}
}

func TestExtractTextContent_404Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	ctx := context.Background()

	text, err := extractor.ExtractTextContent(ctx, server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "" {
		t.Errorf("expected empty text for 404 response, got %q", text)
	}
}

func TestNewContentExtractor(t *testing.T) {
	extractor := NewContentExtractor()

	if extractor == nil {
		t.Fatal("NewContentExtractor returned nil")
	}

	if extractor.client == nil {
		t.Error("extractor.client is nil")
	}

	if extractor.client.Timeout != 15*time.Second {
		t.Errorf("client timeout = %v, want 15s", extractor.client.Timeout)
	}
}

func TestExtractedContent_Fields(t *testing.T) {
	// Test that ExtractedContent struct has expected fields
	ec := ExtractedContent{
		Content:     "<p>HTML content</p>",
		TextContent: "Plain text content",
		Excerpt:     "Short excerpt",
	}

	if ec.Content != "<p>HTML content</p>" {
		t.Errorf("Content field mismatch")
	}
	if ec.TextContent != "Plain text content" {
		t.Errorf("TextContent field mismatch")
	}
	if ec.Excerpt != "Short excerpt" {
		t.Errorf("Excerpt field mismatch")
	}
}
