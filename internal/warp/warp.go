// Package warp manages a Cloudflare WARP tunnel via usque (MASQUE/QUIC).
// It installs usque + systemd unit + cron watchdog by running an embedded
// shell script, and exposes start/stop/restart/rotate operations.
package warp

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/warp-usque.sh
var installScript []byte

const (
	usqueBin     = "/usr/local/bin/usque"
	serviceName  = "usque.service"
	watchLog     = "/var/log/warp-watch.log"
	installLog   = "/var/log/warp-install.log"
	rotateBin    = "/usr/local/bin/warp-rotate.sh"
	watchBin     = "/usr/local/bin/warp-watch.sh"
	settingsFile = "/etc/usque/warp-settings.json"
	envFile      = "/etc/usque/warp.env"
)

// DefaultTelegramDomains is the default list of Telegram domains for WARP routing.
var DefaultTelegramDomains = []string{
	"t.me",
	"tg.dev",
	"tg.org",
	"tx.me",
	"teleg.xyz",
	"telegram.ai",
	"telegram.asia",
	"telegram.biz",
	"telegram.cloud",
	"telegram.cn",
	"telegram.co",
	"telegram.com",
	"telegram.de",
	"telegram.dev",
	"telegram.dog",
	"telegram.eu",
	"telegram.fr",
	"telegram.host",
	"telegram.in",
	"telegram.info",
	"telegram.io",
	"telegram.jp",
	"telegram.me",
	"telegram.net",
	"telegram.org",
	"telegram.qa",
	"telegram.ru",
	"telegram.services",
	"telegram.solutions",
	"telegram.space",
	"telegram.team",
	"telegram.tech",
	"telegram.uk",
	"telegram.us",
	"telegram.website",
	"telegram.xyz",
	"telegramapp.org",
	"telegra.ph",
	"telesco.pe",
	"nicegram.app",
	"telegramdownload.com",
	"cdn-telegram.org",
	"comments.app",
	"contest.com",
	"fragment.com",
	"graph.org",
	"quiz.directory",
	"tdesktop.com",
	"telega.one",
	"telegram-cdn.org",
	"usercontent.dev",
	"tgram.org",
	"torg.org",
}

// Settings describes WARP tunnel watchdog and routing preferences.
type Settings struct {
	Domains       []string `json:"domains"`
	Timeout       int      `json:"timeout"`
	CheckInterval int      `json:"checkInterval"`
	CheckUrl      string   `json:"checkUrl"`
	AutoRotate    bool     `json:"autoRotate"`
}

func DefaultSettings() Settings {
	return Settings{
		Domains:       DefaultTelegramDomains,
		Timeout:       5,
		CheckInterval: 5,
		CheckUrl:      "https://api.telegram.org/",
		AutoRotate:    true,
	}
}

func GetSettings() Settings {
	st := DefaultSettings()
	data, err := os.ReadFile(settingsFile)
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Timeout <= 0 {
		st.Timeout = 5
	}
	if st.CheckInterval <= 0 {
		st.CheckInterval = 5
	}
	if st.CheckUrl == "" {
		st.CheckUrl = "https://api.telegram.org/"
	}
	if len(st.Domains) == 0 {
		st.Domains = DefaultTelegramDomains
	}
	return st
}

