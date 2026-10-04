package extra

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateStoreDiscovery disables wdtt password-store discovery outside the
// configured dir, so tests only write inside their temp dirs.
func isolateStoreDiscovery(t *testing.T) {
	t.Helper()
	oldCommon, oldProc := wdttCommonDirs, wdttProcessDirs
	t.Cleanup(func() { wdttCommonDirs, wdttProcessDirs = oldCommon, oldProc })
	wdttCommonDirs = nil
	wdttProcessDirs = func() []string { return nil }
}

// TestClientSubscription: each client has its own subscription document.
func TestClientSubscription(t *testing.T) {
	store := memStore{}
	m := NewManager(store)
	cfg := Config{
		SubToken:   "tok",
		SubHost:    "203.0.113.10:56000",
		ListenAddr: "0.0.0.0:56000",
		Clients: []WDTTClient{
			{Name: "Alice", SubscriptionName: "Alice VPN", SubscriptionDescription: "For Alice", Password: "alice-pass", VkHashes: "a-h", Enabled: true, SubURI: "uri0001"},
			{Name: "Bob", SubscriptionName: "Bob VPN", Password: "bob-pass", VkHashes: "b-h", Enabled: true, SubURI: "uri0002"},
		},
	}
	raw, err := json.Marshal(cfg.Merge(WDTT))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetString(WDTT.settingKey(), string(raw)); err != nil {
		t.Fatal(err)
	}

	doc, err := m.ClientSubscription(WDTT, "uri0001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.SubscriptionName != "Alice VPN" {
		t.Errorf("subscriptionName = %q, want 'Alice VPN'", doc.SubscriptionName)
	}
	if doc.Description != "For Alice" {
		t.Errorf("description = %q, want 'For Alice'", doc.Description)
	}
	if len(doc.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(doc.Profiles))
	}
	profile := doc.Profiles[0]
	if profile.Name != "Alice" {
		t.Errorf("profile name = %q, want 'Alice'", profile.Name)
	}
	if profile.Peer != "203.0.113.10:56000" {
		t.Errorf("profile peer = %q, want '203.0.113.10:56000'", profile.Peer)
	}
	if profile.Hashes != "a-h" {
		t.Errorf("profile hashes = %q, want 'a-h'", profile.Hashes)
	}
	if profile.Password != "alice-pass" {
		t.Errorf("profile password = %q, want 'alice-pass'", profile.Password)
	}

	doc2, err := m.ClientSubscription(WDTT, "uri0002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc2.SubscriptionName != "Bob VPN" {
		t.Errorf("subscriptionName = %q, want 'Bob VPN'", doc2.SubscriptionName)
	}
	if doc2.Description != "" {
		t.Errorf("description = %q, want empty", doc2.Description)
	}

	_, err = m.ClientSubscription(WDTT, "nonexistent")
	if err == nil {
		t.Error("expected error for invalid client URI")
	}

	cfg.Clients[0].Enabled = false
	raw, _ = json.Marshal(cfg.Merge(WDTT))
	_ = store.SetString(WDTT.settingKey(), string(raw))
	_, err = m.ClientSubscription(WDTT, "uri0001")
	if err == nil {
		t.Error("expected error for disabled client")
	}
}

// TestClientSubscriptionFallsBackToName: if subscriptionName is empty, use client name.
func TestClientSubscriptionFallsBackToName(t *testing.T) {
	store := memStore{}
	m := NewManager(store)
	cfg := Config{
		SubToken:   "tok",
		SubHost:    "203.0.113.10:56000",
		ListenAddr: "0.0.0.0:56000",
		Clients: []WDTTClient{
			{Name: "Alice", Password: "alice-pass", VkHashes: "a-h", Enabled: true, SubURI: "abc1234"},
		},
	}
	raw, _ := json.Marshal(cfg.Merge(WDTT))
	_ = store.SetString(WDTT.settingKey(), string(raw))
	doc, err := m.ClientSubscription(WDTT, "abc1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.SubscriptionName != "Alice" {
		t.Errorf("subscriptionName = %q, want 'Alice' (fallback to name)", doc.SubscriptionName)
	}
}

