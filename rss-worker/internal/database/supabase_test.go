package database

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/models"
)

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
		{ID: "src-1", Name: "Fresh", IsActive: true, FetchIntervalHours: 2},                        // nil LastFetched → should fetch
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

	// InsertArticles logs errors but doesn't return them
	if err != nil {
		t.Fatalf("InsertArticles error: %v", err)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedArticles)
	}))
	defer server.Close()

	client := newTestClient(server)
	articles, err := client.GetArticlesNeedingOGImage(500)

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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedArticles)
	}))
	defer server.Close()

	client := newTestClient(server)
	articles, err := client.GetArticlesNeedingContent(200)

	if err != nil {
		t.Fatalf("GetArticlesNeedingContent error: %v", err)
	}

	if len(articles) != 2 {
		t.Errorf("got %d articles, want 2", len(articles))
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

func TestUpdateSourcesLastFetched_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if !strings.Contains(r.URL.String(), "id=in.") {
			t.Errorf("expected id=in. in URL, got %s", r.URL.String())
		}
		if !strings.Contains(r.URL.String(), "src-1") || !strings.Contains(r.URL.String(), "src-2") {
			t.Error("expected both source IDs in URL")
		}

		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "last_fetched_at") {
			t.Error("expected last_fetched_at in request body")
		}
		if !strings.Contains(bodyStr, "updated_at") {
			t.Error("expected updated_at in request body")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateSourcesLastFetched([]string{"src-1", "src-2"})

	if err != nil {
		t.Errorf("UpdateSourcesLastFetched error: %v", err)
	}
}

func TestUpdateSourcesLastFetched_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP calls expected for empty IDs")
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateSourcesLastFetched(nil)

	if err != nil {
		t.Errorf("UpdateSourcesLastFetched error: %v", err)
	}
}

func TestUpdateSourcesLastFetched_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "database error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.UpdateSourcesLastFetched([]string{"src-123"})

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