func SaveSettings(s Settings) error {
	if s.Timeout <= 0 {
		s.Timeout = 5
	}
	if s.CheckInterval <= 0 {
		s.CheckInterval = 5
	}
	if s.CheckUrl == "" {
		s.CheckUrl = "https://api.telegram.org/"
	}
	if len(s.Domains) == 0 {
		s.Domains = DefaultTelegramDomains
	}
	if err := requireLinux(); err != nil {
		return nil
	}
	_ = os.MkdirAll("/etc/usque", 0o755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsFile, data, 0o644); err != nil {
		return err
	}
	autoRotateInt := 0
	if s.AutoRotate {
		autoRotateInt = 1
	}
	envContent := fmt.Sprintf("MAX_LATENCY=%d\nCHECK_INTERVAL=%d\nCHECK_URL=%q\nAUTO_ROTATE=%d\n",
		s.Timeout, s.CheckInterval, s.CheckUrl, autoRotateInt)
	_ = os.WriteFile(envFile, []byte(envContent), 0o644)

	timerContent := fmt.Sprintf("[Unit]\nDescription=Run Cloudflare WARP Tunnel Watchdog periodically\n\n[Timer]\nOnBootSec=1min\nOnUnitActiveSec=%dmin\nAccuracySec=10s\n\n[Install]\nWantedBy=timers.target\n", s.CheckInterval)
	if _, err := os.Stat("/etc/systemd/system/warp-watch.timer"); err == nil {
		_ = os.WriteFile("/etc/systemd/system/warp-watch.timer", []byte(timerContent), 0o644)
		_ = run("systemctl", "daemon-reload")
		_ = run("systemctl", "restart", "warp-watch.timer")
	}

	if out, err := runOutput("crontab", "-l"); err == nil && strings.Contains(out, "warp-watch.sh") {
		var lines []string
		for _, l := range strings.Split(out, "\n") {
			trimmed := strings.TrimSpace(l)
			if !strings.Contains(trimmed, "warp-watch.sh") && trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
		lines = append(lines, fmt.Sprintf("*/%d * * * * /usr/local/bin/warp-watch.sh", s.CheckInterval))
		newCron := strings.Join(lines, "\n") + "\n"
		cmd := exec.CommandContext(context.Background(), "crontab", "-")
		cmd.Stdin = strings.NewReader(newCron)
		_ = cmd.Run()
	}
	return nil
}

var errUnsupported = errors.New("warp is only supported on Linux")

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return errUnsupported
	}
	return nil
}

// installing and rotating guard concurrent runs of the long-running tasks.
var (
	mu            sync.Mutex
	installing    bool
	rotating      bool
	installCancel context.CancelFunc
	rotateCancel  context.CancelFunc
)

const defaultCmdTimeout = 20 * time.Second

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

// IsRotating reports whether an IP rotation is in progress.
func IsRotating() bool {
	mu.Lock()
	defer mu.Unlock()
	return rotating
}

// Status captures runtime state of the WARP tunnel.
type Status struct {
	Installed      bool   `json:"installed"`
	Running        bool   `json:"running"`
	WatchdogActive bool   `json:"watchdogActive"`
	IP             string `json:"ip,omitempty"`
	Country        string `json:"country,omitempty"`
	Installing     bool   `json:"installing"`
	Rotating       bool   `json:"rotating"`
	Error          string `json:"error,omitempty"`
}