// TestSaveConfigWDTTWritesServerPasswords: saving qwdtt config with clients
// writes the server's passwords.json (shared port, per-client entries) and
// merges entries created by the server's Telegram bot.
func TestSaveConfigWDTTWritesServerPasswords(t *testing.T) {
	isolateStoreDiscovery(t)

	dir := t.TempDir()
	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.SubHost = "203.0.113.10:56000"
	cfg.Password = "main-pass"
	cfg.ConfigDir = dir
	cfg.Clients = []WDTTClient{
		{Name: "Alice", Password: "alice-pass", VkHashes: "a-h", Enabled: true},
		{Name: "Bob", Password: "bob-pass", Enabled: true},
	}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dbFile := filepath.Join(dir, "passwords.json")
	raw, err := os.ReadFile(dbFile)
	if err != nil {
		t.Fatalf("passwords.json not written: %v", err)
	}
	var db struct {
		MainPassword string                     `json:"main_password"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		t.Fatalf("parse passwords.json: %v", err)
	}
	if db.MainPassword != "main-pass" {
		t.Errorf("main_password = %q", db.MainPassword)
	}
	if len(db.Passwords) != 2 {
		t.Fatalf("passwords = %d, want 2 (Alice, Bob)", len(db.Passwords))
	}
	var alice map[string]any
	if err := json.Unmarshal(db.Passwords["alice-pass"], &alice); err != nil {
		t.Fatal(err)
	}
	if alice["label"] != "Alice" {
		t.Errorf("alice label = %v", alice["label"])
	}
	if alice["vk_hash"] != "a-h" {
		t.Errorf("alice vk_hash = %v", alice["vk_hash"])
	}
	if alice["source"] != "panel" {
		t.Errorf("alice source = %v, want 'panel'", alice["source"])
	}

	// A password created by the Telegram bot must survive the next panel sync.
	bumped := struct {
		MainPassword string                     `json:"main_password"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
	}{MainPassword: "main-pass", Passwords: map[string]json.RawMessage{
		"bot-pass":   json.RawMessage(`{"label":"Bot generated"}`),
		"alice-pass": db.Passwords["alice-pass"],
		"bob-pass":   db.Passwords["bob-pass"],
	}}
	merged, err := json.Marshal(bumped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbFile, merged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("second SaveConfig: %v", err)
	}
	raw2, _ := os.ReadFile(dbFile)
	var db2 struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw2, &db2); err != nil {
		t.Fatal(err)
	}
	if _, ok := db2.Passwords["bot-pass"]; !ok {
		t.Error("bot-created password was dropped by the panel sync")
	}
	// Deleted panel client must be removed from passwords.json.
	cfg.Clients = []WDTTClient{
		{Name: "Alice", Password: "alice-pass", VkHashes: "a-h", Enabled: true},
	}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("third SaveConfig: %v", err)
	}
	raw3, _ := os.ReadFile(dbFile)
	var db3 struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw3, &db3); err != nil {
		t.Fatal(err)
	}
	if _, ok := db3.Passwords["bob-pass"]; ok {
		t.Error("deleted panel client bob-pass should have been removed")
	}
	if _, ok := db3.Passwords["bot-pass"]; !ok {
		t.Error("bot-created password should still exist")
	}
}

// TestSyncPasswordsNoopWithoutConfigDir: SyncPasswords must be a no-op when
// the server's config dir is not configured (fresh install case).
func TestSyncPasswordsNoopWithoutConfigDir(t *testing.T) {
	m := NewManager(memStore{})
	if err := m.SyncPasswords(WDTT, DefaultConfig(WDTT)); err != nil {
		t.Fatalf("SyncPasswords without config dir: %v", err)
	}
	if err := m.SyncPasswords(OLCRTC, DefaultConfig(OLCRTC)); err != nil {
		t.Fatalf("SyncPasswords for non-qwdtt core: %v", err)
	}
}

