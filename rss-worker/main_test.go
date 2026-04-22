package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/database"
	"github.com/pulsefeed/rss-worker/internal/models"
	"github.com/pulsefeed/rss-worker/internal/parser"
)

// TestMain gives the test binary a "run main() instead of tests" mode, keyed
// off the RSS_WORKER_TEST_MAIN env var. Subprocess tests re-invoke the test
// binary with that var set to exercise main() and runCleanup's Fatalf path.
func TestMain(m *testing.M) {
	switch os.Getenv("RSS_WORKER_TEST_MAIN") {
	case "main":
		// Strip -test.* flags from os.Args so main() only sees its own args.
		filtered := []string{os.Args[0]}
		for _, a := range os.Args[1:] {
			if strings.HasPrefix(a, "-test.") {
				continue
			}
			filtered = append(filtered, a)
		}
		os.Args = filtered
		main()
		return // unreached if main Fatalf's
	case "runCleanup":
		// Run runCleanup with a failing store so Fatalf's os.Exit(1) fires.
		runCleanup(context.Background(), &mockStore{cleanupErr: errors.New("forced")}, 30)
		return
	}
	os.Exit(m.Run())
}

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
	sources           []models.Source
	sourcesErr        error
	insertResult      int
	insertSkipped     int
	insertErr         error
	insertPanic       string // if non-empty, InsertArticles panics with this message
	fetchLog          *models.FetchLog
	fetchLogErr       error
	updateLogErr      error
	updateSourcesErr  error
	cleanupResult     int
	cleanupErr        error
	cleanupLogsResult int
	cleanupLogsErr    error
	ogArticles        []database.ArticleForBackfill
	ogArticlesErr     error
	contentArticles   []database.ArticleForContentBackfill
	contentErr        error
	updateImageErr    error
	updateContentErr  error
	bumpErr           error

	// bumped captures calls to BumpBackfillAttempts keyed by kind — tests
	// assert we recorded the attempted url_hashes for failed articles.
	bumpedImage   []string
	bumpedContent []string

	// fetchStateUpdates captures the final BatchUpdateSourceFetchState call
	// so tests can assert per-source circuit/ETag outcomes.
	fetchStateUpdates []database.SourceFetchState
}

func (m *mockStore) GetActiveSources() ([]models.Source, error) {
	return m.sources, m.sourcesErr
}

func (m *mockStore) InsertArticles(articles []*models.Article) (int, int, error) {
	if m.insertPanic != "" {
		panic(m.insertPanic)
	}
	return m.insertResult, m.insertSkipped, m.insertErr
}

