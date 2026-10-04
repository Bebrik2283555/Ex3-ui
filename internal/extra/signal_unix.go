//go:build !windows

package extra

import "syscall"

// signalHUP delivers SIGHUP to an arbitrary process (Linux/Unix only).
func signalHUP(pid int) {
	_ = syscall.Kill(pid, syscall.SIGHUP)
}
