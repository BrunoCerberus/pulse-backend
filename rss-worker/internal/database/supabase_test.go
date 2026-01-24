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
		// Verify request
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/sources") {
			t.Errorf("path = %s, want to contain /sources", r.URL.Path)
		}
		if r.URL.Query().Get("is_active") != "eq.true" {
			t.Error("expected is_active=eq.true query param")
		}

		// Verify headers
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

	if len(sources) != 2 {
		t.Errorf("got %d sources, want 2", len(sources))
	}

	if sources[0].Name != "BBC News" {
		t.Errorf("first source name = %q, want 'BBC News'", sources[0].Name)
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

func TestInsertArticle_Created(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/articles") {
			t.Errorf("path = %s, want to contain /articles", r.URL.Path)
		}
		if r.Header.Get("Prefer") != "return=minimal" {
			t.Error("expected Prefer: return=minimal header")
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := newTestClient(server)
	article := &models.Article{
		Title:       "Test Article",
		URL:         "https://example.com/test",
		URLHash:     models.HashURL("https://example.com/test"),
		SourceID:    "src-1",
		PublishedAt: time.Now(),
	}

	inserted, err := client.InsertArticle(article)

	if err != nil {
		t.Fatalf("InsertArticle error: %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true for new article")
	}
}

func TestInsertArticle_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client := newTestClient(server)
	article := &models.Article{
		Title:       "Test Article",
		URL:         "https://example.com/test",
		URLHash:     models.HashURL("https://example.com/test"),
		SourceID:    "src-1",
		PublishedAt: time.Now(),
	}

	inserted, err := client.InsertArticle(article)

	if err != nil {
		t.Fatalf("InsertArticle error: %v", err)
	}
	if inserted {
		t.Error("expected inserted=false for conflict")
	}
}

func TestInsertArticle_ConflictWithOGImage(t *testing.T) {
	patchCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(http.StatusConflict)
		} else if r.Method == "PATCH" {
			patchCalled = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := newTestClient(server)
	imageURL := "https://example.com/og-image.jpg"
	thumbURL := "https://example.com/thumb.jpg"
	article := &models.Article{
		Title:        "Test Article",
		URL:          "https://example.com/test",
		URLHash:      models.HashURL("https://example.com/test"),
		SourceID:     "src-1",
		ImageURL:     &imageURL,
		ThumbnailURL: &thumbURL,
		PublishedAt:  time.Now(),
	}

	inserted, err := client.InsertArticle(article)

	if err != nil {
		t.Fatalf("InsertArticle error: %v", err)
	}
	if inserted {
		t.Error("expected inserted=false for conflict")
	}
	if !patchCalled {
		t.Error("expected PATCH to be called for og:image update")
	}
}

func TestInsertArticle_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	article := &models.Article{
		Title: "Test",
	}

	_, err := client.InsertArticle(article)

	if err == nil {
		t.Error("expected error for bad request")
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

		// Verify body contains image_url
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

func TestInsertArticles_Batch(t *testing.T) {
	insertCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		insertCount++
		if insertCount <= 2 {
			w.WriteHeader(http.StatusCreated)
		} else {
			w.WriteHeader(http.StatusConflict)
		}
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
