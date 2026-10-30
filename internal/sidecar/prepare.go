package sidecar

// Runtime files written into an inbound's data directory before its core is
// spawned.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Names of the files this stage owns inside a core's config directory.
const (
	qwdttStoreName         = "passwords.json"
	qwdttOwnerPasswordName = "owner-password"
	csqttStampFileName     = "ex3ui-store"
	csqttStoreStamp        = "csqtt-2.5.0-ex3ui-2"
	csqttLegacyName        = "ex3ui-password"
	csqttLegacyExact       = "csqtt-2.5.0"
	csqttLegacyPrefix      = "csqtt-2.5.0-legacy|"
	olcrtcNamesName        = "names"
	olcrtcSurnamesName     = "surnames"
)

// Prepare writes the files one core needs before it is started.
func (i Instance) Prepare() error {
	switch i.Protocol {
	case model.QWDTT:
		return i.prepareQWDTT()
	case model.OLCRTC:
		return i.prepareOLCRTC()
	case model.CSQTT:
		return i.prepareCSQTT()
	}
	return nil
}

// qwdttCredential is the per-client secret the qWDTT child keys everything by.
func qwdttCredential(client model.Client) string {
	if secret := strings.TrimSpace(client.QwdttPassword); secret != "" {
		return secret
	}
	return strings.TrimSpace(client.Password)
}

// csqttCredential has no fallback: a share-only member has no admin identity.
func csqttCredential(client model.Client) string {
	return csqttPasswordForClient(client)
}

// sidecarClientPassword is the panel-side core credential of one client.
func sidecarClientPassword(protocol model.Protocol, client model.Client) string {
	switch protocol {
	case model.QWDTT:
		return qwdttCredential(client)
	case model.CSQTT:
		return csqttPasswordForClient(client)
	}
	return ""
}

// csqttPasswordForClient is the trimmed CSQTT credential; there is no fallback.
func csqttPasswordForClient(client model.Client) string {
	return strings.TrimSpace(client.CsqttPassword)
}

// prepareQWDTT writes the merged store, the owner-password file and the ledger
// of credentials the panel has created in that core.
func (i Instance) prepareQWDTT() error {
	if err := i.writeQWDTTStore(); err != nil {
		return err
	}
	if i.QWDTT == nil {
		return nil
	}
	return writeQwdttOwnerPassword(i.QWDTT.ConfigDir, i.QWDTT.MainPass)
}

// qwdttOwnerPasswordPath is the file that carries the owner password to the
// child; argv is readable by every local user in /proc/<pid>/cmdline.
func qwdttOwnerPasswordPath(dir string) string {
	return filepath.Join(dir, qwdttOwnerPasswordName)
}

