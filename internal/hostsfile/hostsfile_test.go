package hostsfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateHostsContentAcceptsRealFiles(t *testing.T) {
	content := `# managed
127.0.0.1 localhost
::1 ip6-localhost ip6-loopback
104.21.65.111 chatgpt.com # pinned
`
	if err := validateHostsContent(content); err != nil {
		t.Fatalf("validateHostsContent: %v", err)
	}
}

func TestValidateHostsContentRejectsGarbage(t *testing.T) {
	for name, content := range map[string]string{
		"missing hostname": "127.0.0.1\n",
		"not an IP":        "definitely.not.an.ip example.test\n",
		"NUL byte":         "127.0.0.1 localhost\x00\n",
	} {
		if err := validateHostsContent(content); err == nil {
			t.Fatalf("%s: validateHostsContent accepted %q", name, content)
		}
	}
}

// A failed or interrupted write must not leave /etc/hosts truncated, so Set
// swaps a complete temp file in instead of writing in place.
func TestWriteAtomicReplacesTheWholeFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hosts")
	if err := os.WriteFile(target, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := []byte("127.0.0.1 localhost\n")
	if err := writeAtomic(target, content, 0o644); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("hosts = %q, want %q", got, content)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

// Containers keep /etc read-only and bind-mount /etc/hosts as a writable file,
// so the temp file cannot be created; the write must go through the file.
func TestWriteAtomicFallsBackToInPlaceOnReadOnlyDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("directory permissions do not block file creation on this platform")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "hosts")
	if err := os.WriteFile(target, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	content := []byte("127.0.0.1 localhost\n")
	if err := writeAtomic(target, content, 0o644); err != nil {
		t.Fatalf("writeAtomic on read-only dir: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("hosts = %q, want %q", got, content)
	}
}
