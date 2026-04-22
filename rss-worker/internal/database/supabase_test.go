package database

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/models"
)

// withFailingJSONMarshal swaps the package-level jsonMarshal for the duration
// of fn, returning the sentinel error each call. Lets tests exercise the
// defensive marshal-error branches.
func withFailingJSONMarshal(t *testing.T, fn func()) {
	t.Helper()
	saved := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal failed") }
	defer func() { jsonMarshal = saved }()
	fn()
}

func newTestClient(server *httptest.Server) *Client {
	cfg := &config.Config{
		SupabaseURL: server.URL,
		SupabaseKey: "test-api-key",
	}
	return NewClient(cfg)
}

func TestNewClient(t *testing.T) {
	cfg := &config.Config{
		SupabaseURL: "https://test.supabase.co",
		SupabaseKey: "test-key",
	}

	client := NewClient(cfg)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	expectedBaseURL := "https://test.supabase.co/rest/v1"
	if client.baseURL != expectedBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, expectedBaseURL)
	}

	if client.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", client.apiKey, "test-key")
	}

	if client.httpClient == nil {
		t.Error("httpClient is nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("httpClient timeout = %v, want 30s", client.httpClient.Timeout)
	}
}

func TestGetActiveSources_Success(t *testing.T) {
	expectedSources := []models.Source{
		{ID: "src-1", Name: "BBC News", FeedURL: "https://bbc.com/feed", IsActive: true},
		{ID: "src-2", Name: "TechCrunch", FeedURL: "https://techcrunch.com/feed", IsActive: true},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/sources") {
			t.Errorf("path = %s, want to contain /sources", r.URL.Path)
		}
		if r.URL.Query().Get("is_active") != "eq.true" {
			t.Error("expected is_active=eq.true query param")
		}
		if r.URL.Query().Get("select") != "*,categories(name,slug)" {
			t.Errorf("expected select=*,categories(name,slug), got %q", r.URL.Query().Get("select"))
		}
		// Circuit breaker filter (migration 019): skip sources whose
		// circuit_open_until is still in the future.
		or := r.URL.Query().Get("or")
		if !strings.Contains(or, "circuit_open_until.is.null") {
			t.Errorf("expected or filter to contain circuit_open_until.is.null, got %q", or)
		}
		if !strings.Contains(or, "circuit_open_until.lt.") {
			t.Errorf("expected or filter to contain circuit_open_until.lt., got %q", or)
		}

		if r.Header.Get("apikey") != "test-api-key" {
			t.Error("missing apikey header")
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Error("missing Authorization header")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedSources)
	}))
	defer server.Close()

	client := newTestClient(server)
	sources, err := client.GetActiveSources()

	if err != nil {
		t.Fatalf("GetActiveSources error: %v", err)
	}

	// Both sources have nil LastFetched, so ShouldFetch() returns true
	if len(sources) != 2 {
		t.Errorf("got %d sources, want 2", len(sources))
	}

	if sources[0].Name != "BBC News" {
		t.Errorf("first source name = %q, want 'BBC News'", sources[0].Name)
	}
}

func TestGetActiveSources_FiltersByInterval(t *testing.T) {
	recentFetch := time.Now().Add(-1 * time.Hour) // 1 hour ago
	allSources := []models.Source{
		{ID: "src-1", Name: "Fresh", IsActive: true, FetchIntervalHours: 2},                             // nil LastFetched → should fetch
		{ID: "src-2", Name: "Recent", IsActive: true, FetchIntervalHours: 6, LastFetched: &recentFetch}, // 1h ago, interval=6h → skip
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(allSources)
	}))
	defer server.Close()

	client := newTestClient(server)
	sources, err := client.GetActiveSources()

	if err != nil {
		t.Fatalf("GetActiveSources error: %v", err)
	}

	if len(sources) != 1 {
		t.Errorf("got %d sources, want 1 (filtered by interval)", len(sources))
	}

	if len(sources) > 0 && sources[0].Name != "Fresh" {
		t.Errorf("expected 'Fresh' source, got %q", sources[0].Name)
	}
}