func ensureWatchdogService() {
	if _, err := os.Stat(watchBin); err != nil {
		return
	}
	st := GetSettings()
	serviceContent := `[Unit]
Description=Cloudflare WARP Tunnel Watchdog
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/warp-watch.sh
`
	timerContent := fmt.Sprintf(`[Unit]
Description=Run Cloudflare WARP Tunnel Watchdog periodically

[Timer]
OnBootSec=1min
OnUnitActiveSec=%dmin
AccuracySec=10s

[Install]
WantedBy=timers.target
`, st.CheckInterval)

	_ = os.WriteFile("/etc/systemd/system/warp-watch.service", []byte(serviceContent), 0o644)
	_ = os.WriteFile("/etc/systemd/system/warp-watch.timer", []byte(timerContent), 0o644)
	_ = run("systemctl", "daemon-reload")
	_ = run("systemctl", "enable", "--now", "warp-watch.timer")

	if out, err := runOutput("crontab", "-l"); err == nil && !strings.Contains(out, "warp-watch.sh") {
		var lines []string
		for _, l := range strings.Split(out, "\n") {
			trimmed := strings.TrimSpace(l)
			if trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
		lines = append(lines, fmt.Sprintf("*/%d * * * * /usr/local/bin/warp-watch.sh", st.CheckInterval))
		newCron := strings.Join(lines, "\n") + "\n"
		cmd := exec.CommandContext(context.Background(), "crontab", "-")
		cmd.Stdin = strings.NewReader(newCron)
		_ = cmd.Run()
	}
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
	if !st.Installed {
		mu.Lock()
		rotating = false
		mu.Unlock()
		st.Running = false
		st.Rotating = false
		st.WatchdogActive = false
		return st
	}
	if out, err := runOutput("systemctl", "is-active", serviceName); err == nil && strings.TrimSpace(out) == "active" {
		st.Running = true
	}

	checkWatchdog := func() bool {
		if _, err := os.Stat(watchBin); err != nil {
			return false
		}
		if out, err := runOutput("systemctl", "is-active", "warp-watch.timer"); err == nil && strings.TrimSpace(out) == "active" {
			return true
		}
		if out, err := runOutput("systemctl", "is-enabled", "warp-watch.timer"); err == nil && strings.TrimSpace(out) == "enabled" {
			return true
		}
		if out, err := runOutput("crontab", "-l"); err == nil && strings.Contains(out, "warp-watch.sh") {
			return true
		}
		if fi, err := os.Stat(watchLog); err == nil && time.Since(fi.ModTime()) < 15*time.Minute {
			return true
		}
		return false
	}

	st.WatchdogActive = checkWatchdog()
	if st.Installed && !st.WatchdogActive && !st.Installing && st.Running {
		ensureWatchdogService()
		st.WatchdogActive = checkWatchdog()
	}

	if st.Installed && st.Running {
		if out, err := runOutput("curl", "--interface", "warp", "-4", "-s",
			"--max-time", "4", "https://1.1.1.1/cdn-cgi/trace"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				switch {
				case strings.HasPrefix(line, "ip="):
					st.IP = strings.TrimSpace(strings.TrimPrefix(line, "ip="))
				case strings.HasPrefix(line, "loc="):
					st.Country = strings.TrimSpace(strings.TrimPrefix(line, "loc="))
				}
			}
		}
	}
	return st
}

// Install runs the embedded warp-usque.sh script in the background.
// Returns ErrAlreadyRunning (409-worthy) if an install is already in progress.
var ErrAlreadyRunning = errors.New("install already in progress")

func Install(s *Settings) error {
	if err := requireLinux(); err != nil {
		return err
	}
	mu.Lock()
	if installing {
		mu.Unlock()
		return ErrAlreadyRunning
	}
	installing = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	installCancel = cancel
	mu.Unlock()

	if s != nil {
		_ = SaveSettings(*s)
	}

	go func() {
		defer func() {
			mu.Lock()
			installing = false
			installCancel = nil
			mu.Unlock()
			cancel()
		}()
		runInstallScript(ctx, installScript, installLog)
	}()
	return nil
}

