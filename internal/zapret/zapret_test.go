package zapret

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeZip writes an in-memory zip archive to path with the given name->content map.
func makeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	z, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	w := zip.NewWriter(z)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipExtractsFiles(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	makeZip(t, zipPath, map[string]string{
		"bins/x86_64/nfqws":       "binary",
		"files/system/starter.sh": "#!/bin/sh",
	})

	dst := filepath.Join(dir, "out")
	if err := unzip(zipPath, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "bins", "x86_64", "nfqws"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("content = %q, want %q", got, "binary")
	}
}

func TestUnzipRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	makeZip(t, zipPath, map[string]string{"../escape": "pwn"})

	if err := unzip(zipPath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestFindInstallRootUnwrapsTopLevelDir(t *testing.T) {
	dir := t.TempDir()
	wrapped := filepath.Join(dir, "zapret-master")
	if err := os.MkdirAll(filepath.Join(wrapped, "bins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wrapped, "files"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := findInstallRoot(dir)
	if root == "" {
		t.Fatal("findInstallRoot returned empty for a wrapped layout")
	}
	if filepath.Clean(root) != filepath.Clean(wrapped) {
		t.Fatalf("root = %q, want %q", root, wrapped)
	}
}

func TestFindInstallRootRejectsNonZapretDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "misc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if root := findInstallRoot(dir); root != "" {
		t.Fatalf("expected no root for a non-zapret dir, got %q", root)
	}
}
