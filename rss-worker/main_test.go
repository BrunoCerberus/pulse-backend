package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
			// Batch insert returns array of inserted url_hashes
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`[{"url_hash":"abc123"}]`))
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
		Slug:     "test-source",
		FeedURL:  webServer.URL + "/feed",
		Language: "en",
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
		Language: "en",
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
			// Batch insert returns 400 error
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
		Slug:     "error-source",
		FeedURL:  webServer.URL + "/feed",
		Language: "en",
		IsActive: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := processSource(ctx, db, rssParser, source)

	if result.ArticlesInserted != 0 {
		t.Errorf("ArticlesInserted = %d, want 0", result.ArticlesInserted)
	}
}

// --- mockStore for testing runFetch, runCleanup, and backfill wrappers ---

type mockStore struct {
	sources            []models.Source
	sourcesErr         error
	insertResult       int
	insertSkipped      int
	insertErr          error
	fetchLog           *models.FetchLog
	fetchLogErr        error
	updateLogErr       error
	updateSourcesErr   error
	cleanupResult      int
	cleanupErr         error
	cleanupLogsResult  int
	cleanupLogsErr     error
	ogArticles         []database.ArticleForBackfill
	ogArticlesErr      error
	contentArticles    []database.ArticleForContentBackfill
	contentErr         error
	updateImageErr     error
	updateContentErr   error
}

func (m *mockStore) GetActiveSources() ([]models.Source, error) {
	return m.sources, m.sourcesErr
}

func (m *mockStore) InsertArticles(articles []*models.Article) (int, int, error) {
	return m.insertResult, m.insertSkipped, m.insertErr
}

func (m *mockStore) UpdateSourcesLastFetched(sourceIDs []string) error {
	return m.updateSourcesErr
}

func (m *mockStore) CreateFetchLog() (*models.FetchLog, error) {
	return m.fetchLog, m.fetchLogErr
}

func (m *mockStore) UpdateFetchLog(log *models.FetchLog) error {
	return m.updateLogErr
}

func (m *mockStore) CleanupOldArticles(daysToKeep int) (int, error) {
	return m.cleanupResult, m.cleanupErr
}

func (m *mockStore) CleanupOldFetchLogs(daysToKeep int) (int, error) {
	return m.cleanupLogsResult, m.cleanupLogsErr
}

func (m *mockStore) GetArticlesNeedingOGImage(limit int) ([]database.ArticleForBackfill, error) {
	return m.ogArticles, m.ogArticlesErr
}

func (m *mockStore) UpdateArticleImage(urlHash string, imageURL string) error {
	return m.updateImageErr
}

func (m *mockStore) GetArticlesNeedingContent(limit int) ([]database.ArticleForContentBackfill, error) {
	return m.contentArticles, m.contentErr
}

func (m *mockStore) UpdateArticleContent(urlHash string, content string) error {
	return m.updateContentErr
}

// --- runCleanup tests ---

func TestRunCleanup_Success(t *testing.T) {
	db := &mockStore{cleanupResult: 42}
	// runCleanup calls log.Fatalf on error, so we only test the success path
	runCleanup(db, 30)
	// If we reach here without panicking, the test passes
}

// --- runFetch tests ---