func TestGetActiveSources_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "database error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.GetActiveSources()

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestInsertArticles_BatchInsert(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
			return
		}
		if !strings.Contains(r.URL.Path, "/articles") {
			t.Errorf("path = %s, want to contain /articles", r.URL.Path)
		}
		if !strings.Contains(r.URL.String(), "on_conflict=url_hash") {
			t.Error("expected on_conflict=url_hash query param")
		}
		if r.Header.Get("Prefer") != "resolution=ignore-duplicates,return=representation" {
			t.Errorf("Prefer = %q, want resolution=ignore-duplicates,return=representation", r.Header.Get("Prefer"))
		}

		// Verify body is a JSON array
		body, _ := io.ReadAll(r.Body)
		var articles []json.RawMessage
		if err := json.Unmarshal(body, &articles); err != nil {
			t.Errorf("expected JSON array body, got: %s", string(body))
		}

		// Return 2 of 3 as inserted (one was duplicate)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`[{"url_hash":"hash1"},{"url_hash":"hash2"}]`))
	}))
	defer server.Close()

	client := newTestClient(server)
	articles := []*models.Article{
		{Title: "Article 1", URL: "https://example.com/1", URLHash: "hash1"},
		{Title: "Article 2", URL: "https://example.com/2", URLHash: "hash2"},
		{Title: "Article 3", URL: "https://example.com/3", URLHash: "hash3"},
	}

	inserted, skipped, err := client.InsertArticles(articles)

	if err != nil {
		t.Fatalf("InsertArticles error: %v", err)
	}
	if inserted != 2 {
		t.Errorf("inserted = %d, want 2", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

func TestInsertArticles_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP calls expected for empty list")
	}))
	defer server.Close()

	client := newTestClient(server)
	inserted, skipped, err := client.InsertArticles(nil)

	if err != nil {
		t.Fatalf("InsertArticles error: %v", err)
	}
	if inserted != 0 || skipped != 0 {
		t.Errorf("expected 0/0, got inserted=%d, skipped=%d", inserted, skipped)
	}
}

func TestInsertArticles_BatchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/articles") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "bad request"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(server)
	articles := []*models.Article{
		{Title: "Article 1", URL: "https://example.com/1", URLHash: "hash1"},
	}

	inserted, skipped, err := client.InsertArticles(articles)

	// InsertArticles now returns accumulated batch errors
	if err == nil {
		t.Fatal("expected error from InsertArticles, got nil")
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

func TestInsertArticles_WithOGImageUpdate(t *testing.T) {
	rpcCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rpc/batch_update_article_images") {
			rpcCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("1"))
			return
		}
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/articles") {
			// Return empty array = no inserts (all duplicates)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(server)
	imageURL := "https://example.com/og-image.jpg"
	thumbURL := "https://example.com/thumb.jpg"
	articles := []*models.Article{
		{
			Title:        "Test Article",
			URL:          "https://example.com/test",
			URLHash:      "hash1",
			ImageURL:     &imageURL,
			ThumbnailURL: &thumbURL,
		},
	}

	inserted, skipped, err := client.InsertArticles(articles)

	if err != nil {
		t.Fatalf("InsertArticles error: %v", err)
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if !rpcCalled {
		t.Error("expected batch_update_article_images RPC to be called")
	}
}

func TestBatchUpdateArticleImages_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/rpc/batch_update_article_images") {
			t.Errorf("path = %s, want /rpc/batch_update_article_images", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "url_hash") {
			t.Error("expected url_hash in request body")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("2"))
	}))
	defer server.Close()

	client := newTestClient(server)
	updates := []ImageUpdate{
		{URLHash: "hash1", ImageURL: "https://example.com/img1.jpg"},
		{URLHash: "hash2", ImageURL: "https://example.com/img2.jpg"},
	}

	err := client.BatchUpdateArticleImages(updates)
	if err != nil {
		t.Errorf("BatchUpdateArticleImages error: %v", err)
	}
}

func TestBatchUpdateArticleImages_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP calls expected for empty updates")
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.BatchUpdateArticleImages(nil)
	if err != nil {
		t.Errorf("BatchUpdateArticleImages error: %v", err)
	}
}

func TestUpdateArticleImage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if !strings.Contains(r.URL.String(), "url_hash=eq.") {
			t.Error("expected url_hash filter in URL")
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "image_url") {
			t.Error("expected image_url in request body")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateArticleImage("test-hash", "https://example.com/new-image.jpg")

	if err != nil {
		t.Errorf("UpdateArticleImage error: %v", err)
	}
}

