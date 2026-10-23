// Package optimize applies OS-level tuning for weak VPS (low CPU/RAM):
// BBR + TCP buffer tuning via sysctl, a swap file, and stable DNS servers.
// Everything is Linux-specific and must run as root.
package optimize

import (
	"context"
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
	// legacyDNSMarker is the pre-1.4.3 marker location, read for compatibility.
	legacyDNSMarker = "/etc/x-ui-dns-managed"
)

// dnsMarkerPath returns the managed-DNS marker inside the writable DB folder;
// ProtectSystem=full keeps /etc read-only for new files.
func dnsMarkerPath() string {
	return filepath.Join(config.GetDBFolderPath(), "x-ui-dns-managed")
}

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
	if err := requireLinux(); err != nil {
		st.PlatformSupported = false
		st.Error = err.Error()
		return st
	}

	out, _ := runOutput("sysctl", "-n", "net.ipv4.tcp_congestion_control")
	if strings.Contains(strings.ToLower(out), "bbr") {
		st.BBREnabled = true
	}
	out, _ = runOutput("sysctl", "-n", "net.ipv4.tcp_rmem")
	if strings.Contains(out, "16777216") {
		st.TCPBufferTuned = true
	}
	if _, err := os.Stat(swapPath); err == nil {
		st.SwapEnabled = true
		if out, err := runOutput("wc", "-c", swapPath); err == nil {
			fields := strings.Fields(out)
			if len(fields) > 0 {
				var bytes int64
				if _, err := fmt.Sscanf(fields[0], "%d", &bytes); err == nil {
					st.SwapSizeMiB = bytes >> 20
				}
			}
		}
	}
	out, _ = runOutput("cat", "/proc/swaps")
	st.SwapActive = strings.Contains(out, swapPath)

	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "nameserver" {
				st.DNSResolvers = append(st.DNSResolvers, fields[1])
			}
		}
		_, markerErr := os.Stat(dnsMarkerPath())
		if markerErr != nil {
			_, markerErr = os.Stat(legacyDNSMarker)
		}
		if markerErr == nil {
			st.DNSSet = true
		} else if len(st.DNSResolvers) == 2 && st.DNSResolvers[0] == "1.1.1.1" &&
			(st.DNSResolvers[1] == "1.0.0.1" || st.DNSResolvers[1] == "8.8.8.8") {
			st.DNSSet = true
		}
	}
	return st
}

// Apply runs the selected tuning steps in order: DNS, BBR/sysctl, then swap.
func Apply(opts Options) ([]string, error) {
	if err := requireLinux(); err != nil {
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
	_ = run("chattr", "-i", "/etc/resolv.conf")
	// chattr may be unavailable or the file not immutable — continue.
	var b strings.Builder
	for _, ip := range normalized {
		b.WriteString("nameserver ")
		b.WriteString(ip)
		b.WriteByte('\n')
	}
	if err := os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644); err != nil {
		return err
	}
	_ = run("chattr", "+i", "/etc/resolv.conf")
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
	if err := os.WriteFile(sysctlConfBBR, []byte(bbrSysctlConf), 0o644); err != nil {
		return err
	}
	// --system also walks fs/vm keys the sandbox cannot write. Apply only our
	// net.* drop-in; /proc/sys/net is writable under ProtectSystem=full.
	return run("sysctl", "-p", sysctlConfBBR)
}

func applyTCP() error {
	if err := os.WriteFile(sysctlConfTCP, []byte(tcpSysctlConf), 0o644); err != nil {
		return err
	}
	return run("sysctl", "-p", sysctlConfTCP)
}

func swapActive() bool {
	data, err := os.ReadFile("/proc/swaps")
	return err == nil && strings.Contains(string(data), swapPath)
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
`, swapPath, sizeMiB)
	if err := runHostScript("swap", script); err != nil {
		return err
	}
	if !swapActive() {
		return fmt.Errorf("swap file %s is not active after swapon", swapPath)
	}
	return nil
}

// Revert removes the swap file and the panel-managed sysctl tuning.
func Revert() error {
	if err := requireLinux(); err != nil {
		return err
	}
	script := fmt.Sprintf("swapoff %[1]s 2>/dev/null || true\nrm -f %[1]s\nsed -i '\\#^%[1]s #d' /etc/fstab\nrm -f %[2]s %[3]s\n",
		swapPath, sysctlConfBBR, sysctlConfTCP)
	if err := runHostScript("revert", script); err != nil {
		return err
	}
	_ = os.Remove(dnsMarkerPath())
	_ = os.Remove(legacyDNSMarker)
	return nil
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
