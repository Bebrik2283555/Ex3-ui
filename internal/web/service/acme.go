package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// acmeMu serializes certificate issuance so two inbound dialogs can't run
// acme.sh standalone validation against the same port 80 at once.
var acmeMu sync.Mutex

// domainNameRegex matches lower-cased hostnames only (no scheme, port, IP or
// wildcard) so an attacker-supplied value never reaches acme.sh as a flag.
var domainNameRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidateDomainName rejects IPs, scheme prefixes, ports and wildcards so an
// attacker-supplied value can never reach acme.sh as a flag or path.
func ValidateDomainName(raw string) error {
	d := strings.ToLower(strings.TrimSpace(raw))
	if !domainNameRegex.MatchString(d) || net.ParseIP(d) != nil {
		return errors.New("invalid domain name")
	}
	return nil
}

// acmeHome resolves the home directory even when the systemd unit ships an
// empty or missing HOME (acme.sh refuses to run then): prefer a non-empty
// HOME env, then the passwd entry, then /root as a last resort.
func acmeHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/root"
}

// acmeEnv returns the process environment plus a guaranteed HOME (systemd
// service shells often lack it, and acme.sh refuses to run without it).
// Pre-existing HOME/LE_WORKING_DIR entries are dropped first — getenv() stops
// at the first match, so an appended duplicate would lose to a stale empty
// value inherited from the service unit.
func acmeEnv(home string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "LE_WORKING_DIR=") {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"HOME="+home,
		"LE_WORKING_DIR="+filepath.Join(home, ".acme.sh"),
	)
}

// IssueCertificate provisions a TLS certificate for domain via acme.sh in
// standalone mode (same flow as the x-ui.sh wizard): installs acme.sh when
// missing, requires port 80 free, installs into /root/cert/<domain>/ and lets
// acme.sh's own cron renew it (reloadcmd restarts the panel so Xray picks up
// the new files).
func (s *ServerService) IssueCertificate(domain string) (map[string]any, error) {
	if err := ValidateDomainName(domain); err != nil {
		return nil, err
	}
	acmeMu.Lock()
	defer acmeMu.Unlock()

	home := acmeHome()
	acmeBin, err := ensureAcmeSh()
	if err != nil {
		return nil, err
	}
	if err := checkPort80Free(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	env := acmeEnv(home)

	args := []string{"--issue", "-d", domain, "--standalone", "--force"}
	if !hasGlobalIPv4() {
		args = append(args, "--listen-v6")
	}
	cmd := exec.CommandContext(ctx, acmeBin, args...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		logger.Warning("acme issue failed for", domain, ":", strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("acme issue failed: %w", err)
	}

	keyFile := filepath.Join("/root", "cert", domain, "privkey.pem")
	fullchain := filepath.Join("/root", "cert", domain, "fullchain.pem")
	certDir := filepath.Join("/root", "cert", domain)
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return nil, fmt.Errorf("acme cert dir create failed: %w", err)
	}
	installArgs := []string{
		"--install-cert", "-d", domain,
		"--key-file", keyFile,
		"--fullchain-file", fullchain,
		"--reloadcmd", "systemctl restart x-ui 2>/dev/null || rc-service x-ui restart 2>/dev/null || true",
	}
	installCmd := exec.CommandContext(ctx, acmeBin, installArgs...)
	installCmd.Env = env
	if out, err := installCmd.CombinedOutput(); err != nil {
		// acme.sh may report "Reload error" and exit non-zero while the cert
		// files are already in place — verify the files, not the exit code.
		if !certPairExists(certDir) {
			logger.Warning("acme install-cert failed for", domain, ":", strings.TrimSpace(string(out)))
			return nil, fmt.Errorf("acme install-cert failed: %w", err)
		}
		logger.Warning("acme install-cert reloadcmd failed for", domain, ", cert files already in place:", strings.TrimSpace(string(out)))
	}

	return map[string]any{
		"domain":   domain,
		"certFile": fullchain,
		"keyFile":  keyFile,
	}, nil
}

// ensureAcmeSh returns the path to acme.sh, installing it (curl | sh, same as
// the x-ui.sh wizard) when it is missing from the panel's home directory.
func ensureAcmeSh() (string, error) {
	home := acmeHome()
	acmeBin := filepath.Join(home, ".acme.sh", "acme.sh")
	if info, err := os.Stat(acmeBin); err == nil && !info.IsDir() {
		return acmeBin, nil
	}
	installCmd := exec.Command("sh", "-c", "curl -s https://get.acme.sh | sh")
	installCmd.Env = acmeEnv(home)
	out, err := installCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("acme.sh install failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(acmeBin); err != nil {
		return "", errors.New("acme.sh install finished but ~/.acme.sh/acme.sh is missing")
	}
	return acmeBin, nil
}

// checkPort80Free ensures nothing else binds the HTTP-01 validation port, the
// most common reason a standalone issue attempt fails.
func checkPort80Free() error {
	l, err := net.Listen("tcp4", ":80")
	if err != nil {
		return errors.New("port 80 is not available for certificate validation (is a web server running?)")
	}
	_ = l.Close()
	return nil
}

// hasGlobalIPv4 mirrors x-ui.sh's acme_listen_flag: acme.sh binds IPv4 by
// default, so force v6-only only when the host has no public IPv4 at all.
func hasGlobalIPv4() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return true
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip4 := ipn.IP.To4(); ip4 != nil && ip4.IsGlobalUnicast() && !ip4.IsPrivate() {
				return true
			}
		}
	}
	return false
}

