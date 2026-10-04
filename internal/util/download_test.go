package util

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadToRejectsNonHTTPScheme(t *testing.T) {
	if err := DownloadTo("ftp://example.com/file", "x", 0o644); err == nil {
		t.Fatal("expected a non-http scheme to be rejected")
	}
}

func TestDownloadToRejectsLoopback(t *testing.T) {
	// The SSRF guard must refuse loopback/private destinations before any
	// dial attempt — otherwise a single download API is a network probe.
	err := DownloadTo("https://127.0.0.1:1/x", filepath.Join(t.TempDir(), "out.bin"), 0o644)
	if err == nil {
		t.Fatal("expected the loopback destination to be rejected")
	}
	if !strings.Contains(err.Error(), "blocked private") {
		t.Fatalf("err = %v, want an SSRF-guard rejection", err)
	}
}
