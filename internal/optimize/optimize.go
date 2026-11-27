// Package optimize applies OS-level tuning for weak VPS (low CPU/RAM):
// BBR + TCP buffer tuning via sysctl, a swap file, and stable DNS servers.
// Everything is Linux-specific and must run as root.
package optimize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

const (
	// sysctlConfBBR and sysctlConfTCP are separate drop-ins so BBR and
	// TCP buffer tuning can be enabled independently of each other.
	sysctlConfBBR = "/etc/sysctl.d/99-xui-bbr.conf"
	sysctlConfTCP = "/etc/sysctl.d/99-xui-tcp.conf"
	// swapPath is the swap file used on weak servers.
	swapPath = "/swapfile"
	// resolvConfPath is the system DNS configuration file.
	resolvConfPath = "/etc/resolv.conf"
	// legacyDNSMarker is the pre-1.4.3 marker location, read for compatibility.
	legacyDNSMarker = "/etc/x-ui-dns-managed"
	// legacyResolvBackup is the backup path checked by x-ui.sh uninstall.
	legacyResolvBackup = "/var/backups/x-ui/etc-resolv.conf"
)

// dnsMarkerPath returns the managed-DNS marker inside the writable DB folder;
// ProtectSystem=full keeps /etc read-only for new files.
func dnsMarkerPath() string {
	return filepath.Join(config.GetDBFolderPath(), "x-ui-dns-managed")
}

// backupPath stores pre-optimization resolv.conf and sysctl values so Revert
// can undo live kernel parameters and restore DNS without rebooting.
func backupPath() string {
	return filepath.Join(config.GetDBFolderPath(), "optimize-backup.json")
}

type optimizeBackup struct {
	DNSBackupSaved bool              `json:"dnsBackupSaved,omitempty"`
	ResolvSymlink  string            `json:"resolvSymlink,omitempty"`
	ResolvContent  string            `json:"resolvContent,omitempty"`
	Sysctl         map[string]string `json:"sysctl,omitempty"`
}

type sysctlDefault struct {
	key       string
	fallbacks []string
}

var (
	bbrSysctlDefaults = []sysctlDefault{
		{key: "net.core.default_qdisc", fallbacks: []string{"fq_codel", "pfifo_fast"}},
		{key: "net.ipv4.tcp_congestion_control", fallbacks: []string{"cubic", "reno"}},
	}
	tcpSysctlDefaults = []sysctlDefault{
		{key: "net.ipv4.tcp_rmem", fallbacks: []string{"4096 131072 6291456"}},
		{key: "net.ipv4.tcp_wmem", fallbacks: []string{"4096 16384 4194304"}},
		{key: "net.core.somaxconn", fallbacks: []string{"4096"}},
		{key: "net.ipv4.tcp_max_syn_backlog", fallbacks: []string{"512"}},
		{key: "net.core.netdev_max_backlog", fallbacks: []string{"1000"}},
		{key: "net.ipv4.tcp_tw_reuse", fallbacks: []string{"2", "0"}},
		{key: "net.ipv4.tcp_fin_timeout", fallbacks: []string{"60"}},
	}
)

// Overridable paths and runners for unit tests.
var (
	optResolvPath         = resolvConfPath
	optSysctlBBRPath      = sysctlConfBBR
	optSysctlTCPPath      = sysctlConfTCP
	optSwapPath           = swapPath
	optLegacyDNSMarker    = legacyDNSMarker
	optLegacyResolvBackup = legacyResolvBackup
	optProcSwapsPath      = "/proc/swaps"
	optRunHostScript      = runHostScript
	optRun                = run
	optRunOutput          = runOutput
	optRequireLinux       = requireLinux
)

// DefaultDNSResolvers is the resolver pair used when none is supplied.
var DefaultDNSResolvers = []string{"1.1.1.1", "1.0.0.1"}

// Options selects which tuning steps Apply runs.
type Options struct {
	DNS          bool     `json:"dns"`
	DNSResolvers []string `json:"dnsResolvers,omitempty"`
	BBR          bool     `json:"bbr"`
	TCP          bool     `json:"tcp"` // TCP buffer/queue tuning
	Swap         bool     `json:"swap"`
	SwapSize     int      `json:"swapSize"` // MiB, 0 = keep default (1024)
}

// ErrUnsupportedPlatform is returned on non-Linux systems.
var ErrUnsupportedPlatform = errors.New("optimization is only supported on Linux")

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return ErrUnsupportedPlatform
	}
	return nil
}