func (m *mockStore) BatchUpdateSourceFetchState(updates []database.SourceFetchState) error {
	m.fetchStateUpdates = append(m.fetchStateUpdates, updates...)
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

func (m *mockStore) GetArticlesNeedingOGImage(limit, maxAttempts, cooldownHours int) ([]database.ArticleForBackfill, error) {
	return m.ogArticles, m.ogArticlesErr
}

func (m *mockStore) UpdateArticleImage(urlHash string, imageURL string) error {
	return m.updateImageErr
}

func (m *mockStore) GetArticlesNeedingContent(limit, maxAttempts, cooldownHours int) ([]database.ArticleForContentBackfill, error) {
	return m.contentArticles, m.contentErr
}

func (m *mockStore) UpdateArticleContent(urlHash string, content string) error {
	return m.updateContentErr
}

func (m *mockStore) BumpBackfillAttempts(urlHashes []string, kind string) error {
	switch kind {
	case "image":
		m.bumpedImage = append(m.bumpedImage, urlHashes...)
	case "content":
		m.bumpedContent = append(m.bumpedContent, urlHashes...)
	}
	return m.bumpErr
}

// --- runCleanup tests ---

func TestRunCleanup_Success(t *testing.T) {
	db := &mockStore{cleanupResult: 42}
	// runCleanup calls log.Fatalf on error, so we only test the success path
	runCleanup(context.Background(), db, 30)
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
	err := runFetch(context.Background(), db, rssParser, 5)
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
	err := runFetch(context.Background(), db, rssParser, 5)
	if err == nil {
		t.Fatal("expected error from runFetch, got nil")
	}
	if !errors.Is(err, db.sourcesErr) {
		t.Errorf("expected wrapped error containing %q, got %q", db.sourcesErr, err)
	}
}

func TestRunFetch_CreateFetchLogError(t *testing.T) {
	db := &mockStore{
		sources:     []models.Source{},
		fetchLogErr: errors.New("log creation failed"),
	}

	rssParser := parser.New()
	// Should continue despite fetch log creation failure
	err := runFetch(context.Background(), db, rssParser, 5)
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
	err := runFetch(context.Background(), db, rssParser, 5)
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
	err := runFetch(context.Background(), db, rssParser, 1)
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
	err := runFetch(context.Background(), db, rssParser, 5)
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
	err := runFetch(context.Background(), db, rssParser, 5)
	if err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
}

// --- nextCircuitOpenUntil tests (circuit breaker math) ---

func TestNextCircuitOpenUntil(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		failures  int
		threshold int
		base      int
		max       int
		wantNil   bool
		wantHours int
	}{
		{"below threshold returns nil", 4, 5, 1, 24, true, 0},
		{"at threshold uses base", 5, 5, 1, 24, false, 1},
		{"one over threshold doubles", 6, 5, 1, 24, false, 2},
		{"two over quadruples", 7, 5, 1, 24, false, 4},
		{"three over is 8x", 8, 5, 1, 24, false, 8},
		{"capped at max", 15, 5, 1, 24, false, 24},
		{"zero threshold is disabled", 5, 0, 1, 24, true, 0},
		{"zero base is disabled", 5, 5, 0, 24, true, 0},
		{"huge exponent does not overflow", 200, 5, 1, 24, false, 24},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextCircuitOpenUntil(now, c.failures, c.threshold, c.base, c.max)
			if c.wantNil {
				if got != nil {
					t.Errorf("want nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want non-nil, got nil")
			}
			want := now.Add(time.Duration(c.wantHours) * time.Hour)
			if !got.Equal(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// --- buildSourceFetchState tests ---

func TestBuildSourceFetchState_SuccessResetsCircuit(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	oldETag := `"old"`
	pastTrip := now.Add(-1 * time.Hour)
	source := models.Source{
		ID:                  "src-1",
		ETag:                &oldETag,
		ConsecutiveFailures: 3,
		CircuitOpenUntil:    &pastTrip,
	}
	result := models.FetchResult{
		Source:       source,
		ETag:         `"new"`,
		LastModified: "Mon, 01 Jan 2026 12:00:00 GMT",
	}

	state := buildSourceFetchState(source, result, now)

	if state.ConsecutiveFailures != 0 {
		t.Errorf("failures = %d, want 0", state.ConsecutiveFailures)
	}
	if state.CircuitOpenUntil != nil {
		t.Errorf("CircuitOpenUntil = %v, want nil", state.CircuitOpenUntil)
	}
	if state.ETag == nil || *state.ETag != `"new"` {
		t.Errorf("ETag = %v, want \"new\"", state.ETag)
	}
	if state.LastFetchedAt == nil || !state.LastFetchedAt.Equal(now) {
		t.Errorf("LastFetchedAt = %v, want %v", state.LastFetchedAt, now)
	}
}

func TestBuildSourceFetchState_FailurePreservesETagAndIncrements(t *testing.T) {
	origT, origB, origM := circuitFailureThreshold, circuitBaseBackoffHours, circuitMaxBackoffHours
	circuitFailureThreshold, circuitBaseBackoffHours, circuitMaxBackoffHours = 2, 1, 24
	defer func() {
		circuitFailureThreshold, circuitBaseBackoffHours, circuitMaxBackoffHours = origT, origB, origM
	}()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	preserved := `"keep-me"`
	source := models.Source{
		ID:                  "src-1",
		ETag:                &preserved,
		ConsecutiveFailures: 1, // +1 → 2 trips the threshold
	}
	result := models.FetchResult{
		Source: source,
		Error:  errors.New("network error"),
	}

	state := buildSourceFetchState(source, result, now)

	if state.ConsecutiveFailures != 2 {
		t.Errorf("failures = %d, want 2", state.ConsecutiveFailures)
	}
	if state.CircuitOpenUntil == nil {
		t.Fatal("CircuitOpenUntil = nil, want circuit trip")
	}
	want := now.Add(1 * time.Hour)
	if !state.CircuitOpenUntil.Equal(want) {
		t.Errorf("CircuitOpenUntil = %v, want %v", state.CircuitOpenUntil, want)
	}
	if state.ETag == nil || *state.ETag != `"keep-me"` {
		t.Errorf("ETag = %v, want preserved \"keep-me\"", state.ETag)
	}
	if state.LastFetchedAt != nil {
		t.Errorf("LastFetchedAt = %v, want nil (failures don't stamp)", state.LastFetchedAt)
	}
}

func TestBuildSourceFetchState_NotModifiedCountsAsSuccess(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// NotModified is just a successful fetch with no new articles. The
	// parser already populated the validator (possibly falling back to the
	// source's existing one) so the builder just records it.
	source := models.Source{ID: "src-1", ConsecutiveFailures: 3}
	result := models.FetchResult{
		Source:      source,
		NotModified: true,
		ETag:        `"v1"`,
	}

	state := buildSourceFetchState(source, result, now)

	if state.ConsecutiveFailures != 0 {
		t.Errorf("failures = %d, want 0 (304 is success)", state.ConsecutiveFailures)
	}
	if state.ETag == nil || *state.ETag != `"v1"` {
		t.Errorf("ETag = %v, want \"v1\"", state.ETag)
	}
	if state.LastFetchedAt == nil {
		t.Error("LastFetchedAt should be stamped on 304")
	}
}

// --- runBackfill tests ---

func TestRunBackfill_Success(t *testing.T) {
	var processed atomic.Int32
	var bumped []string
	cfg := backfillConfig[string]{
		name:       "test",
		kind:       "image",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit, maxAttempts, cooldownHours int) ([]string, error) {
			return []string{"a", "b", "c"}, nil
		},
		process: func(ctx context.Context, item string) bool {
			processed.Add(1)
			return item != "b" // "b" returns false (skipped)
		},
		hashOf: func(s string) string { return s },
		bumpAttempts: func(hashes []string, kind string) error {
			bumped = append(bumped, hashes...)
			return nil
		},
	}

	if err := runBackfill(context.Background(), cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}

	if got := processed.Load(); got != 3 {
		t.Errorf("processed = %d, want 3", got)
	}
	// Only "b" failed, so only it should be bumped.
	if len(bumped) != 1 || bumped[0] != "b" {
		t.Errorf("bumped = %v, want [b]", bumped)
	}
}

func TestRunBackfill_FetchError(t *testing.T) {
	cfg := backfillConfig[string]{
		name:       "test",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit, maxAttempts, cooldownHours int) ([]string, error) {
			return nil, errors.New("fetch failed")
		},
		process: func(ctx context.Context, item string) bool {
			t.Error("process should not be called on fetch error")
			return false
		},
		hashOf: func(s string) string { return s },
	}

	err := runBackfill(context.Background(), cfg)
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
		fetch: func(limit, maxAttempts, cooldownHours int) ([]int, error) {
			return []int{}, nil
		},
		process: func(ctx context.Context, item int) bool {
			t.Error("process should not be called for empty items")
			return false
		},
		hashOf: func(i int) string { return "" },
	}

	if err := runBackfill(context.Background(), cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

func TestRunBackfill_MoreWorkersThanItems(t *testing.T) {
	cfg := backfillConfig[string]{
		name:       "few-items",
		timeout:    5 * time.Second,
		limit:      100,
		maxWorkers: 10,
		fetch: func(limit, maxAttempts, cooldownHours int) ([]string, error) {
			return []string{"only-one"}, nil
		},
		process: func(ctx context.Context, item string) bool {
			return true
		},
		hashOf: func(s string) string { return s },
	}

	if err := runBackfill(context.Background(), cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

// --- runOGImageBackfill tests ---

func TestRunOGImageBackfill_EmptyList(t *testing.T) {
	db := &mockStore{
		ogArticles: []database.ArticleForBackfill{},
	}
	if err := runOGImageBackfill(context.Background(), db); err != nil {
		t.Fatalf("runOGImageBackfill returned error: %v", err)
	}
}

func TestRunOGImageBackfill_FetchError(t *testing.T) {
	db := &mockStore{
		ogArticlesErr: errors.New("db error"),
	}
	if err := runOGImageBackfill(context.Background(), db); err == nil {
		t.Fatal("expected error from runOGImageBackfill, got nil")
	}
}

// --- runContentBackfill tests ---

func TestRunContentBackfill_EmptyList(t *testing.T) {
	db := &mockStore{
		contentArticles: []database.ArticleForContentBackfill{},
	}
	if err := runContentBackfill(context.Background(), db); err != nil {
		t.Fatalf("runContentBackfill returned error: %v", err)
	}
}

func TestRunContentBackfill_FetchError(t *testing.T) {
	db := &mockStore{
		contentErr: errors.New("db error"),
	}
	if err := runContentBackfill(context.Background(), db); err == nil {
		t.Fatal("expected error from runContentBackfill, got nil")
	}
}

// --- newRunID tests ---

func TestNewRunID(t *testing.T) {
	id := newRunID()
	// Normal path: 4 random bytes → 8 hex chars.
	if len(id) != 8 {
		t.Errorf("len(newRunID()) = %d, want 8", len(id))
	}
	// Two consecutive calls should differ with overwhelming probability.
	if id == newRunID() {
		t.Error("expected distinct IDs across calls")
	}
}

// --- runCleanup additional tests ---

func TestRunCleanup_CtxAlreadyCancelled(t *testing.T) {
	db := &mockStore{
		cleanupErr: errors.New("should not be called"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Must return cleanly without invoking CleanupOldArticles; otherwise
	// the mock's error would trigger log.Fatalf and blow up the test.
	runCleanup(ctx, db, 30)
}

func TestRunCleanup_CleanupFetchLogsError(t *testing.T) {
	db := &mockStore{
		cleanupResult:  10,
		cleanupLogsErr: errors.New("fetch log cleanup failed"),
	}
	// CleanupOldFetchLogs error is non-fatal (warn-only). Should complete.
	runCleanup(context.Background(), db, 30)
}

// --- processSource NotModified branch ---

func TestProcessSource_NotModified(t *testing.T) {
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Conditional-GET: worker sends If-None-Match with the stored ETag.
		// Reply 304 so ParseFeed returns NotModified=true.
		if r.Header.Get("If-None-Match") == "" {
			t.Error("expected If-None-Match header")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer feedServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("DB should not be called on 304, got %s %s", r.Method, r.URL.Path)
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	rssParser := parser.New()
	etag := `"v1"`
	source := models.Source{
		ID:       "src-1",
		Name:     "Test",
		FeedURL:  feedServer.URL,
		Language: "en",
		IsActive: true,
		ETag:     &etag,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := processSource(ctx, db, rssParser, source)
	if result.Error != nil {
		t.Fatalf("processSource error: %v", result.Error)
	}
	if !result.NotModified {
		t.Error("expected NotModified=true")
	}
	if result.ArticlesFetched != 0 {
		t.Errorf("ArticlesFetched = %d, want 0", result.ArticlesFetched)
	}
}

// --- runFetch additional branches ---

func TestRunFetch_BatchUpdateStateError(t *testing.T) {
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid xml")) // force per-source error
	}))
	defer feedServer.Close()

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "S1", FeedURL: feedServer.URL, Language: "en", IsActive: true},
		},
		fetchLog:         &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		updateSourcesErr: errors.New("batch state update failed"),
	}

	// Should complete despite BatchUpdateSourceFetchState error (warn-only).
	if err := runFetch(context.Background(), db, parser.New(), 1); err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
	if len(db.fetchStateUpdates) == 0 {
		t.Error("expected fetch state updates to be attempted")
	}
}

func TestRunFetch_PartialFailure(t *testing.T) {
	var serverURL string
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			rss := fmt.Sprintf(`<?xml version="1.0"?><rss version="2.0"><channel><title>Good</title><item><title>A</title><link>%s/a</link></item></channel></rss>`, serverURL)
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(rss))
		default:
			w.Write([]byte("not valid xml")) // source that fails
		}
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-good", Name: "Good", FeedURL: webServer.URL + "/good", Language: "en", IsActive: true},
			{ID: "src-bad", Name: "Bad", FeedURL: webServer.URL + "/bad", Language: "en", IsActive: true},
		},
		fetchLog:     &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		insertResult: 1,
	}

	if err := runFetch(context.Background(), db, parser.New(), 1); err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
	if len(db.fetchStateUpdates) != 2 {
		t.Errorf("fetchStateUpdates = %d, want 2", len(db.fetchStateUpdates))
	}
}

