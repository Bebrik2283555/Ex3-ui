// Package hostsfile reads and writes the system hosts file (/etc/hosts).
// qwdtt and olcRTC resolve domains through the OS resolver, so domains can be
// pinned to the server IP here — bypassing DNS-based blocking for those cores.
package hostsfile

import (
	"errors"
	"fmt"
	"os"
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
	return os.WriteFile(path, []byte(content), 0o644)
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