// TestWDTTLink: quick links use the new qwdtt://config?... format.
func TestWDTTLink(t *testing.T) {
	oldDetector := publicIPDetector
	t.Cleanup(func() { publicIPDetector = oldDetector })
	publicIPDetector = func() string { return "203.0.113.50" }

	cfg := Config{
		SubHost:    "203.0.113.10:56000",
		ListenAddr: "0.0.0.0:56000",
		WGPort:     56001,
	}
	got := cfg.WDTTLink(WDTTClient{Name: "Alice", Password: "alice-pass", VkHashes: "h1,h2"})
	want := "qwdtt://config?name=Alice&peer=203.0.113.10:56000&hashes=h1,h2&workers=16&port=9000&pass=alice-pass"
	if got != want {
		t.Errorf("WDTTLink = %q, want %q", got, want)
	}
	// A client without VK hashes still gets a link (empty hashes param).
	noHashes := cfg.WDTTLink(WDTTClient{Name: "Bob", Password: "bob-pass"})
	if !strings.HasPrefix(noHashes, "qwdtt://config?name=Bob&peer=203.0.113.10:56000&hashes=&workers=16&port=9000&pass=bob-pass") {
		t.Errorf("WDTTLink without VK hashes = %q", noHashes)
	}
	if cfg.WDTTLink(WDTTClient{Name: "B", Password: "", VkHashes: "h"}) != "" {
		t.Error("WDTTLink without a password must be empty")
	}
	cfg2 := cfg
	cfg2.ListenAddr = "0.0.0.0:56200"
	got2 := cfg2.WDTTLink(WDTTClient{Name: "A", Password: "p", VkHashes: "h"})
	if !strings.HasPrefix(got2, "qwdtt://config?name=A&peer=203.0.113.10:56200&hashes=h&workers=16&port=9000&pass=p") {
		t.Errorf("WDTTLink with custom dtls port = %q", got2)
	}
	got3 := cfg.WDTTLink(WDTTClient{Password: "p", VkHashes: "h"})
	if !strings.Contains(got3, "name=Client") {
		t.Errorf("WDTTLink with empty name should use 'Client': %q", got3)
	}
	// Wildcard listen addresses fall back to the detected public IP.
	cfg3 := Config{ListenAddr: "0.0.0.0:56100"}
	got4 := cfg3.WDTTLink(WDTTClient{Name: "X", Password: "p", VkHashes: "h"})
	if !strings.HasPrefix(got4, "qwdtt://config?name=X&peer=203.0.113.50:56100") {
		t.Errorf("WDTTLink with wildcard listen should use public IP: %q", got4)
	}
	// No host anywhere: no link.
	publicIPDetector = func() string { return "" }
	if cfg3.WDTTLink(WDTTClient{Name: "X", Password: "p", VkHashes: "h"}) != "" {
		t.Error("WDTTLink without a reachable host must be empty")
	}
}

// TestValidateWDTTClients: names and unique non-server passwords are required.
func TestValidateWDTTClients(t *testing.T) {
	base := DefaultConfig(WDTT)
	base.Password = "server-pass"
	ok := base
	ok.Clients = []WDTTClient{{Name: "Alice", Password: "a-pass"}, {Name: "Bob", Password: "b-pass"}}
	if err := validateWDTTClients(ok); err != nil {
		t.Fatalf("valid clients rejected: %v", err)
	}
	cases := []struct {
		name       string
		mutate     func(c *Config)
		errContain string
	}{
		{"empty name", func(c *Config) { c.Clients[0].Name = " " }, "name is required"},
		{"empty password", func(c *Config) { c.Clients[0].Name = "A"; c.Clients[0].Password = "" }, "needs a password"},
		{"reuses server password", func(c *Config) { c.Clients[0].Password = "server-pass" }, "server password"},
		{"duplicate password", func(c *Config) { c.Clients[0].Password = "b-pass" }, "duplicate client password"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base
			cfg.Clients = []WDTTClient{{Name: "Alice", Password: "a-pass"}, {Name: "Bob", Password: "b-pass"}}
			c.mutate(&cfg)
			err := validateWDTTClients(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.errContain) {
				t.Fatalf("error %q should contain %q", err, c.errContain)
			}
		})
	}
}

// TestSaveConfigGeneratesSubURI: new clients must get a 7-char SubURI generated.
func TestSaveConfigGeneratesSubURI(t *testing.T) {
	isolateStoreDiscovery(t)

	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main-pass"
	cfg.Clients = []WDTTClient{
		{Name: "Alice", Password: "a-pass"},
		{Name: "Bob", Password: "b-pass", SubURI: "custom1"},
	}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stored, err := m.LoadConfig(WDTT)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Clients) != 2 {
		t.Fatalf("clients = %d, want 2", len(stored.Clients))
	}
	if len(stored.Clients[0].SubURI) != 7 {
		t.Errorf("SubURI for Alice = %q, want 7 chars", stored.Clients[0].SubURI)
	}
	if stored.Clients[1].SubURI != "custom1" {
		t.Errorf("SubURI for Bob = %q, want 'custom1'", stored.Clients[1].SubURI)
	}
}

// TestSaveConfigGeneratesSubToken: an empty subToken must be filled in on the
// first save and kept stable afterwards, otherwise subscription URLs would
// silently break after every save.
func TestSaveConfigGeneratesSubToken(t *testing.T) {
	isolateStoreDiscovery(t)

	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main-pass"
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stored, err := m.LoadConfig(WDTT)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SubToken) != 16 {
		t.Errorf("SubToken = %q, want 16 chars", stored.SubToken)
	}
	first := stored.SubToken
	if err := m.SaveConfig(WDTT, stored); err != nil {
		t.Fatalf("second SaveConfig: %v", err)
	}
	stored2, _ := m.LoadConfig(WDTT)
	if stored2.SubToken != first {
		t.Errorf("SubToken changed on re-save: %q -> %q", first, stored2.SubToken)
	}
	// An explicit token must never be overwritten.
	stored2.SubToken = "my-token"
	if err := m.SaveConfig(WDTT, stored2); err != nil {
		t.Fatalf("third SaveConfig: %v", err)
	}
	stored3, _ := m.LoadConfig(WDTT)
	if stored3.SubToken != "my-token" {
		t.Errorf("explicit SubToken = %q, want preserved", stored3.SubToken)
	}
}

