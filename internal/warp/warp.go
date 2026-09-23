// Package warp manages a Cloudflare WARP tunnel via usque (MASQUE/QUIC).
// It installs usque + systemd unit + cron watchdog by running an embedded
// shell script, and exposes start/stop/restart/rotate operations.
package warp

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/warp-usque.sh
var installScript []byte

const (
	usqueBin    = "/usr/local/bin/usque"
	serviceName = "usque.service"
	watchLog    = "/var/log/warp-watch.log"
	installLog  = "/var/log/warp-install.log"
	rotateBin   = "/usr/local/bin/warp-rotate.sh"
	watchBin    = "/usr/local/bin/warp-watch.sh"
)

var errUnsupported = errors.New("warp is only supported on Linux")

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return errUnsupported
	}
	return nil
}

// installing and rotating guard concurrent runs of the long-running tasks.
var (
	mu        sync.Mutex
	installing bool
	rotating   bool
)

// IsInstalled reports whether usque binary exists on disk.
func IsInstalled() bool {
	_, err := os.Stat(usqueBin)
	return err == nil
}

// IsInstalling reports whether an install is in progress.
func IsInstalling() bool {
	mu.Lock()
	defer mu.Unlock()
	return installing
}

// IsRotating reports whether a rotate-IP operation is in progress.
func IsRotating() bool {
	mu.Lock()
	defer mu.Unlock()
	return rotating
}

// Status describes the live state of the WARP tunnel.
type Status struct {
	Installed      bool   `json:"installed"`
	Running        bool   `json:"running"`
	WatchdogActive bool   `json:"watchdogActive"`
	Installing     bool   `json:"installing"`
	Rotating       bool   `json:"rotating"`
	Error          string `json:"error,omitempty"`
}

// GetStatus returns the current warp state.
func GetStatus() Status {
	st := Status{
		Installing: IsInstalling(),
		Rotating:   IsRotating(),
	}
	if err := requireLinux(); err != nil {
		st.Error = err.Error()
		return st
	}
	st.Installed = IsInstalled()
	if st.Installed {
		if out, err := runOutput("systemctl", "is-active", serviceName); err == nil && strings.TrimSpace(out) == "active" {
			st.Running = true
		}
	}
	// Check cron watchdog
	if out, err := runOutput("crontab", "-l"); err == nil && strings.Contains(out, "warp-watch.sh") {
		st.WatchdogActive = true
	}
	return st
}

// Install runs the embedded warp-usque.sh script in the background.
// Returns ErrAlreadyRunning (409-worthy) if an install is already in progress.
var ErrAlreadyRunning = errors.New("install already in progress")

func Install() error {
	if err := requireLinux(); err != nil {
		return err
	}
	mu.Lock()
	if installing {
		mu.Unlock()
		return ErrAlreadyRunning
	}
	installing = true
	mu.Unlock()

	go func() {
		defer func() {
			mu.Lock()
			installing = false
			mu.Unlock()
		}()
		runInstallScript(installScript, installLog)
	}()
	return nil
}

// runInstallScript pipes script into bash -s and captures output to logFile.
func runInstallScript(script []byte, logFile string) {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== warp install started at %s ===\n", time.Now().Format(time.RFC3339))
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(f, "\n=== exit error: %v ===\n", err)
	} else {
		fmt.Fprintf(f, "\n=== done at %s ===\n", time.Now().Format(time.RFC3339))
	}
}

// Uninstall stops the service, removes the unit, binaries, config and cron.
func Uninstall() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("systemctl", "stop", serviceName)
	_ = run("systemctl", "disable", serviceName)
	_ = os.Remove("/etc/systemd/system/" + serviceName)
	_ = run("systemctl", "daemon-reload")
	_ = os.RemoveAll("/etc/usque")
	_ = os.Remove(usqueBin)
	_ = os.Remove(rotateBin)
	_ = os.Remove(watchBin)
	_ = os.Remove("/run/warp-watch.state")
	removeCronEntry("warp-watch.sh")
	return nil
}

// removeCronEntry removes lines containing needle from the current user's crontab.
func removeCronEntry(needle string) {
	out, err := runOutput("crontab", "-l")
	if err != nil {
		return
	}
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, needle) {
			kept = append(kept, line)
		}
	}
	filtered := strings.Join(kept, "\n")
	tmp, err := os.CreateTemp("", "crontab")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(filtered); err != nil {
		tmp.Close()
		return
	}
	tmp.Close()
	_ = run("crontab", tmp.Name())
}

// Start starts the usque systemd service.
func Start() error {
	if err := requireLinux(); err != nil {
		return err
	}
	return run("systemctl", "start", serviceName)
}

// Stop stops the usque systemd service.
func Stop() error {
	if err := requireLinux(); err != nil {
		return err
	}
	return run("systemctl", "stop", serviceName)
}

// Restart restarts the usque systemd service.
func Restart() error {
	if err := requireLinux(); err != nil {
		return err
	}
	return run("systemctl", "restart", serviceName)
}

var ErrRotateRunning = errors.New("rotate already in progress")

// RotateIP runs warp-rotate.sh in the background to pick a fresh WARP IP.
func RotateIP() error {
	if err := requireLinux(); err != nil {
		return err
	}
	if _, err := os.Stat(rotateBin); err != nil {
		return errors.New("warp-rotate.sh not found; install warp first")
	}
	mu.Lock()
	if rotating {
		mu.Unlock()
		return ErrRotateRunning
	}
	rotating = true
	mu.Unlock()

	go func() {
		defer func() {
			mu.Lock()
			rotating = false
			mu.Unlock()
		}()
		cmd := exec.Command("bash", rotateBin)
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run()
	}()
	return nil
}

// Logs returns the last n lines of the warp watchdog log.
func Logs(n int) []string {
	if err := requireLinux(); err != nil {
		return nil
	}
	if n <= 0 {
		n = 200
	}
	return tailFile(watchLog, n)
}

// InstallLogs returns the last n lines of the warp install log.
func InstallLogs(n int) []string {
	if err := requireLinux(); err != nil {
		return nil
	}
	if n <= 0 {
		n = 100
	}
	return tailFile(installLog, n)
}

// tailFile reads the last n lines of a file.
func tailFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// DefaultBinaryPath returns the expected path to a warp-related binary (unused;
// kept for consistency with the extra package layout).
func DefaultBinaryPath() string {
	return filepath.Join("/usr/local/bin", "usque")
}