// Status reports the current tuning state.
type Status struct {
	BBREnabled        bool     `json:"bbrEnabled"`
	TCPBufferTuned    bool     `json:"tcpBufferTuned"`
	SwapEnabled       bool     `json:"swapEnabled"`
	SwapSizeMiB       int64    `json:"swapSizeMiB"`
	SwapActive        bool     `json:"swapActive"`
	DNSSet            bool     `json:"dnsSet"`
	DNSResolvers      []string `json:"dnsResolvers"`
	PlatformSupported bool     `json:"platformSupported"`
	Error             string   `json:"error,omitempty"`
}

// GetStatus reads the live system state.
func GetStatus() Status {
	st := Status{PlatformSupported: true}
	if err := optRequireLinux(); err != nil {
		st.PlatformSupported = false
		st.Error = err.Error()
		return st
	}

	st.BBREnabled = liveBBREnabled()
	st.TCPBufferTuned = liveTCPTuned()
	if _, err := os.Stat(optSwapPath); err == nil {
		st.SwapEnabled = true
		if out, err := optRunOutput("wc", "-c", optSwapPath); err == nil {
			fields := strings.Fields(out)
			if len(fields) > 0 {
				var bytes int64
				if _, err := fmt.Sscanf(fields[0], "%d", &bytes); err == nil {
					st.SwapSizeMiB = bytes >> 20
				}
			}
		}
	}
	out, _ := optRunOutput("cat", "/proc/swaps")
	st.SwapActive = strings.Contains(out, optSwapPath)

	if data, err := os.ReadFile(optResolvPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "nameserver" {
				st.DNSResolvers = append(st.DNSResolvers, fields[1])
			}
		}
		if hasDNSMarker() {
			st.DNSSet = true
		}
	}
	return st
}

// Apply runs the selected tuning steps in order: DNS, BBR/sysctl, then swap.
func Apply(opts Options) ([]string, error) {
	if err := optRequireLinux(); err != nil {
		return nil, err
	}
	var steps []string
	if opts.DNS {
		if err := applyDNS(opts.DNSResolvers); err != nil {
			return steps, fmt.Errorf("DNS: %w", err)
		}
		steps = append(steps, "dns")
	}
	if opts.BBR {
		if err := applyBBR(); err != nil {
			return steps, fmt.Errorf("BBR: %w", err)
		}
		steps = append(steps, "bbr")
	}
	if opts.TCP {
		if err := applyTCP(); err != nil {
			return steps, fmt.Errorf("TCP tuning: %w", err)
		}
		steps = append(steps, "tcp")
	}
	if opts.Swap {
		size := opts.SwapSize
		if size <= 0 {
			size = 1024
		}
		if err := applySwap(size); err != nil {
			return steps, fmt.Errorf("swap: %w", err)
		}
		steps = append(steps, "swap")
	}
	if len(steps) == 0 {
		return nil, errors.New("no tuning steps selected")
	}
	return steps, nil
}

func applyDNS(resolvers []string) error {
	normalized, err := normalizeDNSResolvers(resolvers)
	if err != nil {
		return err
	}
	if err := saveDNSBackup(); err != nil {
		return err
	}
	var b strings.Builder
	for _, ip := range normalized {
		b.WriteString("nameserver ")
		b.WriteString(ip)
		b.WriteByte('\n')
	}
	if err := writeResolvConf(b.String()); err != nil {
		return err
	}
	return os.WriteFile(dnsMarkerPath(), []byte(strings.Join(normalized, "\n")+"\n"), 0o644)
}