func TestUpdateArticleImage_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "database error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateArticleImage("test-hash", "https://example.com/image.jpg")

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCleanupOldArticles_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/rpc/cleanup_old_articles") {
			t.Errorf("path = %s, want to contain /rpc/cleanup_old_articles", r.URL.Path)
		}

		// Verify body contains days_to_keep
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "days_to_keep") {
			t.Error("expected days_to_keep in request body")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("42"))
	}))
	defer server.Close()

	client := newTestClient(server)
	deleted, err := client.CleanupOldArticles(30)

	if err != nil {
		t.Fatalf("CleanupOldArticles error: %v", err)
	}
	if deleted != 42 {
		t.Errorf("deleted = %d, want 42", deleted)
	}
}

func TestCleanupOldArticles_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "database error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.CleanupOldArticles(30)

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestGetArticlesNeedingOGImage_Success(t *testing.T) {
	expectedArticles := []ArticleForBackfill{
		{URLHash: "hash1", URL: "https://example.com/1"},
		{URLHash: "hash2", URL: "https://example.com/2"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.String(), "limit=") {
			t.Error("expected limit param in URL")
		}
		// Attempt cap + cooldown should be present in the filter.
		if !strings.Contains(r.URL.String(), "image_backfill_attempts.lt.3") {
			t.Errorf("expected image_backfill_attempts.lt.3 filter, got %s", r.URL.String())
		}
		if !strings.Contains(r.URL.String(), "image_backfill_last_attempt_at") {
			t.Errorf("expected image_backfill_last_attempt_at in filter, got %s", r.URL.String())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedArticles)
	}))
	defer server.Close()

	client := newTestClient(server)
	articles, err := client.GetArticlesNeedingOGImage(500, 3, 24)

	if err != nil {
		t.Fatalf("GetArticlesNeedingOGImage error: %v", err)
	}

	if len(articles) != 2 {
		t.Errorf("got %d articles, want 2", len(articles))
	}
}

func TestGetArticlesNeedingContent_Success(t *testing.T) {
	expectedArticles := []ArticleForContentBackfill{
		{URLHash: "hash1", URL: "https://example.com/1"},
		{URLHash: "hash2", URL: "https://example.com/2"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.String(), "content_backfill_attempts.lt.5") {
			t.Errorf("expected content_backfill_attempts.lt.5 filter, got %s", r.URL.String())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedArticles)
	}))
	defer server.Close()

	client := newTestClient(server)
	articles, err := client.GetArticlesNeedingContent(200, 5, 12)

	if err != nil {
		t.Fatalf("GetArticlesNeedingContent error: %v", err)
	}

	if len(articles) != 2 {
		t.Errorf("got %d articles, want 2", len(articles))
	}
}

func TestBumpBackfillAttempts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/rpc/bump_backfill_attempts") {
			t.Errorf("path = %s, want /rpc/bump_backfill_attempts", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["kind"] != "image" {
			t.Errorf("kind = %v, want image", payload["kind"])
		}
		hashes, ok := payload["url_hashes"].([]any)
		if !ok || len(hashes) != 2 {
			t.Errorf("url_hashes = %v, want 2 elements", payload["url_hashes"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("1"))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.BumpBackfillAttempts([]string{"h1", "h2"}, "image"); err != nil {
		t.Fatalf("BumpBackfillAttempts error: %v", err)
	}
}

func TestBumpBackfillAttempts_EmptyListIsNoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit for empty list")
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.BumpBackfillAttempts(nil, "image"); err != nil {
		t.Fatalf("BumpBackfillAttempts returned error for empty list: %v", err)
	}
}

func TestUpdateArticleContent_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "content") {
			t.Error("expected content in request body")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateArticleContent("test-hash", "Article content here")

	if err != nil {
		t.Errorf("UpdateArticleContent error: %v", err)
	}
}

func TestCreateFetchLog_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/fetch_logs") {
			t.Errorf("path = %s, want to contain /fetch_logs", r.URL.Path)
		}
		if r.Header.Get("Prefer") != "return=representation" {
			t.Error("expected Prefer: return=representation header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`[{"id": "log-123", "status": "running"}]`))
	}))
	defer server.Close()

	client := newTestClient(server)
	log, err := client.CreateFetchLog()

	if err != nil {
		t.Fatalf("CreateFetchLog error: %v", err)
	}

	if log == nil {
		t.Fatal("expected non-nil log")
	}

	if log.ID != "log-123" {
		t.Errorf("log.ID = %q, want 'log-123'", log.ID)
	}
}