// TestSyncPasswordsSkipsDisabledClients: a disabled client must not stay
// usable — its password is removed from passwords.json and re-added once the
// client is enabled again.
func TestSyncPasswordsSkipsDisabledClients(t *testing.T) {
	isolateStoreDiscovery(t)

	dir := t.TempDir()
	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main-pass"
	cfg.ConfigDir = dir
	cfg.Clients = []WDTTClient{
		{Name: "Alice", Password: "alice-pass", Enabled: true},
		{Name: "Bob", Password: "bob-pass", Enabled: false},
	}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dbFile := filepath.Join(dir, "passwords.json")
	raw, _ := os.ReadFile(dbFile)
	var db struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		t.Fatal(err)
	}
	if _, ok := db.Passwords["alice-pass"]; !ok {
		t.Error("enabled client alice-pass must be present")
	}
	if _, ok := db.Passwords["bob-pass"]; ok {
		t.Error("disabled client bob-pass must not be in passwords.json")
	}
	// Re-enabling Bob brings his password back.
	cfg.Clients[1].Enabled = true
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("re-enable SaveConfig: %v", err)
	}
	raw2, _ := os.ReadFile(dbFile)
	var db2 struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw2, &db2); err != nil {
		t.Fatal(err)
	}
	if _, ok := db2.Passwords["bob-pass"]; !ok {
		t.Error("re-enabled client bob-pass must be back in passwords.json")
	}
}

// TestSyncPasswordsPurgesDeviceBindings: deleting a client removes the
// Telegram device bindings that reference its password from passwords.json.
func TestSyncPasswordsPurgesDeviceBindings(t *testing.T) {
	isolateStoreDiscovery(t)

	dir := t.TempDir()
	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main-pass"
	cfg.ConfigDir = dir
	cfg.Clients = []WDTTClient{{Name: "Alice", Password: "alice-pass", Enabled: true}}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dbFile := filepath.Join(dir, "passwords.json")
	raw, _ := os.ReadFile(dbFile)
	var db struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
		Devices   json.RawMessage            `json:"devices"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		t.Fatal(err)
	}
	bumped := struct {
		MainPassword string                     `json:"main_password"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
		Devices      json.RawMessage            `json:"devices"`
	}{
		MainPassword: "main-pass",
		Passwords: map[string]json.RawMessage{
			"alice-pass": db.Passwords["alice-pass"],
			"stale-pass": json.RawMessage(`{"label":"gone","source":"panel"}`),
		},
		Devices: json.RawMessage(`{
			"dev1": {"name": "Alice phone", "password": "alice-pass"},
			"dev2": {"name": "Old client", "password": "stale-pass"},
			"dev3": {"name": "bot only", "pass": "bot-pass"}
		}`),
	}
	merged, _ := json.Marshal(bumped)
	if err := os.WriteFile(dbFile, merged, 0o600); err != nil {
		t.Fatal(err)
	}
	// Delete Alice: her entry, the stale entry and both device bindings must
	// go, while the bot-only binding stays.
	cfg.Clients = nil
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig after delete: %v", err)
	}
	raw2, _ := os.ReadFile(dbFile)
	var db2 struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
		Devices   map[string]map[string]any  `json:"devices"`
	}
	if err := json.Unmarshal(raw2, &db2); err != nil {
		t.Fatal(err)
	}
	if len(db2.Passwords) != 0 {
		t.Errorf("passwords = %v, want empty", db2.Passwords)
	}
	if _, ok := db2.Devices["dev1"]; ok {
		t.Error("device binding for deleted client must be purged")
	}
	if _, ok := db2.Devices["dev2"]; ok {
		t.Error("device binding for stale password must be purged")
	}
	if _, ok := db2.Devices["dev3"]; !ok {
		t.Error("bot-only device binding must survive the panel sync")
	}
}