// CleanupUnusedAcmeCerts removes certificates our acme flow created for the
// inbound's tlsSettings, but only when no remaining inbound references them.
// Best-effort: revokes/untracks in acme.sh, then deletes the files.
func CleanupUnusedAcmeCerts(streamSettings string, selfID int) {
	for _, domain := range acmeCertPathsFromStreamSettings(streamSettings) {
		if acmeCertInUse(domain, selfID) {
			continue
		}
		removeAcmeCert(domain)
	}
}

// acmeCertPathsFromStreamSettings extracts /root/cert/<domain>/ certificate
// file paths from an inbound's stream settings JSON (paths our own flow
// creates; anything else is left alone).
func acmeCertPathsFromStreamSettings(streamSettings string) []string {
	if streamSettings == "" {
		return nil
	}
	var parsed struct {
		TLSSettings struct {
			Certificates []struct {
				CertificateFile string `json:"certificateFile"`
			} `json:"certificates"`
		} `json:"tlsSettings"`
	}
	if err := json.Unmarshal([]byte(streamSettings), &parsed); err != nil {
		return nil
	}
	var domains []string
	for _, cert := range parsed.TLSSettings.Certificates {
		if domain := acmeDomainFromCertFile(cert.CertificateFile); domain != "" {
			domains = append(domains, domain)
		}
	}
	return domains
}

// acmeDomainFromCertFile returns the domain when path matches
// /root/cert/<valid domain>/..., empty otherwise.
func acmeDomainFromCertFile(path string) string {
	const prefix = "/root/cert/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	domain, after, _ := strings.Cut(rest, "/")
	if after == "" || ValidateDomainName(domain) != nil {
		return ""
	}
	return domain
}

// acmeCertInUse reports whether another inbound still references the domain's
// cert files. True on a lookup error — never delete on uncertainty.
func acmeCertInUse(domain string, selfID int) bool {
	db := database.GetDB()
	var count int64
	pattern := "/root/cert/" + domain + "/"
	if err := db.Model(&model.Inbound{}).Where("id <> ? AND stream_settings LIKE ?", selfID, "%"+pattern+"%").Count(&count).Error; err != nil {
		logger.Warning("acme cert in-use check failed:", err)
		return true
	}
	return count > 0
}

// findAcmeSh returns the acme.sh path when installed (never installs it).
func findAcmeSh() string {
	home := acmeHome()
	path := filepath.Join(home, ".acme.sh", "acme.sh")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// removeAcmeCert tears down a certificate: revoke + untrack in acme.sh (so
// the renewal cron stops recreating it), drop its state dirs, then delete the
// /root/cert/<domain>/ files.
func removeAcmeCert(domain string) {
	home := acmeHome()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if acmeBin := findAcmeSh(); acmeBin != "" {
		env := acmeEnv(home)
		revokeCmd := exec.CommandContext(ctx, acmeBin, "--revoke", "-d", domain)
		revokeCmd.Env = env
		if out, err := revokeCmd.CombinedOutput(); err != nil {
			logger.Debug("acme revoke skipped for", domain, ":", strings.TrimSpace(string(out)))
		}
		removeCmd := exec.CommandContext(ctx, acmeBin, "--remove", "-d", domain)
		removeCmd.Env = env
		if out, err := removeCmd.CombinedOutput(); err != nil {
			logger.Warning("acme remove failed for", domain, ":", strings.TrimSpace(string(out)))
		}
		_ = os.RemoveAll(filepath.Join(home, ".acme.sh", domain))
		_ = os.RemoveAll(filepath.Join(home, ".acme.sh", domain+"_ecc"))
	}
	_ = os.RemoveAll(filepath.Join("/root", "cert", domain))
}