// --- runBackfill additional branches ---

func TestRunBackfill_BumpAttemptsError(t *testing.T) {
	cfg := backfillConfig[string]{
		name:       "test",
		kind:       "image",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 2,
		fetch: func(limit, maxAttempts, cooldownHours int) ([]string, error) {
			return []string{"a"}, nil
		},
		process: func(ctx context.Context, item string) bool {
			return false // force failure so bumpAttempts is called
		},
		hashOf: func(s string) string { return s },
		bumpAttempts: func(hashes []string, kind string) error {
			return errors.New("bump failed")
		},
	}

	// Warn-only: bumpAttempts error must not propagate to caller.
	if err := runBackfill(context.Background(), cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

// --- runBackfill: worker hits ctx.Done() between items ---

func TestRunBackfill_CtxCancelledDuringProcessing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := backfillConfig[int]{
		name:       "ctx-cancel",
		kind:       "image",
		timeout:    5 * time.Second,
		limit:      10,
		maxWorkers: 1, // single worker for deterministic ordering
		fetch: func(limit, maxAttempts, cooldownHours int) ([]int, error) {
			return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, nil
		},
		process: func(ctx context.Context, item int) bool {
			if item == 1 {
				cancel()
				// Give cancellation a moment to propagate so the worker's
				// next select reliably observes ctx.Done().
				time.Sleep(20 * time.Millisecond)
			}
			return true
		},
		hashOf:       func(i int) string { return "" },
		bumpAttempts: func(hashes []string, kind string) error { return nil },
	}

	if err := runBackfill(ctx, cfg); err != nil {
		t.Fatalf("runBackfill returned error: %v", err)
	}
}

// --- runOGImageBackfill with real articles exercises the process closure ---

func TestRunOGImageBackfill_ProcessesArticles(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta property="og:image" content="https://example.com/og.jpg"></head></html>`))
	}))
	defer webServer.Close()

	db := &mockStore{
		ogArticles: []database.ArticleForBackfill{
			{URLHash: "hash-1", URL: webServer.URL},
		},
	}

	if err := runOGImageBackfill(context.Background(), db); err != nil {
		t.Fatalf("runOGImageBackfill returned error: %v", err)
	}
}

// --- runFetch NotModified + log branch ---

func TestRunFetch_SourceNotModified(t *testing.T) {
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer feedServer.Close()

	etag := `"v1"`
	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "S1", FeedURL: feedServer.URL, Language: "en", IsActive: true, ETag: &etag},
		},
		fetchLog: &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
	}

	if err := runFetch(context.Background(), db, parser.New(), 1); err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
	// One source processed, fetch state should record NotModified success (failures reset).
	if len(db.fetchStateUpdates) != 1 {
		t.Fatalf("fetchStateUpdates = %d, want 1", len(db.fetchStateUpdates))
	}
	if db.fetchStateUpdates[0].ConsecutiveFailures != 0 {
		t.Errorf("failures = %d, want 0 on 304", db.fetchStateUpdates[0].ConsecutiveFailures)
	}
}

// --- runFetch: GetActiveSources error AND UpdateFetchLog error (both warn paths) ---

func TestRunFetch_GetSourcesErrorAndUpdateLogError(t *testing.T) {
	db := &mockStore{
		sourcesErr:   errors.New("db down"),
		fetchLog:     &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		updateLogErr: errors.New("log update failed"),
	}

	err := runFetch(context.Background(), db, parser.New(), 5)
	if err == nil {
		t.Fatal("expected error from runFetch")
	}
}

// --- processContentBackfill: transport-level extract error ---

func TestProcessContentBackfill_ExtractTransportError(t *testing.T) {
	// Closed server → connection refused → ExtractTextContent returns err != nil
	// (non-200 status paths return "", nil, so we need a real transport error).
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := webServer.URL
	webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called on extract error")
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	contentExtractor := parser.NewContentExtractor()
	article := database.ArticleForContentBackfill{URLHash: "hash-1", URL: unreachableURL}

	if result := processContentBackfill(context.Background(), db, contentExtractor, article); result {
		t.Error("expected false (extract error), got true")
	}
}

// --- processOGImageBackfill: transport-level extract error ---

func TestProcessOGImageBackfill_ExtractTransportError(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := webServer.URL
	webServer.Close()

	dbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("DB should not be called on extract error")
	}))
	defer dbServer.Close()

	db := newTestDBClient(dbServer)
	ogExtractor := parser.NewOGImageExtractor()
	article := database.ArticleForBackfill{URLHash: "hash-1", URL: unreachableURL}

	if result := processOGImageBackfill(context.Background(), db, ogExtractor, article); result {
		t.Error("expected false (extract error), got true")
	}
}

// --- runContentBackfill with real articles exercises the process closure ---

func TestRunContentBackfill_ProcessesArticles(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Article</title></head><body><article><p>This is the full article content that is definitely long enough to be extracted by the readability parser. It needs to be well over one hundred characters to pass the length threshold check.</p></article></body></html>`))
	}))
	defer webServer.Close()

	db := &mockStore{
		contentArticles: []database.ArticleForContentBackfill{
			{URLHash: "hash-1", URL: webServer.URL},
		},
	}

	if err := runContentBackfill(context.Background(), db); err != nil {
		t.Fatalf("runContentBackfill returned error: %v", err)
	}
}

