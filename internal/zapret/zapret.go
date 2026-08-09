// Package zapret manages the transparent DPI-bypass tool: it installs the
// bundled nfqws binary and config into /opt/zapret, creates the systemd
// unit, and exposes the editable domain lists (autohosts/ignore).
package zapret

import (
	"archive/zip"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util"
)

//go:embed all:assets
var assets embed.FS

const (
	installDir  = "/opt/zapret"
	serviceName = "zapret.service"
)

var errUnsupported = errors.New("zapret is only supported on Linux")

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return errUnsupported
	}
	return nil
}

// archDir maps GOARCH to the bundled binary subfolder.
func archDir() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	case "riscv64":
		return "riscv64"
	default:
		return ""
	}
}

// IsInstalled reports whether zapret has been installed to /opt/zapret.
func IsInstalled() bool {
	_, err := os.Stat(filepath.Join(installDir, "system", "nfqws"))
	return err == nil
}

// Install copies the binaries bundled into the panel into /opt/zapret, selects
// the nfqws binary for this architecture and writes a systemd unit.
// firewallType is "iptables" or "nftables"; ifaceWan/ifaceLan are
// space-separated interface lists (may be empty).
func Install(firewallType, ifaceWan, ifaceLan string) error {
	return installFromFS(assets, "assets", firewallType, ifaceWan, ifaceLan)
}

