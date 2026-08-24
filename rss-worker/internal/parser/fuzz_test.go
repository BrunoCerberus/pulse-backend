package parser

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The parser package is the repo's primary hostile-input surface: every string
// fuzzed here arrives verbatim from an attacker-controlled RSS feed or article
// page. Statement coverage proves each line ran once; these targets probe what
// unusual bytes do to it. Seeds double as regression tests — `go test` replays
// the corpus (including testdata/fuzz/*) on every run without -fuzz.

// seedHostile adds inputs that have historically broken byte-offset arithmetic:
// bytes whose lowercase form is a different length (invalid UTF-8 -> U+FFFD,
// U+023A/U+023E -> 3 bytes), truncated tags, and unbalanced markup.
func seedHostile(f *testing.F) {
	seeds := []string{
		"",
		"plain text",
		"<script>alert(1)</script>hello",
		"<STYLE>body{}</STYLE>ok",
		"\x90\x90\x90\x90<STYLE",
		"\xff\xfe<script>x</script>",
		"Ⱥ<script>x</script>tail",
		"<script",
		"</script>",
		"<scriptx>keep</scriptx>",
		"&lt;script&gt;alert&lt;/script&gt;",
		"&#x3c;script&#x3e;",
		"a\u202Eb\u200Fc", // RLO + RLM: bidi spoofing, stripped by C-SANITIZE
		"<p>one</p><br/>two",
		"K<script>a</script>b",
		// Long enough that the lowercased copy outgrows the original by more
		// than the tag length — the shape that produced the slice-bounds panic.
		strings.Repeat("Ⱦ", 20) + "<style>y</style>",
	}
	for _, s := range seeds {
		f.Add(s)
	}
}

func FuzzCleanHTML(f *testing.F) {
	seedHostile(f)
	f.Fuzz(func(t *testing.T, s string) {
		out := cleanHTML(s)
		// C-SANITIZE: <script>/<style> bodies must never reach article text,
		// and no tag may survive the strip pass.
		for _, tag := range []string{"<script", "<style", "<"} {
			if idx := asciiIndexFold(out, tag); idx != -1 {
				t.Fatalf("cleanHTML(%q) = %q: leaked %q at %d", s, out, tag, idx)
			}
		}
	})
}

func FuzzSanitizeText(f *testing.F) {
	seedHostile(f)
	f.Fuzz(func(t *testing.T, s string) {
		for _, max := range []int{0, 1, 7, 4096} {
			out := sanitizeText(s, max)
			if max > 0 && utf8.RuneCountInString(out) > max {
				t.Fatalf("sanitizeText(%q, %d) = %q: %d runes exceeds cap",
					s, max, out, utf8.RuneCountInString(out))
			}
			// C-SANITIZE: control and bidi-override codepoints are stripped,
			// not merely truncated away.
			for _, r := range out {
				if isControlOrBidi(r) {
					t.Fatalf("sanitizeText(%q, %d) = %q: kept U+%04X", s, max, out, r)
				}
			}
		}
	})
}

func FuzzCanonicalizeURL(f *testing.F) {
	for _, s := range []string{
		"https://example.com/a?b=1&a=2#frag",
		"HTTPS://Example.COM/A",
		"https://example.com/x?id=1;ref=y",
		"://",
		"%",
		"http://[::1]/",
		"\x00https://example.com",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		once := canonicalizeURL(s)
		// url_hash dedup depends on this being a fixed point: two feeds
		// publishing the same article must collapse to one row.
		if twice := canonicalizeURL(once); twice != once {
			t.Fatalf("canonicalizeURL not idempotent for %q: %q -> %q", s, once, twice)
		}
	})
}

func FuzzURLSafety(f *testing.F) {
	for _, s := range []string{
		"https://example.com/ok",
		"javascript:alert(1)",
		"data:text/html,x",
		"http://127.0.0.1/",
		"http://0x7f.1/",
		"http://2130706433/",
		"http://017700000001/",
		"http://[::ffff:127.0.0.1]/",
		"http://example.com\x00/",
		strings.Repeat("a", 4000),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// C-SSRF / C-URLSAFE: anything the article check accepts must also
		// clear the media check once the length cap is satisfied, and an
		// accepted host must never decode to a forbidden IP literal.
		safe := isSafeArticleURL(s)
		if safe && len(s) <= maxURLLen && !isSafeMediaURL(s) {
			t.Fatalf("isSafeArticleURL(%q) accepted but isSafeMediaURL rejected", s)
		}
		if safe && urlHasUnsafeRune(s) {
			t.Fatalf("isSafeArticleURL(%q) accepted a URL with an unsafe rune", s)
		}
	})
}

func FuzzParseDuration(f *testing.F) {
	for _, s := range []string{
		"", "90", "1:30", "01:02:03", "99999999999999999999",
		"-1", "::", "1:2:3:4", " 42 ", "0x10", "1:-1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d := parseDuration(s)
		if d < 0 || d > maxMediaDurationSeconds {
			t.Fatalf("parseDuration(%q) = %d, outside [0,%d]", s, d, maxMediaDurationSeconds)
		}
	})
}

func FuzzTruncateToFirstParagraph(f *testing.F) {
	seedHostile(f)
	f.Fuzz(func(t *testing.T, s string) {
		out := truncateToFirstParagraph(s)
		if !strings.HasPrefix(s, out) {
			t.Fatalf("truncateToFirstParagraph(%q) = %q: not a prefix", s, out)
		}
		// Byte-slicing must not split a multi-byte rune.
		if utf8.ValidString(s) && !utf8.ValidString(out) {
			t.Fatalf("truncateToFirstParagraph(%q) = %q: broke UTF-8", s, out)
		}
	})
}