// writeQwdttOwnerPassword rewrites the 0600 password file, or removes it when
// no password is configured so the child never reads a stale secret.
func writeQwdttOwnerPassword(dir, password string) error {
	path := qwdttOwnerPasswordPath(dir)
	if strings.TrimSpace(password) == "" {
		removeQuietly(path)
		return nil
	}
	if err := os.WriteFile(path, []byte(password), 0o600); err != nil {
		return fmt.Errorf("qWDTT: write owner password: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// writeQWDTTStore merges the panel's clients into the child's password store
// without ever discarding keys the child owns, then records the ledger.
func (i Instance) writeQWDTTStore() error {
	cfg := i.QWDTT
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return errors.New("qWDTT: config dir is not set")
	}
	dir := cfg.ConfigDir
	if strings.Contains(dir, "..") {
		return errors.New("qWDTT: invalid config dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	storePath := filepath.Join(dir, qwdttStoreName)
	document, passwords, err := loadQWDTTStore(storePath)
	if err != nil {
		return err
	}

	ledger := readLedger(filepath.Join(dir, csqttLedgerFileSuffix))
	wanted := make(map[string]struct{}, len(i.Clients))
	for _, client := range i.Clients {
		credential := qwdttCredential(client)
		if credential == "" || !client.Enable {
			continue
		}
		wanted[credential] = struct{}{}
		entry := passwords[credential]
		if entry == nil {
			entry = make(map[string]json.RawMessage)
		}
		entry["label"] = rawJSON(strings.TrimSpace(client.Email))
		entry["is_deactivated"] = json.RawMessage("false")
		if client.ExpiryTime > 0 {
			entry["expires_at"] = rawJSON(client.ExpiryTime / 1000)
		} else {
			delete(entry, "expires_at")
		}
		if hash := hashFor(cfg.HashesByEmail, client.Email, cfg.Hashes); hash != "" {
			entry["vk_hash"] = rawJSON(hash)
		} else {
			delete(entry, "vk_hash")
		}
		passwords[credential] = entry
	}
	for _, credential := range ledger {
		if _, keep := wanted[credential]; keep {
			continue
		}
		delete(passwords, credential)
	}

	merged, err := json.Marshal(passwords)
	if err != nil {
		return err
	}
	document["passwords"] = json.RawMessage(merged)
	rendered, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(storePath, rendered, 0o600); err != nil {
		return err
	}
	return writeLedger(filepath.Join(dir, csqttLedgerFileSuffix), setKeys(wanted))
}

// loadQWDTTStore reads the child's store as raw values so untouched keys keep
// their exact meaning. A missing or unreadable file is an empty document.
func loadQWDTTStore(path string) (map[string]json.RawMessage, map[string]map[string]json.RawMessage, error) {
	document := make(map[string]json.RawMessage)
	passwords := make(map[string]map[string]json.RawMessage)

	data, err := os.ReadFile(path)
	if err != nil {
		return document, passwords, nil
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, nil, fmt.Errorf("qWDTT: invalid passwords.json: %w", err)
	}
	if document == nil {
		document = make(map[string]json.RawMessage)
	}
	raw, ok := document["passwords"]
	if !ok {
		return document, passwords, nil
	}
	if err := json.Unmarshal(raw, &passwords); err != nil {
		return nil, nil, fmt.Errorf("qWDTT: invalid passwords.json: %w", err)
	}
	if passwords == nil {
		passwords = make(map[string]map[string]json.RawMessage)
	}
	return document, passwords, nil
}

func rawJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(encoded)
}

// hashFor resolves the per-email override, then the default hash list.
func hashFor(byEmail map[string]string, email, fallback string) string {
	if hash := strings.TrimSpace(byEmail[email]); hash != "" {
		return hash
	}
	return strings.TrimSpace(fallback)
}

// prepareCSQTT only guarantees the directory and the store stamp; the child
// creates its SQLite database on first start.
func (i Instance) prepareCSQTT() error {
	cfg := i.CSQTT
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return nil
	}
	if err := os.MkdirAll(cfg.ConfigDir, 0o700); err != nil {
		return err
	}
	syncCSQTTStoreVersion(cfg.ConfigDir)
	return nil
}

// syncCSQTTStoreVersion reconciles the store stamp. Every failure is invisible: a
// broken stamp must never fail a start.
func syncCSQTTStoreVersion(dir string) {
	stampPath := filepath.Join(dir, csqttStampFileName)
	legacyPath := filepath.Join(dir, csqttLegacyName)

	if data, err := os.ReadFile(stampPath); err == nil {
		switch value := strings.TrimSpace(string(data)); {
		case value == csqttStoreStamp:
			return
		case csqttLegacyCompatible(value):
			removeQuietly(legacyPath)
		default:
			dropCSQTTStore(dir)
			removeQuietly(legacyPath)
		}
		writeStamp(stampPath)
		return
	}

	if data, err := os.ReadFile(legacyPath); err == nil && csqttLegacyCompatible(strings.TrimSpace(string(data))) {
		removeQuietly(legacyPath)
	} else {
		dropCSQTTStore(dir)
		removeQuietly(legacyPath)
	}
	writeStamp(stampPath)
}

func csqttLegacyCompatible(value string) bool {
	return value == csqttLegacyExact || strings.HasPrefix(value, csqttLegacyPrefix)
}

// dropCSQTTStore discards the cached database so the child rebuilds it.
func dropCSQTTStore(dir string) {
	for _, name := range []string{"csqtt.db", "csqtt.db-wal", "csqtt.db-shm"} {
		removeQuietly(filepath.Join(dir, name))
	}
}

func removeQuietly(path string) {
	_ = os.Remove(path)
}

func writeStamp(path string) {
	_ = os.WriteFile(path, []byte(csqttStoreStamp), 0o600)
}

// prepareOLCRTC seeds the core's dictionaries once and regenerates the YAML
// that describes this inbound.
func (i Instance) prepareOLCRTC() error {
	cfg := i.OLCRTC
	if cfg == nil || strings.TrimSpace(cfg.ConfigFile) == "" {
		return errors.New("olcRTC: config file is not set")
	}
	if strings.Contains(cfg.ConfigFile, "..") || strings.Contains(cfg.DataDir, "..") {
		return errors.New("olcRTC: invalid path")
	}
	if strings.TrimSpace(cfg.DataDir) != "" {
		if err := seedOLCRTCDataDir(cfg.DataDir); err != nil {
			return fmt.Errorf("olcRTC: seed data dir: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o700); err != nil {
		return fmt.Errorf("olcRTC: create config dir: %w", err)
	}
	return os.WriteFile(cfg.ConfigFile, []byte(olcrtcYAML(cfg)), 0o600)
}

// seedOLCRTCDataDir writes each dictionary only when no regular file is there
// yet: an operator-edited file is never overwritten.
func seedOLCRTCDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, file := range []struct{ name, body string }{
		{olcrtcNamesName, olcrtcNames},
		{olcrtcSurnamesName, olcrtcSurnames},
	} {
		path := filepath.Join(dir, file.name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			continue
		}
		if err := os.WriteFile(path, []byte(file.body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// olcrtcYAML renders the child's server configuration.
func olcrtcYAML(cfg *OLCRTCConfig) string {
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}

	line("mode: srv")
	line("auth:")
	line("  provider: %s", yamlQuoted(cfg.Provider))
	line("room:")
	line("  id: %s", yamlQuoted(cfg.RoomID))
	if cfg.CryptoKey != "" {
		line("crypto:")
		line("  key: %s", yamlQuoted(cfg.CryptoKey))
	}
	line("net:")
	line("  transport: %s", yamlQuoted(cfg.Transport))
	if cfg.DNS != "" {
		line("  dns: %s", yamlQuoted(cfg.DNS))
	}
	switch cfg.Transport {
	case "vp8channel":
		line("vp8:")
		line("  fps: %d", clampInt(cfg.VP8Fps, 1, 120))
		line("  batch_size: %d", clampInt(cfg.VP8Batch, 1, 64))
	case "seichannel":
		line("sei:")
		line("  fps: %d", clampInt(cfg.SeiFps, 1, 120))
		line("  batch_size: %d", clampInt(cfg.SeiBatch, 1, 64))
		line("  fragment_size: %d", cfg.SeiFrag)
		line("  ack_timeout_ms: %d", cfg.SeiAck)
	case "videochannel":
		line("video:")
		line("  width: %d", cfg.VideoW)
		line("  height: %d", cfg.VideoH)
		line("  fps: %d", clampInt(cfg.VideoFps, 1, 120))
		line("  codec: %s", yamlQuoted(cfg.VideoCodec))
	}
	if cfg.RouteThroughXray && cfg.RouteXrayPort > 0 {
		line("socks:")
		line("  proxy_addr: \"127.0.0.1\"")
		line("  proxy_port: %d", cfg.RouteXrayPort)
		line("  proxy_user: \"\"")
		line("  proxy_pass: \"\"")
	}
	line("liveness:")
	line("  interval: 10s")
	line("  timeout: 15s")
	line("  failures: 4")
	line("data: %s", yamlQuoted(cfg.DataDir))
	line("debug: %t", cfg.Debug)
	return b.String()
}

// yamlQuoted escapes only backslash, quote and newline, in that order; every
// other byte, including non-ASCII, is copied through untouched.
func yamlQuoted(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// readLedger loads the panel-authored credential list. Anything unusable is an
// empty ledger, never an error.
func readLedger(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ledger []string
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil
	}
	return ledger
}

func writeLedger(path string, credentials []string) error {
	if path == "" {
		return nil
	}
	if credentials == nil {
		credentials = []string{}
	}
	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func setKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

// ensureExecutable makes the core binary runnable; Windows has no exec bit.
func ensureExecutable(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("make %s executable: %w", path, err)
	}
	return nil
}
