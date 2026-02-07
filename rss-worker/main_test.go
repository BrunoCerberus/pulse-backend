package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/database"
	"github.com/pulsefeed/rss-worker/internal/models"
	"github.com/pulsefeed/rss-worker/internal/parser"
)

func newTestDBClient(server *httptest.Server) *database.Client {
	cfg := &config.Config{
		SupabaseURL: server.URL,
		SupabaseKey: "test-api-key",
	}
	return database.NewClient(cfg)
}

// --- processOGImageBackfill tests ---

func TestProcessOGImageBackfill_Success(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/new-og.jpg"></head></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	article := database.ArticleForBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processOGImageBackfill(ctx, db, ogExtractor, article)

	if !result {
		t.Error("expected true (updated), got false")
	}
}

func TestProcessOGImageBackfill_NoOGImage(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>No OG</title></head></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called when no og:image found")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	article := database.ArticleForBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processOGImageBackfill(ctx, db, ogExtractor, article)

	if result {
		t.Error("expected false (no og:image), got true")
	}
}

func TestProcessOGImageBackfill_SameImage(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/same.jpg"></head></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called when og:image matches current image")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	existingImage := "https://example.com/same.jpg"
	article := database.ArticleForBackfill{
		URLHash:  "hash-1",
		URL:      webServer.URL,
		ImageURL: &existingImage,
	}

	ctx := context.Background()
	result := processOGImageBackfill(ctx, db, ogExtractor, article)

	if result {
		t.Error("expected false (same image), got true")
	}
}

func TestProcessOGImageBackfill_ExtractError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called on extract error")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	article := database.ArticleForBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processOGImageBackfill(ctx, db, ogExtractor, article)

	if result {
		t.Error("expected false (extract error), got true")
	}
}

func TestProcessOGImageBackfill_DBUpdateError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/new-og.jpg"></head></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	article := database.ArticleForBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processOGImageBackfill(ctx, db, ogExtractor, article)

	if result {
		t.Error("expected false (DB update error), got true")
	}
}

// --- processContentBackfill tests ---

func TestProcessContentBackfill_Success(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Article</title></head><body><article><p>This is the full article content that is definitely long enough to be extracted by the readability parser. It needs to be well over one hundred characters to pass the length threshold check.</p></article></body></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	contentExtractor := parser.NewContentExtractor()
	article := database.ArticleForContentBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processContentBackfill(ctx, db, contentExtractor, article)

	if !result {
		t.Error("expected true (updated), got false")
	}
}

func TestProcessContentBackfill_EmptyContent(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><p>Short.</p></body></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called when content is empty/short")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	contentExtractor := parser.NewContentExtractor()
	article := database.ArticleForContentBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processContentBackfill(ctx, db, contentExtractor, article)

	if result {
		t.Error("expected false (empty content), got true")
	}
}

func TestProcessContentBackfill_ExtractError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called on extract error")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	contentExtractor := parser.NewContentExtractor()
	article := database.ArticleForContentBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processContentBackfill(ctx, db, contentExtractor, article)

	if result {
		t.Error("expected false (extract error), got true")
	}
}

func TestProcessContentBackfill_DBUpdateError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Article</title></head><body><article><p>This is the full article content that is definitely long enough to be extracted by the readability parser. It needs to be well over one hundred characters to pass the length threshold check.</p></article></body></html>`))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	contentExtractor := parser.NewContentExtractor()
	article := database.ArticleForContentBackfill{
		URLHash: "hash-1",
		URL:     webServer.URL,
	}

	ctx := context.Background()
	result := processContentBackfill(ctx, db, contentExtractor, article)

	if result {
		t.Error("expected false (DB update error), got true")
	}
}

// --- processSource tests ---

func TestProcessSource_Success(t *testing.T) {
	var serverURL string

	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Test Article</title>
      <link>%s/article/1</link>
    </item>
  </channel>
</rss>`, serverURL)
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(rss))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head><body><article><p>Article content long enough for extraction to work properly and pass the minimum length threshold check.</p></article></body></html>`))
		}
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusCreated)
		case "PATCH":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	rssParser := parser.New()

	source := models.Source{
		ID:       "src-1",
		Name:     "Test Source",
		FeedURL:  webServer.URL + "/feed",
		IsActive: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := processSource(ctx, db, rssParser, source)

	if result.Error != nil {
		t.Fatalf("processSource error: %v", result.Error)
	}
	if result.ArticlesFetched != 1 {
		t.Errorf("ArticlesFetched = %d, want 1", result.ArticlesFetched)
	}
	if result.ArticlesInserted != 1 {
		t.Errorf("ArticlesInserted = %d, want 1", result.ArticlesInserted)
	}
}

func TestProcessSource_ParseError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid xml"))
	}))
	defer webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	rssParser := parser.New()

	source := models.Source{
		ID:       "src-1",
		Name:     "Bad Source",
		FeedURL:  webServer.URL,
		IsActive: true,
	}

	ctx := context.Background()
	result := processSource(ctx, db, rssParser, source)

	if result.Error == nil {
		t.Error("expected error for invalid feed")
	}
}

func TestProcessSource_InsertError(t *testing.T) {
	var serverURL string

	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Test Article</title>
      <link>%s/article/1</link>
    </item>
  </channel>
</rss>`, serverURL)
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(rss))
		default:
			w.Write([]byte(`<html><head></head><body></body></html>`))
		}
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "bad request"}`))
		case "PATCH":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	rssParser := parser.New()

	source := models.Source{
		ID:       "src-1",
		Name:     "Error Source",
		FeedURL:  webServer.URL + "/feed",
		IsActive: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := processSource(ctx, db, rssParser, source)

	if result.ArticlesInserted != 0 {
		t.Errorf("ArticlesInserted = %d, want 0", result.ArticlesInserted)
	}
}
