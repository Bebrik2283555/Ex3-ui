package util

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDownloadToWritesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := DownloadTo(srv.URL, dst, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want %q", data, "hello")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("mode = %v, want executable", info.Mode())
		}
	}
}

func TestDownloadToRejectsNonHTTPScheme(t *testing.T) {
	if err := DownloadTo("ftp://example.com/file", "x", 0o644); err == nil {
		t.Fatal("expected a non-http scheme to be rejected")
	}
}

func TestDownloadToPropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := DownloadTo(srv.URL, dst, 0o644); err == nil {
		t.Fatal("expected a 403 to be returned as an error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("failed download must not leave a file behind")
	}
}