func runInstallScript(ctx context.Context, script []byte, logFile string) {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== warp install started at %s ===\n", time.Now().Format(time.RFC3339))
	unit := fmt.Sprintf("ex3ui-warp-install-%d", time.Now().UnixNano())
	if path, lookErr := exec.LookPath("systemd-run"); lookErr == nil {
		// apt/systemd need full root: run as a transient unit outside the
		// panel's NoNewPrivileges/CapabilityBoundingSet sandbox.
		cmd := exec.CommandContext(ctx, path, "--quiet", "--wait", "--pipe", "--collect", "--unit", unit, "/bin/bash", "-s")
		cmd.Stdin = bytes.NewReader(script)
		cmd.Stdout = f
		cmd.Stderr = f
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(f, "\n=== exit error: %v ===\n", err)
			if ctx.Err() != nil {
				// Killing systemd-run leaves the transient unit running.
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				_ = exec.CommandContext(cleanupCtx, "systemctl", "stop", unit).Run()
				cancel()
			}
			return
		}
		fmt.Fprintf(f, "\n=== done at %s ===\n", time.Now().Format(time.RFC3339))
		return
	}
	// Fallback for hosts without systemd-run; privileged steps will report
	// their own sandbox errors into the install log.
	cmd := exec.CommandContext(ctx, "bash", "-s")
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
	mu.Lock()
	if installCancel != nil {
		installCancel()
		installCancel = nil
	}
	if rotateCancel != nil {
		rotateCancel()
		rotateCancel = nil
	}
	installing = false
	rotating = false
	mu.Unlock()

	_ = run("pkill", "-9", "-f", "warp-rotate.sh")
	_ = run("pkill", "-9", "-f", "warp-watch.sh")
	_ = run("pkill", "-9", "-f", "usque")

	_ = run("systemctl", "stop", serviceName)
	_ = run("systemctl", "disable", serviceName)
	_ = os.Remove("/etc/systemd/system/" + serviceName)
	_ = run("systemctl", "daemon-reload")
	_ = run("systemctl", "stop", "warp-watch.timer")
	_ = run("systemctl", "disable", "warp-watch.timer")
	_ = os.Remove("/etc/systemd/system/warp-watch.timer")
	_ = os.Remove("/etc/systemd/system/warp-watch.service")
	_ = os.RemoveAll("/etc/usque")
	_ = os.Remove(usqueBin)
	_ = os.Remove(rotateBin)
	_ = os.Remove(watchBin)
	_ = os.Remove("/run/warp-watch.state")
	_ = os.Remove("/run/warp-rotate.lock")
	_ = os.Remove("/run/warp-install.lock")
	_ = os.Remove(watchLog)
	_ = os.Remove(installLog)
	removeCronEntry("warp-watch.sh")
	cleanHostsFile()
	_ = run("ip", "rule", "del", "fwmark", "10000", "table", "10000", "priority", "99")
	_ = run("ip", "-6", "rule", "del", "fwmark", "10000", "table", "10000", "priority", "99")
	_ = run("ip6tables", "-D", "OUTPUT", "-m", "mark", "--mark", "10000", "!", "-o", "warp", "-j", "REJECT")
	_ = run("ip", "link", "del", "warp")
	return nil
}

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

// Start starts the usque systemd service and watchdog timer.
func Start() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("ip", "link", "del", "warp")
	if err := run("systemctl", "start", serviceName); err != nil {
		return err
	}
	_ = run("systemctl", "start", "warp-watch.timer")
	go func() {
		time.Sleep(4 * time.Second)
		if out, err := runOutput("curl", "--interface", "warp", "-4", "-s", "--max-time", "3", "https://1.1.1.1/"); err != nil || len(out) == 0 {
			_ = RotateIP()
		}
	}()
	return nil
}

// Stop stops the usque systemd service and watchdog timer.
func Stop() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("systemctl", "stop", "warp-watch.timer")
	if err := run("systemctl", "stop", serviceName); err != nil {
		return err
	}
	_ = run("ip", "link", "del", "warp")
	return nil
}

// Restart restarts the usque systemd service and recovers connection if dead.
func Restart() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("systemctl", "stop", serviceName)
	_ = run("ip", "link", "del", "warp")
	if err := run("systemctl", "start", serviceName); err != nil {
		return err
	}
	_ = run("systemctl", "start", "warp-watch.timer")
	go func() {
		time.Sleep(4 * time.Second)
		if out, err := runOutput("curl", "--interface", "warp", "-4", "-s", "--max-time", "3", "https://1.1.1.1/"); err != nil || len(out) == 0 {
			_ = RotateIP()
		}
	}()
	return nil
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	rotateCancel = cancel
	mu.Unlock()

	go func() {
		defer func() {
			mu.Lock()
			rotating = false
			rotateCancel = nil
			mu.Unlock()
			cancel()
		}()
		cmd := exec.CommandContext(ctx, "bash", rotateBin, "--force")
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
	_, err := runOutput(name, args...)
	return err
}

func runOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("%s: превышен лимит времени %s", name, defaultCmdTimeout)
	}
	return string(out), err
}

func cleanHostsFile() {
	content, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return
	}
	lines := strings.Split(string(content), "\n")
	var newLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "ff02::2 ip6-allrouters") {
			newLines = append(newLines, "ff02::2 ip6-allrouters")
			continue
		}
		if strings.Contains(line, "api.cloudflareclient.com") {
			continue
		}
		newLines = append(newLines, line)
	}
	_ = os.WriteFile("/etc/hosts", []byte(strings.Join(newLines, "\n")), 0o644)
}