// TestPurgeDeviceBindingsFlatShape: device bindings stored as plain
// device key → password strings are handled too.
func TestPurgeDeviceBindingsFlatShape(t *testing.T) {
	devices := json.RawMessage(`{"dev1":"alice-pass","dev2":"keep-pass"}`)
	got := purgeDeviceBindings(devices, map[string]bool{"alice-pass": true})
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["dev1"]; ok {
		t.Error("flat binding for removed password must be purged")
	}
	if _, ok := parsed["dev2"]; !ok {
		t.Error("flat binding for surviving password must stay")
	}
	// Opaque shapes pass through untouched.
	opaque := json.RawMessage(`[{"dev":"alice-pass"}]`)
	if string(purgeDeviceBindings(opaque, map[string]bool{"alice-pass": true})) != string(opaque) {
		t.Error("opaque devices shape must be returned unchanged")
	}
}

// TestSyncPasswordsPurgesLedgerEntries: entries the bot created before the
// panel took a client over carry no source marker, so the ledger of passwords
// the panel has managed is what lets a later deletion purge them.
func TestSyncPasswordsPurgesLedgerEntries(t *testing.T) {
	isolateStoreDiscovery(t)

	dir := t.TempDir()
	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main-pass"
	cfg.ConfigDir = dir
	cfg.Clients = []WDTTClient{{Name: "Alice", Password: "alice-pass", Enabled: true}}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dbFile := filepath.Join(dir, "passwords.json")
	// Simulate the bot store: Alice's entry was created by the bot and only
	// later claimed by the panel, so it has no source marker.
	bot := struct {
		MainPassword string                     `json:"main_password"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
	}{
		MainPassword: "main-pass",
		Passwords: map[string]json.RawMessage{
			"alice-pass": json.RawMessage(`{"label":"Alice","vk_hash":"h1"}`),
			"bot-pass":   json.RawMessage(`{"label":"Bot own"}`),
		},
	}
	merged, _ := json.Marshal(bot)
	if err := os.WriteFile(dbFile, merged, 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := m.LoadConfig(WDTT)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.PanelPasswords) != 1 || saved.PanelPasswords[0] != "alice-pass" {
		t.Fatalf("ledger = %v, want [alice-pass]", saved.PanelPasswords)
	}
	cfg.Clients = nil
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig after delete: %v", err)
	}
	raw2, _ := os.ReadFile(dbFile)
	var db2 struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw2, &db2); err != nil {
		t.Fatal(err)
	}
	if _, ok := db2.Passwords["alice-pass"]; ok {
		t.Error("ledgered password of deleted client must be purged")
	}
	if _, ok := db2.Passwords["bot-pass"]; !ok {
		t.Error("bot-owned entry must survive the panel sync")
	}
}

// TestSyncPasswordsDiscoversServerStore: when a passwords.json exists next to
// the running server (e.g. started outside the panel), it must be kept in
// sync too — deleting a client in the panel must take effect there as well.
func TestSyncPasswordsDiscoversServerStore(t *testing.T) {
	isolateStoreDiscovery(t)

	oldDirs := wdttProcessDirs
	t.Cleanup(func() { wdttProcessDirs = oldDirs })

	dir := t.TempDir()
	panelDir := filepath.Join(dir, "panel")
	serverDir := filepath.Join(dir, "server")
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The server's own store (as if written by the bot running outside the
	// panel) holds Alice's password plus a bot-only entry.
	serverDB := filepath.Join(serverDir, "passwords.json")
	boot := struct {
		MainPassword string                     `json:"main_password"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
	}{
		MainPassword: "main",
		Passwords: map[string]json.RawMessage{
			"alice-pass": json.RawMessage(`{"label":"Alice","source":"panel"}`),
			"bot-pass":   json.RawMessage(`{"label":"bot only"}`),
		},
	}
	raw0, _ := json.Marshal(boot)
	if err := os.WriteFile(serverDB, raw0, 0o600); err != nil {
		t.Fatal(err)
	}
	wdttProcessDirs = func() []string { return []string{serverDir} }

	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(WDTT)
	cfg.Password = "main"
	cfg.ConfigDir = panelDir
	cfg.Clients = []WDTTClient{{Name: "Alice", Password: "alice-pass", Enabled: true}}
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	// Deleting Alice must remove her entry from the server's real store.
	cfg.Clients = nil
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("second SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(serverDB)
	if err != nil {
		t.Fatalf("server store not updated: %v", err)
	}
	var db struct {
		Passwords map[string]json.RawMessage `json:"passwords"`
	}
	if err := json.Unmarshal(raw, &db); err != nil {
		t.Fatal(err)
	}
	if _, ok := db.Passwords["alice-pass"]; ok {
		t.Error("alice-pass must be removed from the discovered server store")
	}
	if _, ok := db.Passwords["bot-pass"]; !ok {
		t.Error("bot-only entry must survive in the discovered store")
	}
}