func TestUpdateFetchLog_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if !strings.Contains(r.URL.String(), "id=eq.log-123") {
			t.Error("expected id filter in URL")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(server)
	log := &models.FetchLog{
		ID:     "log-123",
		Status: "completed",
	}

	err := client.UpdateFetchLog(log)

	if err != nil {
		t.Errorf("UpdateFetchLog error: %v", err)
	}
}

func TestBatchUpdateSourceFetchState_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/rpc/batch_update_source_fetch_state") {
			t.Errorf("path = %s, want /rpc/batch_update_source_fetch_state", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		updates, ok := payload["updates"].([]any)
		if !ok || len(updates) != 2 {
			t.Fatalf("updates = %v, want 2 elements", payload["updates"])
		}
		// The first row: ETag captured, failures reset, circuit cleared.
		first, _ := updates[0].(map[string]any)
		if first["id"] != "src-1" {
			t.Errorf("updates[0].id = %v, want src-1", first["id"])
		}
		if first["etag"] != `"abc"` {
			t.Errorf("updates[0].etag = %v, want quoted abc", first["etag"])
		}
		if first["consecutive_failures"] != float64(0) {
			t.Errorf("updates[0].consecutive_failures = %v, want 0", first["consecutive_failures"])
		}
		if first["circuit_open_until"] != nil {
			t.Errorf("updates[0].circuit_open_until = %v, want null", first["circuit_open_until"])
		}
		// The second row: failure path — non-nil circuit_open_until, preserved etag.
		second, _ := updates[1].(map[string]any)
		if second["consecutive_failures"] != float64(6) {
			t.Errorf("updates[1].consecutive_failures = %v, want 6", second["consecutive_failures"])
		}
		if second["circuit_open_until"] == nil {
			t.Error("updates[1].circuit_open_until should be non-null on failure trip")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("2"))
	}))
	defer server.Close()

	client := newTestClient(server)

	etag := `"abc"`
	now := time.Now().UTC()
	circuitUntil := now.Add(2 * time.Hour)
	preservedETag := `"preserved"`
	updates := []SourceFetchState{
		{
			ID:                  "src-1",
			ETag:                &etag,
			ConsecutiveFailures: 0,
			CircuitOpenUntil:    nil,
			LastFetchedAt:       &now,
		},
		{
			ID:                  "src-2",
			ETag:                &preservedETag,
			ConsecutiveFailures: 6,
			CircuitOpenUntil:    &circuitUntil,
		},
	}

	if err := client.BatchUpdateSourceFetchState(updates); err != nil {
		t.Fatalf("BatchUpdateSourceFetchState error: %v", err)
	}
}

func TestBatchUpdateSourceFetchState_EmptyListIsNoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP calls expected for empty updates")
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.BatchUpdateSourceFetchState(nil); err != nil {
		t.Errorf("BatchUpdateSourceFetchState(nil) error: %v", err)
	}
}

func TestBatchUpdateSourceFetchState_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "rpc error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.BatchUpdateSourceFetchState([]SourceFetchState{{ID: "src-1"}})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCleanupOldFetchLogs_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/fetch_logs") {
			t.Errorf("path = %s, want to contain /fetch_logs", r.URL.Path)
		}
		if !strings.Contains(r.URL.String(), "started_at=lt.") {
			t.Error("expected started_at date filter in URL")
		}
		if r.Header.Get("Prefer") != "return=representation" {
			t.Error("expected Prefer: return=representation header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"log-1"},{"id":"log-2"},{"id":"log-3"}]`))
	}))
	defer server.Close()

	client := newTestClient(server)
	deleted, err := client.CleanupOldFetchLogs(30)

	if err != nil {
		t.Fatalf("CleanupOldFetchLogs error: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}
}

func TestCleanupOldFetchLogs_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "database error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.CleanupOldFetchLogs(30)

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestDoWithRetry_RetriesOnTransientError(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": "service unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]models.Source{
			{ID: "src-1", Name: "Test", IsActive: true},
		})
	}))
	defer server.Close()

	client := newTestClient(server)
	sources, err := client.GetActiveSources()

	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("got %d sources, want 1", len(sources))
	}
	if attempt != 3 {
		t.Errorf("expected 3 attempts, got %d", attempt)
	}
}

