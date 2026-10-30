package warp

import (
	"strings"
	"testing"
)

// warp.env is sourced by the watchdog with ".", so the value must survive a
// shell as a literal: double quotes would still expand $(...) and backticks.
func TestShellQuoteNeutralizesExpansion(t *testing.T) {
	got := shellQuote("https://example.test/$(id)")
	if strings.ContainsAny(got[1:len(got)-1], "\"`") {
		t.Fatalf("shellQuote = %q, want no double quotes or backticks", got)
	}
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Fatalf("shellQuote = %q, want single-quoted value", got)
	}
	if got != `'https://example.test/$(id)'` {
		t.Fatalf("shellQuote = %q", got)
	}
	if escaped := shellQuote("a'b"); escaped != `'a'\''b'` {
		t.Fatalf("shellQuote with a quote = %q", escaped)
	}
}

func TestValidateCheckURL(t *testing.T) {
	for _, raw := range []string{"https://api.telegram.org/", "http://example.test:8080/health"} {
		if err := validateCheckURL(raw); err != nil {
			t.Fatalf("validateCheckURL(%q): %v", raw, err)
		}
	}
	for _, raw := range []string{
		"",
		"api.telegram.org",
		"ftp://example.test/",
		"https://",
		"https://example.test/\nAUTO_ROTATE=0",
		"https://example.test/\x00",
	} {
		if err := validateCheckURL(raw); err == nil {
			t.Fatalf("validateCheckURL(%q) accepted an unsafe value", raw)
		}
	}
}
