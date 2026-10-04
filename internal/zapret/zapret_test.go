package zapret

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
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

// buildLayout writes a zapret-linux-easy style layout (top folder wrapping
// bins/ and files/) dir and returns the install root.
func buildLayout(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, "zapret-linux-easy-main")
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("install.sh", "#!/bin/sh")
	write("bins/x86_64/nfqws", "nfqws-binary")
	write("bins/arm64/nfqws", "nfqws-binary")
	write("files/autohosts.txt", "example.com\n")
	write("files/config.txt", "--wssize=1")
	write("files/system/starter.sh", "#!/bin/sh\necho start")
	write("files/system/stopper.sh", "#!/bin/sh\necho stop")
	write("files/system/zapret.service", "[Unit]\nDescription=zapret")
	return root
}

// TestInstallFilesStripsArchiveWrapper asserts that the files/ tree of the
// zapret-linux-easy archive is installed under /opt/zapret without the files/
// prefix: lists at the root and system helpers in system/, as the unit and
// starter.sh reference them. This is the layout broken by the original
// files/-preserving copy (nfqws could not be written because system/ did not
// exist).
func TestInstallFilesStripsArchiveWrapper(t *testing.T) {
	dir := t.TempDir()
	root := buildLayout(t, dir)
	dest := filepath.Join(dir, "opt", "zapret")
	if err := installFiles(os.DirFS(root), ".", dest); err != nil {
		t.Fatal(err)
	}
	assert := func(rel, want string) {
		got, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	assert("autohosts.txt", "example.com\n")
	assert("config.txt", "--wssize=1")
	assert("system/starter.sh", "#!/bin/sh\necho start")
	assert("system/zapret.service", "[Unit]\nDescription=zapret")
	if st, err := os.Stat(filepath.Join(dest, "system", "starter.sh")); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && st.Mode()&0o100 == 0 {
		t.Errorf("starter.sh must be executable, mode = %v", st.Mode())
	}
	// The binary lives in bins/ and is copied separately as system/nfqws.
	if _, err := os.Stat(filepath.Join(dest, "bins")); !os.IsNotExist(err) {
		t.Errorf("bins must not be copied, err = %v", err)
	}
}

func TestCopyNFQWSFilesToSystemDir(t *testing.T) {
	dir := t.TempDir()
	root := buildLayout(t, dir)
	dest := filepath.Join(dir, "opt", "zapret")
	if err := copyNFQWS(os.DirFS(root), ".", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "system", "nfqws"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nfqws-binary" {
		t.Errorf("nfqws = %q, want %q", got, "nfqws-binary")
	}
}

func TestRestoreZipFilesWritesOnlyEditableLists(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "backup.zip")
	makeZip(t, zipPath, map[string]string{
		"autohosts.txt":            "example.com\n",
		"config.txt":               "--wssize=1",
		"README.md":                "ignored",
		"../escape.txt":            "pwn",
		"zapret-master/ignore.txt": "blocked.org\n",
	})
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreZipFiles(data, dir); err != nil {
		t.Fatal(err)
	}
	assert := func(name, want string) {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	assert("autohosts.txt", "example.com\n")
	assert("config.txt", "--wssize=1")
	assert("ignore.txt", "blocked.org\n")
	// Non-editable entries and traversal attempts must not be written.
	empty := func(name string) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s must not exist", name)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	empty("README.md")
	empty("escape.txt")
}

func TestRestoreZipRejectsInvalidArchive(t *testing.T) {
	if err := restoreZipFiles([]byte("not a zip"), t.TempDir()); err == nil {
		t.Fatal("expected an error for a malformed archive")
	}
}
