// Package httputil provides a shared HTTP transport with tuned connection pooling.
//
// Using a shared transport across all HTTP clients (OG image extractor, content
// extractor, database client) enables connection reuse when multiple workers hit
// the same hosts, avoiding constant connection teardown and recreation.
package httputil

import (
	"net"
	"net/http"
	"time"
)

// SharedTransport is a shared http.Transport with tuned connection pool settings.
// It is safe for concurrent use across all HTTP clients.
var SharedTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
	ForceAttemptHTTP2:   true,
}

// NewClient creates an http.Client using the shared transport with the given timeout.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: SharedTransport,
	}
}
