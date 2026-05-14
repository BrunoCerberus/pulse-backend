package parser

import (
	"os"
	"testing"

	"github.com/pulsefeed/rss-worker/internal/httputil"
)

// TestMain enables the loopback exemption on the SSRF guard so tests that
// fetch from httptest.Server (which binds 127.0.0.1) succeed. Production
// code never calls SetAllowLoopback, so the default (loopback blocked) holds
// outside the test binary.
func TestMain(m *testing.M) {
	httputil.SetAllowLoopback(true)
	defer httputil.SetAllowLoopback(false)
	os.Exit(m.Run())
}
