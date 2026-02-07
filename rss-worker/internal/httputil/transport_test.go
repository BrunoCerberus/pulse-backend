package httputil

import (
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
