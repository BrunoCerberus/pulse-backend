// Package httputil provides a shared HTTP transport with tuned connection pooling.
//
// Using a shared transport across all HTTP clients (OG image extractor, content
// extractor, database client) enables connection reuse when multiple workers hit
// the same hosts, avoiding constant connection teardown and recreation.
//
// It also exposes:
//   - a per-host rate-limited variant (NewRateLimitedClient) that wraps the
//     shared transport with a token bucket keyed by req.URL.Host so we stay
//     polite when enriching many articles from the same domain.
//   - an SSRF-aware safe transport (SafeTransport) used by the rate-limited
//     clients. SafeTransport dials via a custom DialContext that resolves the
//     hostname once, rejects forbidden IP ranges (loopback / private /
//     link-local / multicast), then connects to the resolved IP directly so a
//     hostile DNS server can't rebind between check and connect.
package httputil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// allowLoopback (atomic) gates whether SSRF checks treat loopback IPs as
// safe. Production code MUST leave this false; the SetAllowLoopback helper
// is intended for tests that use httptest.Server (which binds 127.0.0.1).
var allowLoopback atomic.Bool

// SetAllowLoopback toggles loopback exemption in IsForbiddenIP. Returns the
// previous value so tests can restore via defer. Test-only.
func SetAllowLoopback(v bool) bool {
	return allowLoopback.Swap(v)
}

// SharedTransport is a shared http.Transport with tuned connection pool
// settings. Used by the Supabase client (single trusted host, no SSRF guard
// needed). For user-content fetches (RSS feeds, og:image, content) use
// SafeTransport instead.
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

// ErrBlockedByPolicy is returned when an SSRF check refuses to connect.
var ErrBlockedByPolicy = errors.New("blocked by SSRF policy")

// lookupIP is the IP lookup function used by SafeTransport. Tests can override
// it to simulate hostile DNS responses.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// IsForbiddenIP reports whether ip falls in a range we refuse to connect to.
// Covers cloud-metadata (link-local 169.254.169.254), internal networks
// (RFC 1918 private + IPv6 ULA), loopback, multicast, and the unspecified
// 0.0.0.0/::. Public-routable IPs return false.
//
// Loopback (127.0.0.0/8, ::1) is exempt when SetAllowLoopback(true) was
// called — used by tests that fetch from httptest.Server.
func IsForbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() {
		return !allowLoopback.Load()
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// The stdlib classifiers above miss several internal / IPv4↔IPv6-translation
	// ranges that an SSRF guard must still refuse (see forbiddenCIDRs). Fail
	// closed on a malformed (non-4/16-byte) address slice.
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	for _, p := range forbiddenCIDRs {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// forbiddenCIDRs enumerates ranges that net.IP's stdlib classifiers
// (IsPrivate / IsLinkLocal* / IsMulticast / IsUnspecified) do NOT cover but
// that an SSRF guard must still refuse: the "this host"/"this network" block,
// carrier-grade NAT, the IPv4↔IPv6 translation prefixes (NAT64 / 6to4 /
// Teredo, which can wrap a forbidden v4 such as the 169.254.169.254
// cloud-metadata address), 6to4 relay anycast, reserved Class-E /
// limited-broadcast space, and benchmarking / documentation space. Parsed once
// at package init via MustParsePrefix on static literals.
//
// NOTE on 0.0.0.0/8: net.IP.IsUnspecified() matches ONLY the single address
// 0.0.0.0 (and ::), not the rest of the /8. On Linux the whole 0.0.0.0/8
// "this network" block routes to the local host, so 0.0.0.1 reaches loopback —
// it must be blocked explicitly here.
var forbiddenCIDRs = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // RFC 1122 "this host on this network" — 0.0.0.1 routes to localhost on Linux
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598 carrier-grade NAT / cloud-internal shared space
	netip.MustParsePrefix("192.0.0.0/24"),    // RFC 6890 IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC 5737 documentation (TEST-NET-1)
	netip.MustParsePrefix("192.88.99.0/24"),  // RFC 7526 6to4 relay anycast (deprecated)
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC 2544 benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // RFC 5737 documentation (TEST-NET-2)
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC 5737 documentation (TEST-NET-3)
	netip.MustParsePrefix("240.0.0.0/4"),     // RFC 1112 reserved Class-E + 255.255.255.255 limited broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // RFC 6052 NAT64 well-known prefix (wraps IPv4)
	netip.MustParsePrefix("64:ff9b:1::/48"),  // RFC 8215 NAT64 local-use prefix
	netip.MustParsePrefix("2002::/16"),       // RFC 3056 6to4 (wraps IPv4)
	netip.MustParsePrefix("2001::/32"),       // RFC 4380 Teredo (wraps IPv4)
	netip.MustParsePrefix("2001:db8::/32"),   // RFC 3849 documentation
}

// ValidateSSRFTarget checks a URL string for safety before issuing a request.
// Returns nil if the URL scheme is http(s) and the host resolves only to
// allowed (public) IPs. Use this for explicit pre-flight checks in callers
// that want a clear error message; SafeTransport also enforces this at the
// dial layer, so passing through it is sufficient defence even without
// pre-flight.
func ValidateSSRFTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not allowed", ErrBlockedByPolicy, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedByPolicy)
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsForbiddenIP(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedByPolicy, ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := lookupIP(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	for _, ip := range ips {
		if IsForbiddenIP(ip) {
			return fmt.Errorf("%w: %s resolves to %s", ErrBlockedByPolicy, host, ip)
		}
	}
	return nil
}

// SecureDialContext resolves the host once, rejects forbidden IPs, then dials
// the resolved IP directly. This prevents DNS rebinding (where a hostile name
// server returns a public IP on first lookup and a private IP on the dial-time
// lookup).
func SecureDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = lookupIP(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve host %s: %w", host, err)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: no IPs for host %s", ErrBlockedByPolicy, host)
	}
	for _, ip := range ips {
		if IsForbiddenIP(ip) {
			return nil, fmt.Errorf("%w: host %s resolves to %s", ErrBlockedByPolicy, host, ip)
		}
	}
	// Dial the first allowed IP literal so the dialer doesn't repeat the
	// lookup (which would be a rebinding window).
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// SafeTransport is a transport whose DialContext refuses forbidden IPs. Used
// as the base of all user-content rate-limited clients.
var SafeTransport = &http.Transport{
	DialContext:         SecureDialContext,
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
// rate (requests/sec) and burst. base may be nil to use SafeTransport.
func NewRateLimitingTransport(base http.RoundTripper, rps float64, burst int) *RateLimitingTransport {
	if base == nil {
		base = SafeTransport
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

// NewRateLimitedClient returns an http.Client that rate-limits per host and
// dials via SafeTransport (SSRF-aware DialContext). Used for all user-content
// fetches (RSS feeds, og:image, full content). rps and burst apply
// independently to each host. maxRedirects mirrors NewClientWithRedirectLimit's
// behavior (pass 0 to keep Go's default of 10).
func NewRateLimitedClient(timeout time.Duration, rps float64, burst int, maxRedirects int) *http.Client {
	client := &http.Client{
		Timeout:   timeout,
		Transport: NewRateLimitingTransport(SafeTransport, rps, burst),
	}
	if maxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			// Defence-in-depth: explicitly validate every redirect hop.
			// SafeTransport would already reject the dial, but this gives a
			// clearer error and short-circuits before the network call.
			if err := ValidateSSRFTarget(req.URL.String()); err != nil {
				return err
			}
			return nil
		}
	}
	return client
}
