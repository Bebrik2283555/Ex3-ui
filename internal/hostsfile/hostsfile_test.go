package hostsfile

import (
	"os"
	"path/filepath"
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
