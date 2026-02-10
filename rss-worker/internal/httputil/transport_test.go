package httputil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
