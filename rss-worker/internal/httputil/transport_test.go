package httputil

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSharedTransport_NotNil(t *testing.T) {
	if SharedTransport == nil {
		t.Fatal("SharedTransport is nil")
	}
}

func TestSharedTransport_MaxIdleConns(t *testing.T) {
	if SharedTransport.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", SharedTransport.MaxIdleConns)
	}
}

func TestSharedTransport_MaxIdleConnsPerHost(t *testing.T) {
	if SharedTransport.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 10", SharedTransport.MaxIdleConnsPerHost)
	}
}

func TestSharedTransport_IdleConnTimeout(t *testing.T) {
	if SharedTransport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", SharedTransport.IdleConnTimeout)
	}
}

func TestSharedTransport_ForceAttemptHTTP2(t *testing.T) {
	if !SharedTransport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 = false, want true")
	}
}

func TestNewClient_Timeout(t *testing.T) {
	client := NewClient(5 * time.Second)
	if client.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", client.Timeout)
	}
}

func TestNewClient_UsesSharedTransport(t *testing.T) {
	client := NewClient(5 * time.Second)
	if client.Transport != SharedTransport {
		t.Error("client.Transport is not SharedTransport (expected same pointer)")
	}
}

func TestNewClientWithRedirectLimit_Timeout(t *testing.T) {
	client := NewClientWithRedirectLimit(7*time.Second, 3)
	if client.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v, want 7s", client.Timeout)
	}
	if client.Transport != SharedTransport {
		t.Error("Transport is not SharedTransport")
	}
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil")
	}
}

func TestNewClientWithRedirectLimit_StopsAtLimit(t *testing.T) {
	const maxRedirects = 2

	// Chain of redirects: /0 -> /1 -> /2 -> /3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/0":
			http.Redirect(w, r, "/1", http.StatusFound)
		case "/1":
			http.Redirect(w, r, "/2", http.StatusFound)
		case "/2":
			http.Redirect(w, r, "/3", http.StatusFound)
		case "/3":
			fmt.Fprint(w, "final")
		}
	}))
	defer server.Close()

	client := NewClientWithRedirectLimit(5*time.Second, maxRedirects)

	resp, err := client.Get(server.URL + "/0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// With maxRedirects=2, should stop after 2 redirects (at /2) and return that redirect response
	if resp.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d (redirect stopped)", resp.StatusCode, http.StatusFound)
	}
}

func TestNewClientWithRedirectLimit_AllowsWithinLimit(t *testing.T) {
	// One redirect: /start -> /end
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/end", http.StatusFound)
		case "/end":
			fmt.Fprint(w, "done")
		}
	}))
	defer server.Close()

	client := NewClientWithRedirectLimit(5*time.Second, 2)

	resp, err := client.Get(server.URL + "/start")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// --- RateLimitingTransport tests ---

func TestRateLimitingTransport_SerializesSameHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	// 5 rps with burst 1 → first request goes through immediately, each
	// subsequent one waits ~200ms.
	client := NewRateLimitedClient(5*time.Second, 5.0, 1, 0)

	start := time.Now()
	const n = 3
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 3 requests at 5 rps, burst 1: second waits ~200ms, third ~400ms.
	// Total should be at least ~350ms. Allow generous slop for CI jitter.
	const minWait = 300 * time.Millisecond
	if elapsed < minWait {
		t.Errorf("elapsed = %v, expected ≥ %v — rate limit not applied", elapsed, minWait)
	}
}

func TestRateLimitingTransport_DifferentHostsIndependent(t *testing.T) {
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "A")
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "B")
	}))
	defer serverB.Close()

	// 1 rps with burst 1 per host. Two hosts → both first requests land
	// immediately; total wall time should stay well under 1 second.
	client := NewRateLimitedClient(5*time.Second, 1.0, 1, 0)

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := client.Get(serverA.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	go func() {
		defer wg.Done()
		resp, err := client.Get(serverB.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	wg.Wait()
	elapsed := time.Since(start)

	// Both should complete fast (each host had burst=1 available).
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, expected <500ms — different hosts should not share a limiter", elapsed)
	}
}

func TestRateLimitingTransport_CancelShortCircuits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	// 0.5 rps with burst 1 → second request would have to wait ~2s.
	client := NewRateLimitedClient(5*time.Second, 0.5, 1, 0)

	// Burn the burst so the next request has to wait.
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("first request unexpected error: %v", err)
	}
	_ = resp.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	// Cancel before sending; Wait should return ctx.Err quickly.
	cancel()
	start := time.Now()
	_, err = client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, expected fast short-circuit on cancel", elapsed)
	}
}

func TestNewRateLimitedClient_AppliesRedirectLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			http.Redirect(w, r, "/c", http.StatusFound)
		case "/c":
			fmt.Fprint(w, "done")
		}
	}))
	defer server.Close()

	// maxRedirects=1 means we stop after the first redirect.
	client := NewRateLimitedClient(5*time.Second, 100.0, 10, 1)
	resp, err := client.Get(server.URL + "/a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusFound)
	}
}
