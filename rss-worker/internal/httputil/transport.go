// Package httputil provides a shared HTTP transport with tuned connection pooling.
//
// Using a shared transport across all HTTP clients (OG image extractor, content
// extractor, database client) enables connection reuse when multiple workers hit
// the same hosts, avoiding constant connection teardown and recreation.
//
// It also exposes a per-host rate-limited variant (NewRateLimitedClient) that
// wraps the shared transport with a token bucket keyed by req.URL.Host so we
// stay polite when enriching many articles from the same domain.
package httputil

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
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

// NewClientWithRedirectLimit creates an http.Client using the shared transport
// with the given timeout and a maximum number of redirects before stopping.
func NewClientWithRedirectLimit(timeout time.Duration, maxRedirects int) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: SharedTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// RateLimitingTransport wraps an underlying RoundTripper with a per-host token
// bucket rate limiter. Requests to the same URL.Host share one limiter;
// different hosts proceed independently.
//
// The limiter map grows unbounded across the process lifetime. For this worker
// that's fine (single fetch run of ~10 min against ~133 hosts). A long-running
// process would need eviction.
type RateLimitingTransport struct {
	base  http.RoundTripper
	rps   rate.Limit
	burst int

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

// NewRateLimitingTransport wraps base with a per-host rate limiter at the given
// rate (requests/sec) and burst. base may be nil to use SharedTransport.
func NewRateLimitingTransport(base http.RoundTripper, rps float64, burst int) *RateLimitingTransport {
	if base == nil {
		base = SharedTransport
	}
	return &RateLimitingTransport{
		base:     base,
		rps:      rate.Limit(rps),
		burst:    burst,
		limiters: make(map[string]*rate.Limiter),
	}
}

// limiterFor returns the limiter for host, creating it on first use.
func (t *RateLimitingTransport) limiterFor(host string) *rate.Limiter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if l, ok := t.limiters[host]; ok {
		return l
	}
	l := rate.NewLimiter(t.rps, t.burst)
	t.limiters[host] = l
	return l
}

// RoundTrip waits for a token against req.URL.Host before delegating to base.
// Wait honors req.Context() so cancellation short-circuits the wait.
func (t *RateLimitingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiterFor(req.URL.Host).Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

// NewRateLimitedClient returns an http.Client that rate-limits per host using
// SharedTransport underneath. rps and burst apply independently to each host.
// maxRedirects mirrors NewClientWithRedirectLimit's behavior (pass 0 to keep
// Go's default of 10).
func NewRateLimitedClient(timeout time.Duration, rps float64, burst int, maxRedirects int) *http.Client {
	client := &http.Client{
		Timeout:   timeout,
		Transport: NewRateLimitingTransport(SharedTransport, rps, burst),
	}
	if maxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		}
	}
	return client
}