func TestIsRetryable(t *testing.T) {
	retryable := []int{429, 502, 503, 504}
	for _, code := range retryable {
		if !isRetryable(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}

	nonRetryable := []int{200, 201, 400, 401, 403, 404, 409, 500}
	for _, code := range nonRetryable {
		if isRetryable(code) {
			t.Errorf("expected %d to NOT be retryable", code)
		}
	}
}

func TestGetActiveSources_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.GetActiveSources(); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestBatchUpdateArticleImages_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "rpc error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.BatchUpdateArticleImages([]ImageUpdate{{URLHash: "h1", ImageURL: "http://x/i.jpg"}})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCreateFetchLog_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.CreateFetchLog(); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCreateFetchLog_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.CreateFetchLog(); err == nil {
		t.Error("expected error when no fetch log returned")
	}
}

func TestCreateFetchLog_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.CreateFetchLog(); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestUpdateFetchLog_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateFetchLog(&models.FetchLog{ID: "log-1", Status: "completed"})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestUpdateArticleContent_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.UpdateArticleContent("hash-1", "content"); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestGetArticlesNeedingOGImage_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.GetArticlesNeedingOGImage(500, 3, 24); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestGetArticlesNeedingOGImage_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.GetArticlesNeedingOGImage(500, 3, 24); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestGetArticlesNeedingContent_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "db error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.GetArticlesNeedingContent(200, 5, 12); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestGetArticlesNeedingContent_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.GetArticlesNeedingContent(200, 5, 12); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestBumpBackfillAttempts_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "rpc error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.BumpBackfillAttempts([]string{"h1"}, "image"); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestCleanupOldArticles_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-a-number`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.CleanupOldArticles(30); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestCleanupOldFetchLogs_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if _, err := client.CleanupOldFetchLogs(30); err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestDoWithRetry_ExhaustsRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.GetActiveSources()
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	// maxRetries=3 means 4 total attempts (initial + 3 retries).
	if attempts != 4 {
		t.Errorf("attempts = %d, want 4", attempts)
	}
}

func TestDoWithRetry_TransportError(t *testing.T) {
	// Start a server then close it so Do() returns a connection error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := newTestClient(server)
	if _, err := client.GetActiveSources(); err == nil {
		t.Error("expected transport error, got nil")
	}
}

// newBadURLClient returns a client whose baseURL contains an invalid percent
// escape, so any subsequent `http.NewRequest`/`doWithRetry` call fails URL
// parsing. This exercises the NewRequest error branches in every method.
func newBadURLClient() *Client {
	return NewClient(&config.Config{
		SupabaseURL: "http://example.com/%ZZ",
		SupabaseKey: "test-api-key",
	})
}

// newUnreachableClient points at a server that's already been closed, so the
// first call produces a transport (connection refused) error. This exercises
// the httpClient.Do error branches.
func newUnreachableClient(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c := newTestClient(server)
	server.Close()
	return c
}

// --- http.NewRequest bad-URL branches ---

func TestUpdateArticleImage_BadURL(t *testing.T) {
	c := newBadURLClient()
	if err := c.UpdateArticleImage("h1", "http://x/i.jpg"); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestUpdateArticleContent_BadURL(t *testing.T) {
	c := newBadURLClient()
	if err := c.UpdateArticleContent("h1", "body"); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestCreateFetchLog_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.CreateFetchLog(); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestUpdateFetchLog_BadURL(t *testing.T) {
	c := newBadURLClient()
	if err := c.UpdateFetchLog(&models.FetchLog{ID: "log-1"}); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestCleanupOldArticles_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.CleanupOldArticles(30); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestCleanupOldFetchLogs_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.CleanupOldFetchLogs(30); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestGetArticlesNeedingOGImage_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.GetArticlesNeedingOGImage(10, 3, 24); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

func TestGetArticlesNeedingContent_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.GetArticlesNeedingContent(10, 3, 24); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

// doWithRetry also has a NewRequest branch (line 435-437); GetActiveSources
// uses doWithRetry, so a bad baseURL exercises that path.
func TestGetActiveSources_BadURL(t *testing.T) {
	c := newBadURLClient()
	if _, err := c.GetActiveSources(); err == nil {
		t.Error("expected bad-URL error, got nil")
	}
}

// --- httpClient.Do transport-error branches (distinct from 5xx retries) ---

func TestUpdateArticleImage_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if err := c.UpdateArticleImage("h1", "http://x/i.jpg"); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestUpdateArticleContent_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if err := c.UpdateArticleContent("h1", "body"); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestCreateFetchLog_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if _, err := c.CreateFetchLog(); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestUpdateFetchLog_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if err := c.UpdateFetchLog(&models.FetchLog{ID: "log-1"}); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestCleanupOldArticles_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if _, err := c.CleanupOldArticles(30); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestCleanupOldFetchLogs_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if _, err := c.CleanupOldFetchLogs(30); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestGetArticlesNeedingOGImage_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if _, err := c.GetArticlesNeedingOGImage(10, 3, 24); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestGetArticlesNeedingContent_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if _, err := c.GetArticlesNeedingContent(10, 3, 24); err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestInsertArticles_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	_, _, err := c.InsertArticles([]*models.Article{{URLHash: "h", URL: "u"}})
	if err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestBatchUpdateArticleImages_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	err := c.BatchUpdateArticleImages([]ImageUpdate{{URLHash: "h", ImageURL: "u"}})
	if err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestBatchUpdateSourceFetchState_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	err := c.BatchUpdateSourceFetchState([]SourceFetchState{{ID: "s1"}})
	if err == nil {
		t.Error("expected transport error, got nil")
	}
}

func TestBumpBackfillAttempts_TransportError(t *testing.T) {
	c := newUnreachableClient(t)
	if err := c.BumpBackfillAttempts([]string{"h1"}, "image"); err == nil {
		t.Error("expected transport error, got nil")
	}
}

// --- insertArticleBatch decode-error branch ---

func TestInsertArticles_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// Return non-JSON so the decode step fails.
		w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	c := newTestClient(server)
	_, _, err := c.InsertArticles([]*models.Article{{URLHash: "h", URL: "u"}})
	if err == nil {
		t.Error("expected decode error, got nil")
	}
}

// --- InsertArticles batch-image-update warn branch (line 153) ---

// TestInsertArticles_BatchImageUpdateError covers the warn-only path where
// BatchUpdateArticleImages fails after a successful insert — InsertArticles
// should still return nil error (image update is best-effort).
func TestInsertArticles_BatchImageUpdateError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rpc/batch_update_article_images") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "rpc error"}`))
			return
		}
		// POST /articles: return empty inserted list so the caller falls
		// through to the image-update step for the duplicate article.
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/articles") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(server)
	imageURL := "https://example.com/og.jpg"
	thumb := "https://example.com/thumb.jpg"
	articles := []*models.Article{{
		URLHash:      "h1",
		URL:          "https://example.com/a",
		ImageURL:     &imageURL,
		ThumbnailURL: &thumb,
	}}
	// The outer InsertArticles error should be nil — the image update failure
	// is logged via Warnf but not propagated.
	if _, _, err := c.InsertArticles(articles); err != nil {
		t.Errorf("expected nil error from InsertArticles, got %v", err)
	}
}

