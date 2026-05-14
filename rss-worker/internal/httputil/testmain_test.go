package httputil

import (
	"os"
	"testing"
)

// TestMain enables the loopback exemption on the SSRF guard so tests that
// fetch from httptest.Server (which binds 127.0.0.1) succeed.
func TestMain(m *testing.M) {
	SetAllowLoopback(true)
	defer SetAllowLoopback(false)
	os.Exit(m.Run())
}
