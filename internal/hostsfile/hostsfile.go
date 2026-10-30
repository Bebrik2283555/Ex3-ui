// Package hostsfile reads and writes the system hosts file (/etc/hosts).
// qwdtt and olcRTC resolve domains through the OS resolver, so domains can be
// pinned to the server IP here — bypassing DNS-based blocking for those cores.
package hostsfile

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util"
)

const path = "/etc/hosts"

// ErrUnsupportedPlatform is returned on non-Linux systems.
var ErrUnsupportedPlatform = errors.New("system hosts editing is only supported on Linux")

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return ErrUnsupportedPlatform
	}
	return nil
}

// Entry is a single hosts line.
type Entry struct {
	IP     string `json:"ip"`
	Domain string `json:"domain"`
}

// HostsFile is the parsed content of /etc/hosts.
type HostsFile struct {
	Entries []Entry `json:"entries"`
	Raw     string  `json:"raw"`
}

// Get reads and parses /etc/hosts.
func Get() (HostsFile, error) {
	var hf HostsFile
	if err := requireLinux(); err != nil {
		return hf, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return hf, err
	}
	hf.Raw = string(data)
	for _, line := range strings.Split(hf.Raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		hf.Entries = append(hf.Entries, Entry{IP: fields[0], Domain: fields[1]})
	}
	return hf, nil
}

// Set replaces the entire /etc/hosts content.
func Set(content string) error {
	if err := requireLinux(); err != nil {
		return err
	}
	if len(content) == 0 {
		return fmt.Errorf("refusing to write an empty hosts file")
	}
	if err := validateHostsContent(content); err != nil {
		return err
	}
	return writeAtomic(path, []byte(content), 0o644)
}

// validateHostsContent rejects text the resolver cannot parse, so a downloaded
// or pasted file cannot leave /etc/hosts in a broken state.
func validateHostsContent(content string) error {
	if strings.ContainsRune(content, 0) {
		return fmt.Errorf("hosts content contains a NUL byte")
	}
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			return fmt.Errorf("hosts line %d: expected an IP address and a hostname", i+1)
		}
		if net.ParseIP(fields[0]) == nil {
			return fmt.Errorf("hosts line %d: %q is not an IP address", i+1, fields[0])
		}
	}
	return nil
}

// writeAtomic replaces the target through a temp file in the same directory:
// a crash or a full disk must never leave /etc/hosts truncated. A symlinked
// target (containers mount their own file) is written through, not replaced.
func writeAtomic(target string, content []byte, perm os.FileMode) error {
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".xui-hosts-")
	if err != nil {
		// Containers and immutable-root images keep /etc read-only and mount
		// /etc/hosts as a writable file, so no temp file can be created there.
		return writeInPlace(target, content, perm)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		// A read-only parent directory rejects the rename but not a write
		// through the mounted file; report the original error if that fails too.
		if fallbackErr := writeInPlace(target, content, perm); fallbackErr != nil {
			return err
		}
	}
	return nil
}

// writeInPlace truncates and rewrites the target. It is the fallback for
// read-only parent directories: not crash-safe, but the only way to update a
// bind-mounted file such as /etc/hosts inside a container.
func writeInPlace(target string, content []byte, perm os.FileMode) error {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ApplyEntries writes a hosts file built from a header comment, the given
// entries, and any existing lines that are not managed (comments / localhost).
func ApplyEntries(entries []Entry) error {
	var b strings.Builder
	b.WriteString("# Managed by x-ui panel — do not edit manually.\n")
	for _, e := range entries {
		if strings.TrimSpace(e.IP) == "" || strings.TrimSpace(e.Domain) == "" {
			continue
		}
		b.WriteString(e.IP + " " + e.Domain + "\n")
	}
	return Set(b.String())
}

// SetFromURL downloads the hosts content from a public link and replaces the
// system hosts file with it. The remote content is capped by util.DownloadTo.
func SetFromURL(url string) error {
	if err := requireLinux(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "xui-hosts-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := util.DownloadTo(url, tmp.Name(), 0o644); err != nil {
		return err
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return err
	}
	return Set(string(data))
}