// --- newRunID fallback path (via randRead injection) ---

func TestNewRunID_RandReadFallback(t *testing.T) {
	saved := randRead
	randRead = func(b []byte) (int, error) { return 0, errors.New("rand failed") }
	defer func() { randRead = saved }()

	id := newRunID()
	// Fallback format is "ts-<nanos>"; length varies but prefix is fixed.
	if !strings.HasPrefix(id, "ts-") {
		t.Errorf("fallback ID = %q, want prefix 'ts-'", id)
	}
}

// --- processSource panic recovery ---

// TestRunFetch_RecoversFromPanic covers the deferred recover in runFetch's
// per-source goroutine. A mockStore whose InsertArticles panics forces the
// recover to wrap the panic value as a FetchResult error.
func TestRunFetch_RecoversFromPanic(t *testing.T) {
	var serverURL string
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := fmt.Sprintf(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title><item><title>A</title><link>%s/a</link></item></channel></rss>`, serverURL)
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(rss))
	}))
	defer webServer.Close()
	serverURL = webServer.URL

	db := &mockStore{
		sources: []models.Source{
			{ID: "src-1", Name: "s1", FeedURL: webServer.URL, Language: "en", IsActive: true},
		},
		fetchLog:    &models.FetchLog{ID: "log-1", Status: "running", Errors: []string{}},
		insertPanic: "boom",
	}

	// runFetch should not propagate the panic — the recover wraps it into the
	// source's FetchResult.Error, which ends up in the fetch log's errors slice.
	if err := runFetch(context.Background(), db, parser.New(), 1); err != nil {
		t.Fatalf("runFetch returned error: %v", err)
	}
	if len(db.fetchStateUpdates) != 1 {
		t.Fatalf("fetchStateUpdates = %d, want 1", len(db.fetchStateUpdates))
	}
	// The recovered panic should trip the failure counter.
	if db.fetchStateUpdates[0].ConsecutiveFailures == 0 {
		t.Error("expected failures > 0 after panic recovery")
	}
}

// --- runCleanup Fatalf via subprocess (keyed off RSS_WORKER_TEST_MAIN=runCleanup) ---

func TestRunCleanup_FatalfExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRunCleanup_FatalfExits")
	cmd.Env = append(os.Environ(), "RSS_WORKER_TEST_MAIN=runCleanup")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected non-zero exit, got %v (stderr: %s)", err, stderr.String())
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Cleanup failed") {
		t.Errorf("stderr missing Cleanup failed; got %q", stderr.String())
	}
}

// --- main() via subprocess (keyed off RSS_WORKER_TEST_MAIN=main) ---

// supabaseMockHandler returns a handler that satisfies the minimal endpoints
// main()'s commands exercise: fetch log create/update, sources listing,
// articles backfill listings, cleanup RPC, and BatchUpdateSourceFetchState.
func supabaseMockHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/sources"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/fetch_logs"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`[{"id":"log-1","status":"running","errors":[]}]`))
		case strings.Contains(r.URL.Path, "/fetch_logs"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/rpc/cleanup_old_articles"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("0"))
		case strings.Contains(r.URL.Path, "/rpc/batch_update_source_fetch_state"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`0`))
		case strings.Contains(r.URL.Path, "/articles"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

func runMainSubprocess(t *testing.T, args []string, env map[string]string) (string, string, int) {
	t.Helper()
	cmdArgs := append([]string{"-test.run=^$"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "RSS_WORKER_TEST_MAIN=main")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("unexpected error type: %v", err)
		}
	}
	return stdout.String(), stderr.String(), exit
}

func TestMainBinary_DefaultFetch(t *testing.T) {
	server := httptest.NewServer(supabaseMockHandler(t))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, nil, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	if !strings.Contains(stderr, "run_started") {
		t.Errorf("stderr missing run_started: %s", stderr)
	}
	if !strings.Contains(stderr, "run_completed") {
		t.Errorf("stderr missing run_completed: %s", stderr)
	}
}

func TestMainBinary_CleanupCommand(t *testing.T) {
	server := httptest.NewServer(supabaseMockHandler(t))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, []string{"cleanup"}, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	if !strings.Contains(stderr, "Cleanup complete") {
		t.Errorf("stderr missing Cleanup complete: %s", stderr)
	}
}

func TestMainBinary_BackfillImagesCommand(t *testing.T) {
	server := httptest.NewServer(supabaseMockHandler(t))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, []string{"backfill-images"}, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
}

func TestMainBinary_BackfillContentCommand(t *testing.T) {
	server := httptest.NewServer(supabaseMockHandler(t))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, []string{"backfill-content"}, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
}

func TestMainBinary_ConfigLoadError(t *testing.T) {
	// No SUPABASE_URL / key → config.Load returns error → Fatalf → exit 1.
	_, stderr, exit := runMainSubprocess(t, nil, map[string]string{
		"SUPABASE_URL":              "",
		"SUPABASE_SERVICE_ROLE_KEY": "",
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "Failed to load config") {
		t.Errorf("stderr missing 'Failed to load config': %s", stderr)
	}
}

func TestMainBinary_FetchError(t *testing.T) {
	// Server that always returns 500 on /sources → runFetch returns error → Fatalf → exit 1.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/fetch_logs") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`[{"id":"log-1","status":"running","errors":[]}]`))
			return
		}
		if strings.Contains(r.URL.Path, "/fetch_logs") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// /sources → 500
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, nil, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1 (stderr: %s)", exit, stderr)
	}
	if !strings.Contains(stderr, "Fetch failed") {
		t.Errorf("stderr missing 'Fetch failed': %s", stderr)
	}
}

func TestMainBinary_BackfillImagesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, []string{"backfill-images"}, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1 (stderr: %s)", exit, stderr)
	}
}

func TestMainBinary_BackfillContentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, stderr, exit := runMainSubprocess(t, []string{"backfill-content"}, map[string]string{
		"SUPABASE_URL":              server.URL,
		"SUPABASE_SERVICE_ROLE_KEY": "test-key",
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1 (stderr: %s)", exit, stderr)
	}
}
