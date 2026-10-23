package service

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A full backup is all-or-error: every readable certificate file must land in
// the archive and be listed in the manifest.
func TestAppendCertDirAtIncludesCerts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "example.com"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "example.com", "fullchain.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "account.key"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var manifest fullBackupManifest
	if err := appendCertDirAt(zw, &manifest, root); err != nil {
		t.Fatalf("appendCertDirAt: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	for _, want := range []string{"cert/example.com/fullchain.pem", "cert/account.key"} {
		if !slices.Contains(names, want) {
			t.Fatalf("archive names = %v, want %s", names, want)
		}
	}
	if !slices.Contains(manifest.Certs, "example.com/fullchain.pem") ||
		!slices.Contains(manifest.Certs, "account.key") {
		t.Fatalf("manifest certs = %v", manifest.Certs)
	}
}

// Skipping an oversized certificate would produce a valid but incomplete
// archive, so the backup must fail instead.
func TestAppendCertDirAtRejectsOversizedCert(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "huge.pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxCertFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var manifest fullBackupManifest
	err = appendCertDirAt(zw, &manifest, root)
	_ = zw.Close()
	if err == nil || !strings.Contains(err.Error(), "certificate limit") {
		t.Fatalf("appendCertDirAt error = %v, want the certificate size limit", err)
	}
}