func TestRunFetch_Success(t *testing.T) {
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
			w.Write([]byte(`<html><head></head><body></body></html>`))
		}
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "Test Source", FeedURL: webServer.URL + "/feed", Language: "en", IsActive: true},
		},
		fetchLog:     &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		insertResult: 1,
	}

	rssParser := parser.New()
	err := runFetch(db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

func TestRunFetch_GetSourcesError(t *testing.T) {
	db := &mockStore{
		sourcesErr: errors.New("db connection failed"),
		fetchLog:   &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
	}

	rssParser := parser.New()
	err := runFetch(db, rssParser, 5)
	if err == nil {
		t.Fatal("expected error from runFetch, got nil")
	}
	if !errors.Is(err, db.sourcesErr) {
		t.Errorf("expected wrapped error containing %q, got %q", db.sourcesErr, err)
	}
}

func TestRunFetch_CreateFetchLogError(t *testing.T) {
	db := &mockStore{
		sources:    []models.Source{},
		fetchLogErr: errors.New("log creation failed"),
	}

	rssParser := parser.New()
	// Should continue despite fetch log creation failure
	err := runFetch(db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

func TestRunFetch_EmptySources(t *testing.T) {
	db := &mockStore{
		sources:  []models.Source{},
		fetchLog: &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
	}

	rssParser := parser.New()
	err := runFetch(db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

func TestRunFetch_MultipleSources(t *testing.T) {
	var serverURL string

	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Feed</title>
    <item>
      <title>Article</title>
      <link>%s/article/%s</link>
    </item>
  </channel>
</rss>`, serverURL, r.URL.Path)
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss))
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "Source 1", FeedURL: webServer.URL + "/feed1", Language: "en", IsActive: true},
			{ID: "src-2", Name: "Source 2", FeedURL: webServer.URL + "/feed2", Language: "en", IsActive: true},
		},
		fetchLog:     &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		insertResult: 1,
	}

	rssParser := parser.New()
	// Use maxConcurrent=1 to avoid triggering gofeed's lazy-init race
	// (gofeed.Parser.httpClient() is not goroutine-safe on first use)
	err := runFetch(db, rssParser, 1)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

func TestRunFetch_SourceParseError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid xml"))
	}))
	defer webServer.Close()

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "Bad Source", FeedURL: webServer.URL, Language: "en", IsActive: true},
		},
		fetchLog: &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
	}

	rssParser := parser.New()
	// runFetch should succeed even if individual sources fail
	err := runFetch(db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

func TestRunFetch_UpdateFetchLogError(t *testing.T) {
	db := &mockStore{
		sources:      []models.Source{},
		fetchLog:     &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		updateLogErr: errors.New("log update failed"),
	}

	rssParser := parser.New()
	// Should complete without error despite log update failure
	err := runFetch(db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

// --- runBackfill tests ---

func TestRunBackfill_Success(t *testing.T) {
	var processed atomic.Int32
	cfg := backfillConfig[string]{
		name:       "test",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit int) ([]string, error) {
			return []string{"a", "b", "c"}, nil
		},
		process: func(ctx context.Context, item string) bool {
			processed.Add(1)
			return item != "b" // "b" returns false (skipped)
		},
	}

	if err := runBackfill(cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}

	if got := processed.Load(); got != 3 {
		t.Errorf("processed = %d, want 3", got)
	}
}

func TestRunBackfill_FetchError(t *testing.T) {
	cfg := backfillConfig[string]{
		name:       "test",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit int) ([]string, error) {
			return nil, errors.New("fetch failed")
		},
		process: func(ctx context.Context, item string) bool {
			t.Error("process should not be called on fetch error")
			return false
		},
	}

	err := runBackfill(cfg)
	if err == nil {
		t.Fatal("expected error from runBackfill, got nil")
	}
}

func TestRunBackfill_EmptyItems(t *testing.T) {
	cfg := backfillConfig[int]{
		name:       "empty",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit int) ([]int, error) {
			return []int{}, nil
		},
		process: func(ctx context.Context, item int) bool {
			t.Error("process should not be called for empty items")
			return false
		},
	}

	if err := runBackfill(cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

func TestRunBackfill_MoreWorkersThanItems(t *testing.T) {
	cfg := backfillConfig[string]{
		name:       "few-items",
		timeout:    5 * time.Second,
		limit:      100,
		maxWorkers: 10,
		fetch: func(limit int) ([]string, error) {
			return []string{"only-one"}, nil
		},
		process: func(ctx context.Context, item string) bool {
			return true
		},
	}

	if err := runBackfill(cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

// --- runOGImageBackfill tests ---

func TestRunOGImageBackfill_EmptyList(t *testing.T) {
	db := &mockStore{
		ogArticles: []database.ArticleForBackfill{},
	}
	if err := runOGImageBackfill(db); err != nil {
		t.Fatalf("runOGImageBackfill returned error: %v", err)
	}
}

func TestRunOGImageBackfill_FetchError(t *testing.T) {
	db := &mockStore{
		ogArticlesErr: errors.New("db error"),
	}
	if err := runOGImageBackfill(db); err == nil {
		t.Fatal("expected error from runOGImageBackfill, got nil")
	}
}

// --- runContentBackfill tests ---

func TestRunContentBackfill_EmptyList(t *testing.T) {
	db := &mockStore{
		contentArticles: []database.ArticleForContentBackfill{},
	}
	if err := runContentBackfill(db); err != nil {
		t.Fatalf("runContentBackfill returned error: %v", err)
	}
}

func TestRunContentBackfill_FetchError(t *testing.T) {
	db := &mockStore{
		contentErr: errors.New("db error"),
	}
	if err := runContentBackfill(db); err == nil {
		t.Fatal("expected error from runContentBackfill, got nil")
	}
}