// installFromFS installs zapret from any file tree (the embedded assets or the
// unwrapped contents of a downloaded zip archive) into /opt/zapret. base is the
// directory inside src whose child bins/ and files/ layout is used.
func installFromFS(src fs.FS, base, firewallType, ifaceWan, ifaceLan string) error {
	if err := requireLinux(); err != nil {
		return err
	}
	if firewallType != "iptables" && firewallType != "nftables" {
		return fmt.Errorf("invalid firewall type %q", firewallType)
	}
	if err := os.RemoveAll(installDir); err != nil {
		return err
	}
	// Copy files/ tree.
	if err := fs.WalkDir(src, base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, base)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(installDir, rel), 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(installDir, rel)
		mode := fs.FileMode(0o644)
		if strings.HasSuffix(dst, ".sh") || strings.HasSuffix(dst, "nfqws") {
			mode = 0o755
		}
		return os.WriteFile(dst, data, mode)
	}); err != nil {
		return err
	}

	arch := archDir()
	if arch == "" {
		return fmt.Errorf("unsupported architecture %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	srcBin := filepath.Join(installDir, "bins", arch, "nfqws")
	dstBin := filepath.Join(installDir, "system", "nfqws")
	data, err := os.ReadFile(srcBin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dstBin, data, 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(installDir, "bins"))

	writeCfg := func(name, value string) error {
		return os.WriteFile(filepath.Join(installDir, "system", name), []byte(value+"\n"), 0o644)
	}
	if err := writeCfg("FWTYPE", firewallType); err != nil {
		return err
	}
	if err := writeCfg("IFACE_WAN", ifaceWan); err != nil {
		return err
	}
	if err := writeCfg("IFACE_LAN", ifaceLan); err != nil {
		return err
	}

	unit, err := os.ReadFile(filepath.Join(installDir, "files", "system", serviceName))
	if err != nil {
		return err
	}
	unitDst := "/etc/systemd/system/" + serviceName
	if err := os.WriteFile(unitDst, unit, 0o644); err != nil {
		return err
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "enable", serviceName); err != nil {
		return err
	}
	return Start()
}

// InstallFromZip downloads a zapret release zip from url, unpacks it and
// installs it with the same layout as Install. The archive may wrap the files
// in a single top-level folder; the root that holds bins/ and files/ is picked
// automatically.
func InstallFromZip(url, firewallType, ifaceWan, ifaceLan string) error {
	if err := requireLinux(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "zapret-dl")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	zipPath := filepath.Join(dir, "zapret.zip")
	if err := util.DownloadTo(url, zipPath, 0o644); err != nil {
		return err
	}
	if err := unzip(zipPath, dir); err != nil {
		return err
	}
	base := findInstallRoot(dir)
	if base == "" {
		return errors.New("archive does not contain the zapret bins/ and files/ layout")
	}
	return installFromFS(os.DirFS(base), ".", firewallType, ifaceWan, ifaceLan)
}

// findInstallRoot returns the directory that directly contains the bins/ and
// files/ layout, unwrapping a single top-level wrapper folder if present.
func findInstallRoot(dir string) string {
	if hasZapretLayout(dir) {
		return dir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && hasZapretLayout(filepath.Join(dir, e.Name())) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func hasZapretLayout(dir string) bool {
	if b, err := os.Stat(filepath.Join(dir, "bins")); err != nil || !b.IsDir() {
		return false
	}
	if f, err := os.Stat(filepath.Join(dir, "files")); err != nil || !f.IsDir() {
		return false
	}
	return true
}

// unzip extracts the zip archive into dst, guarding against path traversal.
func unzip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}
		target := filepath.Join(dst, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall stops and removes the systemd unit and /opt/zapret.
func Uninstall() error {
	if err := requireLinux(); err != nil {
		return err
	}
	_ = run("systemctl", "stop", serviceName)
	_ = run("systemctl", "disable", serviceName)
	_ = os.Remove("/etc/systemd/system/" + serviceName)
	if err := run("systemctl", "daemon-reload"); err != nil {
		// A missing unit still lets us continue cleanup.
	}
	if _, err := os.Stat(installDir); err == nil {
		if err := os.RemoveAll(installDir); err != nil {
			return err
		}
	}
	return nil
}

// Start enables and starts the zapret service.
func Start() error {
	if err := requireLinux(); err != nil {
		return err
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	return run("systemctl", "start", serviceName)
}

// Stop stops the zapret service.
func Stop() error {
	if err := requireLinux(); err != nil {
		return err
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	return run("systemctl", "stop", serviceName)
}

// Restart restarts the zapret service.
func Restart() error {
	if err := requireLinux(); err != nil {
		return err
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	return run("systemctl", "restart", serviceName)
}

// Status describes the installed/running state of zapret.
type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Firewall  string `json:"firewall"`
	Enabled   bool   `json:"enabled"`
	Error     string `json:"error,omitempty"`
}

// GetStatus inspects the live zapret state.
func GetStatus() Status {
	st := Status{}
	if err := requireLinux(); err != nil {
		st.Error = err.Error()
		return st
	}
	st.Installed = IsInstalled()
	if st.Installed {
		if data, err := os.ReadFile(filepath.Join(installDir, "system", "FWTYPE")); err == nil {
			st.Firewall = strings.TrimSpace(string(data))
		}
		if out, err := runOutput("systemctl", "is-active", serviceName); err == nil && strings.TrimSpace(out) == "active" {
			st.Running = true
		}
		if out, err := runOutput("systemctl", "is-enabled", serviceName); err == nil && strings.TrimSpace(out) == "enabled" {
			st.Enabled = true
		}
	}
	return st
}

// Hosts returns the current autohosts (bypass) and ignore lists.
type Hosts struct {
	Bypass []string `json:"bypass"`
	Ignore []string `json:"ignore"`
}

// GetHosts reads the domain lists.
func GetHosts() (Hosts, error) {
	var h Hosts
	var err error
	h.Bypass, err = readLines(filepath.Join(installDir, "autohosts.txt"))
	if err != nil {
		return h, err
	}
	h.Ignore, err = readLines(filepath.Join(installDir, "ignore.txt"))
	return h, err
}

// SetHosts writes the domain lists and restarts the service to apply them.
func SetHosts(h Hosts) error {
	if err := requireLinux(); err != nil {
		return err
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	if err := writeLines(filepath.Join(installDir, "autohosts.txt"), h.Bypass); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(installDir, "ignore.txt"), h.Ignore); err != nil {
		return err
	}
	return Restart()
}

// Logs returns recent service output from journald.
func Logs(lines int) []string {
	if err := requireLinux(); err != nil {
		return nil
	}
	if lines <= 0 {
		lines = 200
	}
	out, err := runOutput("journalctl", "-u", serviceName, "--no-pager", "-n", fmt.Sprintf("%d", lines))
	if err != nil {
		return nil
	}
	parts := strings.Split(out, "\n")
	return parts
}

// EditableFiles lists the files users may view/edit/back up from the panel.
// The names are fixed so the controller can never address an arbitrary path.
var EditableFiles = []string{"autohosts.txt", "ignore.txt", "whitelist.txt", "ipset.txt", "youtube.txt", "config.txt"}

func editable(name string) bool {
	for _, f := range EditableFiles {
		if f == name {
			return true
		}
	}
	return false
}

func requireEditable(name string) error {
	if !editable(name) {
		return fmt.Errorf("not an editable zapret file: %s", name)
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	return nil
}

// GetFile returns the raw content of one editable zapret file.
func GetFile(name string) (string, error) {
	if err := requireLinux(); err != nil {
		return "", err
	}
	if err := requireEditable(name); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(installDir, name))
	return string(data), err
}

// GetAllFiles returns the raw content of every editable zapret file.
func GetAllFiles() (map[string]string, error) {
	if err := requireLinux(); err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, name := range EditableFiles {
		data, err := os.ReadFile(filepath.Join(installDir, name))
		if err != nil {
			return nil, err
		}
		out[name] = string(data)
	}
	return out, nil
}

// SetFile writes the raw content of one editable zapret file verbatim (comments
// and blank lines included) and restarts the service to apply the change.
func SetFile(name, content string) error {
	if err := requireLinux(); err != nil {
		return err
	}
	if err := requireEditable(name); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(installDir, name), []byte(content), 0o644); err != nil {
		return err
	}
	return Restart()
}

// BackupZip writes an archive with every editable zapret file into w, for the
// in-panel backup button.
func BackupZip(w io.Writer) error {
	if err := requireLinux(); err != nil {
		return err
	}
	if !IsInstalled() {
		return errors.New("zapret is not installed")
	}
	zw := zip.NewWriter(w)
	for _, name := range EditableFiles {
		data, err := os.ReadFile(filepath.Join(installDir, name))
		if err != nil {
			return err
		}
		fw, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func writeLines(path string, lines []string) error {
	var b strings.Builder
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		b.WriteString(l + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
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
