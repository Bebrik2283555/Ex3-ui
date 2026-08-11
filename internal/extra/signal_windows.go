//go:build windows

package extra

// signalHUP is a no-op on Windows, which has no SIGHUP.
func signalHUP(pid int) {}
