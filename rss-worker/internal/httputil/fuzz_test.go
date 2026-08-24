package httputil

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
)

// C-SSRF lives or dies on these two functions correctly classifying whatever a
// hostile feed puts in a URL. TestMain enables the loopback exemption for the
// package's httptest-based tests, so each target below pins the flag to the
// production setting itself rather than inheriting it.

func FuzzIsForbiddenIP(f *testing.F) {
	for _, b := range [][]byte{
		nil,
		{},
		{127, 0, 0, 1},
		{169, 254, 169, 254},
		{10, 0, 0, 1},
		{8, 8, 8, 8},
		{0, 0, 0, 0},
		net.ParseIP("::1"),
		net.ParseIP("fd00::1"),
		net.ParseIP("::ffff:169.254.169.254"),
		net.ParseIP("2606:4700::1111"),
		make([]byte, 5),
		make([]byte, 17),
	} {
		f.Add([]byte(b))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		prev := SetAllowLoopback(false)
		defer SetAllowLoopback(prev)

		ip := net.IP(b)
		forbidden := IsForbiddenIP(ip)

		// Fail closed: anything that isn't a well-formed 4- or 16-byte address
		// must be refused rather than treated as routable.
		if len(b) != net.IPv4len && len(b) != net.IPv6len && !forbidden {
			t.Fatalf("IsForbiddenIP(%v) = false for a malformed %d-byte address", b, len(b))
		}
		// An address that IPv4-maps to a forbidden v4 range must stay forbidden
		// in its v6 form — ::ffff:169.254.169.254 is the classic bypass.
		if v4 := ip.To4(); v4 != nil && IsForbiddenIP(v4) != forbidden {
			t.Fatalf("IsForbiddenIP disagrees between %v and its 4-byte form %v", ip, v4)
		}
	})
}

func FuzzValidateSSRFTarget(f *testing.F) {
	for _, s := range []string{
		"https://example.com/",
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:8080/",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"https://",
		"http://[::1]/",
		// Embedded userinfo (the `trusted.com@evil/` spoof). The guard tests
		// `u.User != nil`, so userinfo without a password exercises the same
		// branch while keeping these literals clear of two repo-wide scanners:
		// an IP-literal host is not email-shaped (the conformance PII scan
		// bans email literals) and omitting `:password` keeps TruffleHog from
		// reading them as credential-bearing URIs.
		"https://user@127.0.0.1/",
		"https://user@[::1]/",
		"http://example.com\x00.evil.test/",
		"%zz",
	} {
		f.Add(s)
	}

	// Pin DNS: a fuzz target must not make network calls, and a fixed hostile
	// answer keeps the resolved-IP branch reachable and deterministic.
	prev := lookupIP
	f.Cleanup(func() { lookupIP = prev })
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if host == "unresolvable.invalid" {
			return nil, errors.New("no such host")
		}
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}

	f.Fuzz(func(t *testing.T, s string) {
		prevLoop := SetAllowLoopback(false)
		defer SetAllowLoopback(prevLoop)

		// Every name resolves to link-local above, so the only URLs that may
		// pass are those bearing a literal public IP. Anything else escaping
		// the guard is an SSRF hole.
		if err := ValidateSSRFTarget(s); err == nil {
			u, perr := parseHostname(s)
			if perr != nil {
				t.Fatalf("ValidateSSRFTarget(%q) passed but the URL does not parse: %v", s, perr)
			}
			ip := net.ParseIP(u)
			if ip == nil {
				t.Fatalf("ValidateSSRFTarget(%q) passed a hostname that resolves to link-local", s)
			}
			if IsForbiddenIP(ip) {
				t.Fatalf("ValidateSSRFTarget(%q) passed forbidden IP %v", s, ip)
			}
		}
	})
}

// parseHostname mirrors ValidateSSRFTarget's own host extraction so the target
// above asserts against the same value the guard actually inspected.
func parseHostname(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}
