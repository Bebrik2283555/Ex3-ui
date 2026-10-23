package sidecar

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Prepare writes the runtime files the sidecar binary reads before start.
func (i Instance) Prepare() error {
	switch i.Protocol {
	case model.QWDTT:
		return i.writeQWDTTStore()
	case model.OLCRTC:
		return i.writeOLCRTCConfig()
	case model.CSQTT:
		// csqtt owns its SQLite database and creates it on first start.
		if i.CSQTT != nil {
			if err := os.MkdirAll(i.CSQTT.ConfigDir, 0o700); err != nil {
				return err
			}
			syncCSQTTStoreVersion(i.CSQTT.ConfigDir)
		}
	}
	return nil
}

// csqttStoreStamp identifies the bundled csqtt build and its SQLite layout.
// Keep it independent of the main password: changing a client or main password
// must not wipe traffic counters and operator-created clients.
const csqttStoreStamp = "csqtt-2.5.0-ex3ui-2"

// syncCSQTTStoreVersion drops the cached SQLite store only when the bundled
// csqtt build changes. Stores written by 1.4.2 used a password-bound stamp;
// they are compatible, so the database is kept and only the stamp is rewritten.
func syncCSQTTStoreVersion(dir string) {
	if dir == "" {
		return
	}
	stamp := filepath.Join(dir, "ex3ui-store")
	legacyStamp := filepath.Join(dir, "ex3ui-password")
	if data, err := os.ReadFile(stamp); err == nil {
		prev := strings.TrimSpace(string(data))
		if prev == csqttStoreStamp {
			return
		}
		if isLegacyCSQTTStamp(prev) {
			_ = os.Remove(legacyStamp)
			_ = os.WriteFile(stamp, []byte(csqttStoreStamp), 0o600)
			return
		}
	} else if data, err := os.ReadFile(legacyStamp); err == nil {
		if isLegacyCSQTTStamp(strings.TrimSpace(string(data))) {
			_ = os.Remove(legacyStamp)
			_ = os.WriteFile(stamp, []byte(csqttStoreStamp), 0o600)
			return
		}
	}
	// Also drop stores written before the stamp existed.
	for _, name := range []string{"csqtt.db", "csqtt.db-wal", "csqtt.db-shm"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	_ = os.Remove(legacyStamp)
	_ = os.WriteFile(stamp, []byte(csqttStoreStamp), 0o600)
}

func isLegacyCSQTTStamp(stamp string) bool {
	return strings.HasPrefix(stamp, "csqtt-2.5.0-legacy|") ||
		stamp == "csqtt-2.5.0"
}

// writeQWDTTStore merges the panel clients into {configDir}/passwords.json.
// Unknown fields (including device bindings) are preserved; disabled clients
// are removed so a disabled connection cannot stay usable.
func (i Instance) writeQWDTTStore() error {
	cfg := i.QWDTT
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return errors.New("qWDTT: config dir is not set")
	}
	if strings.Contains(cfg.ConfigDir, "..") {
		return errors.New("qWDTT: invalid config dir")
	}
	path := filepath.Join(cfg.ConfigDir, "passwords.json")
	// Keep the whole root: qwdtt stores device bindings and traffic counters
	// next to "passwords", and dropping them would unbind every device.
	root := map[string]json.RawMessage{}
	passwords := map[string]map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("qWDTT: invalid passwords.json: %w", err)
		}
		if raw, ok := root["passwords"]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &passwords); err != nil {
				return fmt.Errorf("qWDTT: invalid passwords.json: %w", err)
			}
		}
	}
	if passwords == nil {
		passwords = map[string]map[string]any{}
	}

	// qwdtt rewrites passwords.json with its own struct, so a per-entry marker
	// would be lost. Keep the panel-owned password list in a separate ledger.
	ledgerPath := filepath.Join(cfg.ConfigDir, "ex3ui-clients.json")
	ledger := readLedger(ledgerPath)
	keep := make(map[string]bool, len(i.Clients))
	for _, cl := range i.Clients {
		pass := strings.TrimSpace(cl.QwdttPassword)
		if pass == "" {
			pass = strings.TrimSpace(cl.Password)
		}
		if pass == "" || !cl.Enable {
			continue
		}
		keep[pass] = true
		ledger[pass] = true
		entry := passwords[pass]
		if entry == nil {
			entry = map[string]any{}
		}
		entry["label"] = strings.TrimSpace(cl.Email)
		entry["is_deactivated"] = false
		if cl.ExpiryTime > 0 {
			entry["expires_at"] = cl.ExpiryTime / 1000
		} else {
			delete(entry, "expires_at")
		}
		hashes := cfg.Hashes
		if v := cfg.HashesByEmail[cl.Email]; strings.TrimSpace(v) != "" {
			hashes = v
		}
		if hashes = strings.TrimSpace(hashes); hashes != "" {
			entry["vk_hash"] = hashes
		} else {
			delete(entry, "vk_hash")
		}
		passwords[pass] = entry
	}
	for pass := range ledger {
		if !keep[pass] {
			delete(passwords, pass)
			delete(ledger, pass)
		}
	}
	rawPasswords, err := json.Marshal(passwords)
	if err != nil {
		return err
	}
	root["passwords"] = rawPasswords
	if err := os.MkdirAll(cfg.ConfigDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return writeLedger(ledgerPath, ledger)
}

