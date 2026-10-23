package sidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A password change must not drop the csqtt SQLite store: it holds traffic
// counters and clients created in csqtt's own panel.
func TestSyncCSQTTStoreVersionKeepsDBOnPasswordStamp(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "csqtt.db")
	if err := os.WriteFile(db, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ex3ui-password"),
		[]byte("csqtt-2.5.0-legacy|old-password"), 0o600); err != nil {
		t.Fatal(err)
	}

	syncCSQTTStoreVersion(dir)

	if _, err := os.Stat(db); err != nil {
		t.Fatalf("csqtt.db was removed after a password-stamp migration: %v", err)
	}
	stamp, err := os.ReadFile(filepath.Join(dir, "ex3ui-store"))
	if err != nil || string(stamp) != csqttStoreStamp {
		t.Fatalf("store stamp = %q, %v; want %q", stamp, err, csqttStoreStamp)
	}
	if _, err := os.Stat(filepath.Join(dir, "ex3ui-password")); !os.IsNotExist(err) {
		t.Fatalf("legacy password stamp was not removed: %v", err)
	}
}

// A different bundled csqtt build is not compatible with the cached store.
func TestSyncCSQTTStoreVersionDropsIncompatibleDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "csqtt.db")
	if err := os.WriteFile(db, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ex3ui-store"), []byte("csqtt-2.1.9"), 0o600); err != nil {
		t.Fatal(err)
	}

	syncCSQTTStoreVersion(dir)

	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("incompatible csqtt.db was kept: %v", err)
	}
}

// Clearing a client's expiry or VK hash must remove the old values instead of
// leaving them in qwdtt's store.
func TestWriteQWDTTStoreClearsStaleExpiryAndHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwords.json")
	seed := `{"passwords":{"p1":{"label":"old","expires_at":123,"vk_hash":"old-hash","device_id":"keep"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := Instance{
		Protocol: model.QWDTT,
		QWDTT:    &QWDTTConfig{ConfigDir: dir},
		Clients:  []model.Client{{Email: "a@x", QwdttPassword: "p1", Enable: true}},
	}
	if err := inst.writeQWDTTStore(); err != nil {
		t.Fatalf("writeQWDTTStore: %v", err)
	}

	var root struct {
		Passwords map[string]map[string]any `json:"passwords"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	entry := root.Passwords["p1"]
	if _, ok := entry["expires_at"]; ok {
		t.Fatalf("expires_at survived after the expiry was cleared: %v", entry)
	}
	if _, ok := entry["vk_hash"]; ok {
		t.Fatalf("vk_hash survived after the hash was cleared: %v", entry)
	}
	if entry["device_id"] != "keep" {
		t.Fatalf("unknown fields were not preserved: %v", entry)
	}

	inst.Clients[0].ExpiryTime = 1893456000000 // 2030-01-01
	inst.QWDTT.Hashes = "new-hash"
	if err := inst.writeQWDTTStore(); err != nil {
		t.Fatalf("writeQWDTTStore: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	entry = root.Passwords["p1"]
	if entry["expires_at"] != float64(1893456000) {
		t.Fatalf("expires_at = %v, want 1893456000", entry["expires_at"])
	}
	if entry["vk_hash"] != "new-hash" {
		t.Fatalf("vk_hash = %v, want new-hash", entry["vk_hash"])
	}
}