// normalizeDNSResolvers validates and deduplicates user-supplied resolvers.
func normalizeDNSResolvers(in []string) ([]string, error) {
	if len(in) == 0 {
		return DefaultDNSResolvers, nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid DNS resolver %q", ip)
		}
		if seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	if len(out) == 0 {
		return DefaultDNSResolvers, nil
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out, nil
}

const bbrSysctlConf = `# Managed by x-ui panel optimization (BBR)
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
`

const tcpSysctlConf = `# Managed by x-ui panel optimization (TCP buffers)
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.core.somaxconn = 4096
net.ipv4.tcp_max_syn_backlog = 4096
net.core.netdev_max_backlog = 4096
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
`

func applyBBR() error {
	saveSysctlBackup(optSysctlBBRPath, bbrSysctlDefaults)
	if err := os.WriteFile(optSysctlBBRPath, []byte(bbrSysctlConf), 0o644); err != nil {
		return err
	}
	// --system also walks fs/vm keys the sandbox cannot write. Apply only our
	// net.* drop-in; /proc/sys/net is writable under ProtectSystem=full.
	return optRun("sysctl", "-p", optSysctlBBRPath)
}

func applyTCP() error {
	saveSysctlBackup(optSysctlTCPPath, tcpSysctlDefaults)
	if err := os.WriteFile(optSysctlTCPPath, []byte(tcpSysctlConf), 0o644); err != nil {
		return err
	}
	return optRun("sysctl", "-p", optSysctlTCPPath)
}

func swapActive() bool {
	data, err := os.ReadFile(optProcSwapsPath)
	return err == nil && strings.Contains(string(data), optSwapPath)
}

func applySwap(sizeMiB int) error {
	if swapActive() {
		return nil
	}
	// fallocate/swapon/fstab need CAP_SYS_ADMIN and a writable /, so run the
	// whole sequence as a transient systemd unit outside the panel sandbox.
	script := fmt.Sprintf(`set -eu
if [ ! -f %[1]s ]; then
  fallocate -l %[2]dM %[1]s || dd if=/dev/zero of=%[1]s bs=1M count=%[2]d
  chmod 600 %[1]s
  mkswap %[1]s
fi
grep -q '^%[1]s ' /proc/swaps || swapon %[1]s
grep -q '^%[1]s ' /etc/fstab || echo '%[1]s none swap sw 0 0' >> /etc/fstab
`, optSwapPath, sizeMiB)
	if err := optRunHostScript("swap", script); err != nil {
		return err
	}
	if !swapActive() {
		return fmt.Errorf("swap file %s is not active after swapon", optSwapPath)
	}
	return nil
}

// Revert removes the swap file, restores live BBR/TCP sysctl parameters, and
// unlocks/restores /etc/resolv.conf.
func Revert() error {
	if err := optRequireLinux(); err != nil {
		return err
	}
	backup := readBackup()
	var resolvTmpPath string
	if backup.DNSBackupSaved && backup.ResolvContent != "" {
		tmpPath, err := writeTempFile("xui-resolv-*", backup.ResolvContent)
		if err != nil {
			return err
		}
		resolvTmpPath = tmpPath
		defer os.Remove(resolvTmpPath)
	}

	script := buildRevertScript(backup, resolvTmpPath)
	if err := optRunHostScript("revert", script); err != nil {
		return err
	}
	_ = os.Remove(dnsMarkerPath())
	_ = os.Remove(optLegacyDNSMarker)
	_ = os.Remove(backupPath())
	return nil
}

func hasDNSMarker() bool {
	if _, err := os.Stat(dnsMarkerPath()); err == nil {
		return true
	}
	_, err := os.Stat(optLegacyDNSMarker)
	return err == nil
}

func readBackup() optimizeBackup {
	data, err := os.ReadFile(backupPath())
	if err != nil {
		return optimizeBackup{}
	}
	var b optimizeBackup
	if err := json.Unmarshal(data, &b); err != nil {
		return optimizeBackup{}
	}
	return b
}

func writeBackup(b optimizeBackup) error {
	if err := os.MkdirAll(filepath.Dir(backupPath()), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(backupPath(), data, 0o600)
}

func saveDNSBackup() error {
	b := readBackup()
	if b.DNSBackupSaved || hasDNSMarker() {
		return nil
	}
	if target, err := os.Readlink(optResolvPath); err == nil && target != "" && !strings.ContainsAny(target, "\x00\r\n") {
		b.ResolvSymlink = target
	}
	if data, err := os.ReadFile(optResolvPath); err == nil && len(data) > 0 {
		b.ResolvContent = string(data)
	}
	if b.ResolvSymlink == "" && b.ResolvContent == "" {
		return nil
	}
	b.DNSBackupSaved = true
	return writeBackup(b)
}

func saveSysctlBackup(confPath string, defaults []sysctlDefault) {
	if _, err := os.Stat(confPath); err == nil {
		return
	}
	b := readBackup()
	if b.Sysctl == nil {
		b.Sysctl = make(map[string]string, len(defaults))
	}
	changed := false
	for _, d := range defaults {
		if _, exists := b.Sysctl[d.key]; exists {
			continue
		}
		out, err := optRunOutput("sysctl", "-n", d.key)
		if err != nil {
			continue
		}
		val := sanitizeSysctlValue(out)
		if val == "" {
			continue
		}
		b.Sysctl[d.key] = val
		changed = true
	}
	if changed {
		_ = writeBackup(b)
	}
}

func writeResolvConf(content string) error {
	tmpPath, err := writeTempFile("xui-resolv-*", content)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	// Run via host script so chattr has CAP_LINUX_IMMUTABLE and symlink replacement
	// works outside ProtectSystem=full (while still updating bind-mounted files in place).
	qResolv := shellQuote(optResolvPath)
	qTmp := shellQuote(tmpPath)
	script := fmt.Sprintf(`set -eu
chattr -i %[1]s 2>/dev/null || true
if [ -L %[1]s ]; then
  rm -f %[1]s 2>/dev/null || true
fi
cat %[2]s > %[1]s
chmod 644 %[1]s 2>/dev/null || true
chattr +i %[1]s 2>/dev/null || true
`, qResolv, qTmp)
	return optRunHostScript("dns", script)
}

func writeTempFile(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func buildRevertScript(backup optimizeBackup, resolvTmpPath string) string {
	var b strings.Builder
	b.WriteString("set -eu\n")

	qSwap := shellQuote(optSwapPath)
	fmt.Fprintf(&b, "swapoff %s 2>/dev/null || true\n", qSwap)
	fmt.Fprintf(&b, "rm -f %s\n", qSwap)
	fmt.Fprintf(&b, "if [ -f /etc/fstab ] && grep -q %s /etc/fstab 2>/dev/null; then\n", shellQuote("^"+optSwapPath+" "))
	fmt.Fprintf(&b, "  sed -i '\\#^%s #d' /etc/fstab\n", optSwapPath)
	b.WriteString("fi\n")

	qBBRConf := shellQuote(optSysctlBBRPath)
	qTCPConf := shellQuote(optSysctlTCPPath)
	bbrPresent := fileExists(optSysctlBBRPath) || hasAnyBackupKey(backup.Sysctl, bbrSysctlDefaults) || liveBBREnabled()
	tcpPresent := fileExists(optSysctlTCPPath) || hasAnyBackupKey(backup.Sysctl, tcpSysctlDefaults) || liveTCPTuned()

	fmt.Fprintf(&b, "if [ -f %s ]; then rm -f %s; fi\n", qBBRConf, qBBRConf)
	fmt.Fprintf(&b, "if [ -f %s ]; then rm -f %s; fi\n", qTCPConf, qTCPConf)

	if bbrPresent || tcpPresent {
		if bbrPresent {
			appendSysctlFallbacks(&b, bbrSysctlDefaults, backup.Sysctl)
		}
		if tcpPresent {
			appendSysctlFallbacks(&b, tcpSysctlDefaults, backup.Sysctl)
		}
		b.WriteString("sysctl --system >/dev/null 2>&1 || true\n")
		appendSavedSysctl(&b, bbrSysctlDefaults, backup.Sysctl)
		appendSavedSysctl(&b, tcpSysctlDefaults, backup.Sysctl)
	}

	if shouldRevertDNS(backup) {
		appendDNSRevert(&b, backup, resolvTmpPath)
	}
	return b.String()
}

func appendSysctlFallbacks(b *strings.Builder, defs []sysctlDefault, saved map[string]string) {
	for _, d := range defs {
		if v := sanitizeSysctlValue(saved[d.key]); v != "" {
			continue
		}
		for i, fb := range d.fallbacks {
			if i > 0 {
				b.WriteString(" || ")
			}
			fmt.Fprintf(b, "sysctl -qw %s 2>/dev/null", shellQuote(d.key+"="+fb))
		}
		b.WriteString(" || true\n")
	}
}

func appendSavedSysctl(b *strings.Builder, defs []sysctlDefault, saved map[string]string) {
	for _, d := range defs {
		v := sanitizeSysctlValue(saved[d.key])
		if v == "" {
			continue
		}
		fmt.Fprintf(b, "sysctl -qw %s 2>/dev/null || true\n", shellQuote(d.key+"="+v))
	}
}

func appendDNSRevert(b *strings.Builder, backup optimizeBackup, resolvTmpPath string) {
	qResolv := shellQuote(optResolvPath)
	fmt.Fprintf(b, "chattr -i %s 2>/dev/null || true\n", qResolv)

	switch {
	case backup.DNSBackupSaved && backup.ResolvSymlink != "" && !strings.ContainsAny(backup.ResolvSymlink, "\x00\r\n"):
		qTarget := shellQuote(backup.ResolvSymlink)
		if resolvTmpPath != "" {
			fmt.Fprintf(b, "ln -sfn %s %s 2>/dev/null || { cat %s > %s && chmod 644 %s 2>/dev/null || true; }\n",
				qTarget, qResolv, shellQuote(resolvTmpPath), qResolv, qResolv)
		} else {
			fmt.Fprintf(b, "ln -sfn %s %s 2>/dev/null || true\n", qTarget, qResolv)
		}
	case backup.DNSBackupSaved && resolvTmpPath != "":
		fmt.Fprintf(b, "if [ -L %s ]; then rm -f %s 2>/dev/null || true; fi\n", qResolv, qResolv)
		fmt.Fprintf(b, "cat %s > %s\n", shellQuote(resolvTmpPath), qResolv)
		fmt.Fprintf(b, "chmod 644 %s 2>/dev/null || true\n", qResolv)
	default:
		qLegacy := shellQuote(optLegacyResolvBackup)
		fmt.Fprintf(b, `if [ -s %[2]s ]; then
  if [ -L %[1]s ]; then rm -f %[1]s 2>/dev/null || true; fi
  cat %[2]s > %[1]s
  chmod 644 %[1]s 2>/dev/null || true
elif [ -s /run/systemd/resolve/stub-resolv.conf ] && command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet systemd-resolved; then
  ln -sfn /run/systemd/resolve/stub-resolv.conf %[1]s 2>/dev/null || { cat /run/systemd/resolve/stub-resolv.conf > %[1]s && chmod 644 %[1]s 2>/dev/null || true; }
elif [ -s /run/resolvconf/resolv.conf ] && command -v resolvconf >/dev/null 2>&1; then
  resolvconf -u 2>/dev/null || { cat /run/resolvconf/resolv.conf > %[1]s && chmod 644 %[1]s 2>/dev/null || true; }
elif [ -s /run/NetworkManager/resolv.conf ] && command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet NetworkManager; then
  cat /run/NetworkManager/resolv.conf > %[1]s
  chmod 644 %[1]s 2>/dev/null || true
fi
`, qResolv, qLegacy)
	}
}

func shouldRevertDNS(backup optimizeBackup) bool {
	if backup.DNSBackupSaved || hasDNSMarker() || fileExists(optLegacyResolvBackup) {
		return true
	}
	data, err := os.ReadFile(optResolvPath)
	if err != nil {
		return false
	}
	var resolvers []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "nameserver" {
			resolvers = append(resolvers, fields[1])
		}
	}
	return len(resolvers) == 2 && resolvers[0] == "1.1.1.1" &&
		(resolvers[1] == "1.0.0.1" || resolvers[1] == "8.8.8.8")
}

func liveBBREnabled() bool {
	out, _ := optRunOutput("sysctl", "-n", "net.ipv4.tcp_congestion_control")
	return strings.Contains(strings.ToLower(out), "bbr")
}

func liveTCPTuned() bool {
	out, _ := optRunOutput("sysctl", "-n", "net.ipv4.tcp_rmem")
	return strings.Contains(out, "16777216")
}

func hasAnyBackupKey(saved map[string]string, defs []sysctlDefault) bool {
	for _, d := range defs {
		if sanitizeSysctlValue(saved[d.key]) != "" {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sanitizeSysctlValue(raw string) string {
	cleaned := strings.Join(strings.Fields(raw), " ")
	if cleaned == "" {
		return ""
	}
	for i := 0; i < len(cleaned); i++ {
		c := cleaned[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '.' || c == '-' || c == ' ' {
			continue
		}
		return ""
	}
	return cleaned
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func run(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runHostScript runs a bash script as a transient systemd unit so privileged
// operations (swapon, /etc/fstab) escape NoNewPrivileges and the seccomp set.
func runHostScript(action, script string) error {
	path, lookErr := exec.LookPath("systemd-run")
	if lookErr != nil {
		cmd := exec.CommandContext(context.Background(), "bash", "-s")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	unit := fmt.Sprintf("ex3ui-opt-%s-%d", action, time.Now().UnixNano())
	cmd := exec.CommandContext(context.Background(), path, "--quiet", "--wait", "--pipe", "--collect", "--unit", unit, "/bin/bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