// writeOLCRTCConfig renders the server YAML and seeds the display-name
// dictionaries the bundled olcrtc build requires.
func (i Instance) writeOLCRTCConfig() error {
	cfg := i.OLCRTC
	if cfg == nil || strings.TrimSpace(cfg.ConfigFile) == "" {
		return errors.New("olcRTC: config file is not set")
	}
	if strings.Contains(cfg.ConfigFile, "..") || strings.Contains(cfg.DataDir, "..") {
		return errors.New("olcRTC: invalid path")
	}
	if err := ensureNameDictionaries(cfg.DataDir); err != nil {
		return fmt.Errorf("olcRTC: seed data dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o700); err != nil {
		return fmt.Errorf("olcRTC: create config dir: %w", err)
	}
	return os.WriteFile(cfg.ConfigFile, []byte(olcrtcYAML(cfg)), 0o600)
}

func olcrtcYAML(cfg *OLCRTCConfig) string {
	var b strings.Builder
	b.WriteString("mode: srv\n")
	b.WriteString("auth:\n  provider: " + yamlString(cfg.Provider) + "\n")
	b.WriteString("room:\n  id: " + yamlString(cfg.RoomID) + "\n")
	if cfg.CryptoKey != "" {
		b.WriteString("crypto:\n  key: " + yamlString(cfg.CryptoKey) + "\n")
	}
	b.WriteString("net:\n  transport: " + yamlString(cfg.Transport) + "\n")
	if cfg.DNS != "" {
		b.WriteString("  dns: " + yamlString(cfg.DNS) + "\n")
	}
	switch cfg.Transport {
	case "vp8channel":
		b.WriteString("vp8:\n  fps: " + strconv.Itoa(clampInt(cfg.VP8Fps, 1, 120)) +
			"\n  batch_size: " + strconv.Itoa(clampInt(cfg.VP8Batch, 1, 64)) + "\n")
	case "seichannel":
		b.WriteString("sei:\n  fps: " + strconv.Itoa(clampInt(cfg.SeiFps, 1, 120)) +
			"\n  batch_size: " + strconv.Itoa(clampInt(cfg.SeiBatch, 1, 64)) +
			"\n  fragment_size: " + strconv.Itoa(cfg.SeiFrag) +
			"\n  ack_timeout_ms: " + strconv.Itoa(cfg.SeiAck) + "\n")
	case "videochannel":
		codec := cfg.VideoCodec
		if codec != "tile" {
			codec = "qrcode"
		}
		b.WriteString("video:\n  width: " + strconv.Itoa(cfg.VideoW) +
			"\n  height: " + strconv.Itoa(cfg.VideoH) +
			"\n  fps: " + strconv.Itoa(clampInt(cfg.VideoFps, 1, 120)) +
			"\n  codec: " + yamlString(codec) + "\n")
	}
	if cfg.RouteThroughXray && cfg.RouteXrayPort > 0 {
		b.WriteString("socks:\n  proxy_addr: \"127.0.0.1\"\n  proxy_port: " + strconv.Itoa(cfg.RouteXrayPort) +
			"\n  proxy_user: \"\"\n  proxy_pass: \"\"\n")
	}
	b.WriteString("liveness:\n  interval: 10s\n  timeout: 15s\n  failures: 4\n")
	b.WriteString("data: " + yamlString(cfg.DataDir) + "\n")
	b.WriteString("debug: " + strconv.FormatBool(cfg.Debug) + "\n")
	return b.String()
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func yamlString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return "\"" + s + "\""
}

const (
	defaultNames = `Alexander
Alexey
Anatoly
Andrey
Anton
Artyom
Boris
Denis
Dmitry
Egor
Evgeny
Ivan
Igor
Ilya
Kirill
Leonid
Maxim
Mikhail
Nikita
Nikolay
Oleg
Pavel
Roman
Sergei
Stepan
Timofey
Viktor
Vladimir
Anna
Anastasia
Daria
Ekaterina
Elena
Irina
Ksenia
Maria
Natalia
Olga
Polina
Svetlana
Tatiana
Vera
Victoria
Yulia`
	defaultSurnames = `Antonov
Belov
Volkov
Gavrilov
Danilov
Egorov
Efimov
Zhukov
Zaitsev
Ivanov
Ilyin
Kozlov
Kuznetsov
Lebedev
Makarov
Melnikov
Morozov
Nikolaev
Novikov
Orlov
Pavlov
Petrov
Popov
Romanov
Savin
Smirnov
Sokolov
Stepanov
Tarasov
Titov
Fedorov
Filatov
Gusev
Tsvetkov
Chernov
Shevtsov
Shcherbakov
Yuriev
Yakovlev`
)

func ensureNameDictionaries(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, pair := range [...]struct{ name, dict string }{
		{"names", defaultNames},
		{"surnames", defaultSurnames},
	} {
		path := filepath.Join(dir, pair.name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			continue
		}
		if err := os.WriteFile(path, []byte(pair.dict), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// ensureExecutable fixes the exec bit lost by scp/zip uploads.
func ensureExecutable(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("make %s executable: %w", path, err)
	}
	return nil
}