// --- jsonMarshal error branches (unreachable with real payloads) ---

func TestInsertArticles_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient() // never reached
		_, _, err := c.InsertArticles([]*models.Article{{URLHash: "h", URL: "u"}})
		if err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestBatchUpdateArticleImages_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.BatchUpdateArticleImages([]ImageUpdate{{URLHash: "h", ImageURL: "u"}}); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestUpdateArticleImage_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.UpdateArticleImage("h", "u"); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestBatchUpdateSourceFetchState_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.BatchUpdateSourceFetchState([]SourceFetchState{{ID: "s1"}}); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestCreateFetchLog_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if _, err := c.CreateFetchLog(); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestUpdateFetchLog_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.UpdateFetchLog(&models.FetchLog{ID: "log-1"}); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestCleanupOldArticles_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if _, err := c.CleanupOldArticles(30); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestBumpBackfillAttempts_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.BumpBackfillAttempts([]string{"h"}, "image"); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestUpdateArticleContent_MarshalError(t *testing.T) {
	withFailingJSONMarshal(t, func() {
		c := newBadURLClient()
		if err := c.UpdateArticleContent("h", "body"); err == nil {
			t.Error("expected marshal error, got nil")
		}
	})
}

func TestSetHeaders(t *testing.T) {
	cfg := &config.Config{
		SupabaseURL: "https://test.supabase.co",
		SupabaseKey: "my-api-key",
	}
	client := NewClient(cfg)

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	client.setHeaders(req)

	if req.Header.Get("apikey") != "my-api-key" {
		t.Errorf("apikey header = %q, want 'my-api-key'", req.Header.Get("apikey"))
	}

	if req.Header.Get("Authorization") != "Bearer my-api-key" {
		t.Errorf("Authorization header = %q, want 'Bearer my-api-key'", req.Header.Get("Authorization"))
	}

	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type header = %q, want 'application/json'", req.Header.Get("Content-Type"))
	}
}
