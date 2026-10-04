//go:build linux

package mtproto

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// killStrayMtgProcesses terminates orphaned mtg sidecars left over from a
// previous x-ui run and returns how many were killed.
//
// x-ui starts one mtg process per mtproto inbound outside its own lifecycle, and
// on Linux a child is not guaranteed to die with the panel (there is no
// kill-on-exit, unlike the Windows job object). A survivor keeps holding the
// inbound port with a now-stale secret, so new clients are silently
// domain-fronted to the FakeTLS domain instead of proxied to Telegram. x-ui is
// the sole owner of mtg, so any process matching our binary name at startup is
// an orphan and is safe to kill before we start our own.
//
// binaryPath is the configured mtg path (e.g. "bin/mtg-linux-amd64"). A living
// process must match the full executable path, so a foreign mtg binary of the
// same base name (another user's install) is never killed; only processes whose
// executable has been deleted (the post-update case, where /proc/<pid>/exe
// reads as "<path> (deleted)" and argv[0] is the fallback) match on the base
// name.
func killStrayMtgProcesses(binaryPath string) int {
	base := filepath.Base(binaryPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return 0
	}
	full := filepath.Clean(binaryPath)
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		if !isStrayMtg(pid, full, base) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}
	return killed
}

// isStrayMtg reports whether pid is an orphaned mtg of this panel: the live
// executable resolves to our configured path, or (binary deleted by an update)
// the argv[0] base name matches. A foreign process whose executable still
// exists at another path is left alone.
func isStrayMtg(pid int, fullPath, baseName string) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err == nil {
		if filepath.Clean(exe) == fullPath {
			return true
		}
		return false
	}
	return cmdlineArgv0Base(pid) == baseName
}

// cmdlineArgv0Base returns the base name of argv[0] from /proc/<pid>/cmdline,
// the reliable fallback when the binary has been replaced or exe is unreadable.
func cmdlineArgv0Base(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return ""
	}
	argv0 := data
	if i := strings.IndexByte(string(data), 0); i >= 0 {
		argv0 = data[:i]
	}
	if len(argv0) == 0 {
		return ""
	}
	return filepath.Base(string(argv0))
}
