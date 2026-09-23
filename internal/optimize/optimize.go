// Package optimize applies OS-level tuning for weak VPS (low CPU/RAM):
// BBR + TCP buffer tuning via sysctl, a swap file, and stable DNS servers.
// Everything is Linux-specific and must run as root.
package optimize

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// sysctlConfBBR and sysctlConfTCP are separate drop-ins so BBR and
	// TCP buffer tuning can be enabled independently of each other.
	sysctlConfBBR = "/etc/sysctl.d/99-xui-bbr.conf"
	sysctlConfTCP = "/etc/sysctl.d/99-xui-tcp.conf"
	// swapPath is the swap file used on weak servers.
	swapPath = "/swapfile"
)

// Options selects which tuning steps Apply runs.
type Options struct {
	DNS      bool `json:"dns"`
	BBR      bool `json:"bbr"`
	TCP      bool `json:"tcp"` // TCP buffer/queue tuning
	Swap     bool `json:"swap"`
	SwapSize int  `json:"swapSize"` // MiB, 0 = keep default (1024)
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
		if len(st.DNSResolvers) == 2 && st.DNSResolvers[0] == "1.1.1.1" && st.DNSResolvers[1] == "8.8.8.8" {
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
		if err := applyDNS(); err != nil {
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

func applyDNS() error {
	if err := run("chattr", "-i", "/etc/resolv.conf"); err != nil {
		// chattr may be unavailable or the file not immutable — continue.
	}
	content := "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"
	if err := os.WriteFile("/etc/resolv.conf", []byte(content), 0o644); err != nil {
		return err
	}
	_ = run("chattr", "+i", "/etc/resolv.conf")
	return nil
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
	return run("sysctl", "--system")
}

func applyTCP() error {
	if err := os.WriteFile(sysctlConfTCP, []byte(tcpSysctlConf), 0o644); err != nil {
		return err
	}
	return run("sysctl", "--system")
}

func applySwap(sizeMiB int) error {
	if _, err := os.Stat(swapPath); err == nil {
		return nil // already exists
	}
	// Prefer fallocate; fall back to dd when the filesystem rejects it.
	if err := run("fallocate", "-l", fmt.Sprintf("%dM", sizeMiB), swapPath); err != nil {
		if err := run("dd", "if=/dev/zero", "of="+swapPath, "bs=1M", fmt.Sprintf("count=%d", sizeMiB)); err != nil {
			return err
		}
	}
	if err := run("chmod", "600", swapPath); err != nil {
		return err
	}
	if err := run("mkswap", swapPath); err != nil {
		return err
	}
	if err := run("swapon", swapPath); err != nil {
		return err
	}
	fstab, _ := os.ReadFile("/etc/fstab")
	if !strings.Contains(string(fstab), swapPath) {
		entry := swapPath + " none swap sw 0 0\n"
		f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(entry); err != nil {
			return err
		}
	}
	return nil
}

// Revert removes the swap file and the panel-managed sysctl tuning.
func Revert() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("swapoff", swapPath)
	_ = os.Remove(swapPath)
	if data, err := os.ReadFile("/etc/fstab"); err == nil {
		lines := strings.Split(string(data), "\n")
		filtered := lines[:0]
		for _, l := range lines {
			if strings.Contains(l, swapPath) {
				continue
			}
			filtered = append(filtered, l)
		}
		if err := os.WriteFile("/etc/fstab", []byte(strings.Join(filtered, "\n")), 0o644); err != nil {
			return err
		}
	}
	_ = os.Remove(sysctlConfBBR)
	_ = os.Remove(sysctlConfTCP)
	return run("sysctl", "--system")
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
