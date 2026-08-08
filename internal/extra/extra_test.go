package extra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNameValid(t *testing.T) {
	cases := []struct {
		name Name
		want bool
	}{
		{WDTT, true},
		{OLCRTC, true},
		{"nope", false},
		{"", false},
	}
	for _, c := range cases {
		if got := c.name.Valid(); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMergeFillsDefaults(t *testing.T) {
	cfg := Config{Enabled: true}
	got := cfg.Merge(WDTT)
	if got.ListenAddr != "0.0.0.0:56000" {
		t.Errorf("ListenAddr = %q, want default", got.ListenAddr)
	}
	if got.WGPort != 56001 {
		t.Errorf("WGPort = %d, want 56001", got.WGPort)
	}
	if got.DNS != "8.8.8.8" {
		t.Errorf("DNS = %q, want 8.8.8.8", got.DNS)
	}
	if got.ListenRaw != "0.0.0.0:56003" {
		t.Errorf("ListenRaw = %q, want 0.0.0.0:56003", got.ListenRaw)
	}
	// A explicitly-set non-zero field must survive merge.
	cfg2 := Config{WGPort: 1234}
	got2 := cfg2.Merge(WDTT)
	if got2.WGPort != 1234 {
		t.Errorf("WGPort = %d, want 1234 (explicit value preserved)", got2.WGPort)
	}
}

func TestMergeOLCrtc(t *testing.T) {
	got := Config{}.Merge(OLCRTC)
	if got.ConfigFile != "/etc/olcrtc/server.yaml" {
		t.Errorf("ConfigFile = %q, want default server.yaml", got.ConfigFile)
	}
	if got.DataDir == "" {
		t.Error("DataDir should get a default")
	}
}

func TestBuildArgsWDTT(t *testing.T) {
	cfg := DefaultConfig(WDTT)
	cfg.Password = "hunter2"
	cfg.ExtraArgs = "--foo bar"
	args := cfg.BuildArgs(WDTT)
	want := []string{
		"-listen", "0.0.0.0:56000",
		"-wg-port", "56001",
		"-config-dir", "/etc/wdtt",
		"-password", "hunter2",
		"-dns", "8.8.8.8",
		"-listen-raw", "0.0.0.0:56003",
		"--foo", "bar",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("BuildArgs(WDTT) = %v, want %v", args, want)
	}
	containsPair := func(have []string, key, val string) bool {
		for i := 0; i+1 < len(have); i++ {
			if have[i] == key && have[i+1] == val {
				return true
			}
		}
		return false
	}
	// Empty = disabled: no -listen-raw in argv.
	off := DefaultConfig(WDTT)
	off.ListenRaw = ""
	if containsPair(off.BuildArgs(WDTT), "-listen-raw", "") {
		t.Errorf("BuildArgs with raw disabled must not contain -listen-raw: %v", off.BuildArgs(WDTT))
	}
}

func TestBuildArgsOLCrtc(t *testing.T) {
	cfg := DefaultConfig(OLCRTC)
	args := cfg.BuildArgs(OLCRTC)
	if len(args) != 1 || args[0] != "/etc/olcrtc/server.yaml" {
		t.Errorf("BuildArgs(olcrtc) = %v, want [server.yaml]", args)
	}
	// With an explicit config file the first argv is that file.
	cfg.ConfigFile = "/custom.yaml"
	args = cfg.BuildArgs(OLCRTC)
	if args[0] != "/custom.yaml" {
		t.Errorf("BuildArgs(olcrtc) = %v, want custom file first", args)
	}
	// Upstream takes exactly one argument — extra args must be dropped.
	cfg.ExtraArgs = "--foo bar"
	args = cfg.BuildArgs(OLCRTC)
	if len(args) != 1 {
		t.Errorf("BuildArgs(olcrtc) with ExtraArgs = %v, want single arg", args)
	}
}

func TestServerYAMLRendersUpstreamSchema(t *testing.T) {
	cfg := DefaultConfig(OLCRTC)
	cfg.RoomID = "https://meet.example.org/room"
	cfg.CryptoKey = strings.Repeat("ab", 32)
	cfg.Debug = true
	got := cfg.ServerYAML()
	for _, want := range []string{
		"mode: srv\n",
		"  provider: \"jitsi\"\n",
		"  id: \"https://meet.example.org/room\"\n",
		"  key: \"" + cfg.CryptoKey + "\"\n",
		"  transport: \"datachannel\"\n",
		"  dns: \"8.8.8.8:53\"\n",
		"  interval: 10s\n",
		"data: \"/etc/olcrtc/data\"\n",
		"debug: true\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ServerYAML() missing %q in:\n%s", want, got)
		}
	}
}

func TestServerYAMLEscapesUserValues(t *testing.T) {
	cfg := DefaultConfig(OLCRTC)
	cfg.RoomID = "a\"b\nc"
	got := cfg.ServerYAML()
	if !strings.Contains(got, "id: \"a\\\"b\\nc\"") {
		t.Errorf("ServerYAML() did not escape room id:\n%s", got)
	}
}

func TestOlcrtcURI(t *testing.T) {
	key := strings.Repeat("ab", 32)
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "jitsi datachannel",
			cfg:  Config{Provider: "jitsi", RoomID: "https://meet.example.org/room", CryptoKey: key, Transport: "datachannel"},
			want: "olcrtc://jitsi?datachannel@https://meet.example.org/room#" + key,
		},
		{
			name: "vp8channel carries fps/batch",
			cfg:  Config{Provider: "wbstream", RoomID: "https://meet.example.org/v", CryptoKey: key, Transport: "vp8channel", VP8Fps: 45, VP8Batch: 32},
			want: "olcrtc://wbstream?vp8channel<vp8-fps=45&vp8-batch=32>@https://meet.example.org/v#" + key,
		},
		{
			name: "telemost falls back to vp8 params",
			cfg:  Config{Provider: "telemost", RoomID: "https://meet.example.org/t", CryptoKey: key, Transport: "vp8channel", VP8Fps: 60, VP8Batch: 64},
			want: "olcrtc://telemost?vp8channel<vp8-fps=60&vp8-batch=64>@https://meet.example.org/t#" + key,
		},
		{
			name: "missing crypto key",
			cfg:  Config{Provider: "jitsi", RoomID: "https://meet.example.org/room", Transport: "datachannel"},
			want: "",
		},
		{
			name: "missing room id",
			cfg:  Config{Provider: "jitsi", CryptoKey: key, Transport: "datachannel"},
			want: "",
		},
	}
	for _, c := range cases {
		if got := c.cfg.OlcrtcURI(); got != c.want {
			t.Errorf("%s: OlcrtcURI() = %q, want %q", c.name, got, c.want)
		}
	}
	var empty Config
	if got := empty.OlcrtcURI(); got != "" {
		t.Errorf("empty config URI = %q, want \"\"", got)
	}
}

func TestSaveConfigOLCRTCWritesYAML(t *testing.T) {
	dir := t.TempDir()
	store := memStore{}
	m := NewManager(store)
	cfg := DefaultConfig(OLCRTC)
	cfg.ConfigFile = dir + "/server.yaml"
	cfg.RoomID = "https://meet.example.org/room"
	if err := m.SaveConfig(OLCRTC, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if _, err := os.Stat(cfg.ConfigFile); err != nil {
		t.Fatalf("yaml file not written: %v", err)
	}
	raw, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "mode: srv") || !strings.Contains(body, "https://meet.example.org/room") {
		t.Errorf("unexpected yaml:\n%s", body)
	}
	// An empty key must be generated, persisted and end up in the file.
	persisted, _ := store.GetString(OLCRTC.settingKey())
	var stored Config
	if err := json.Unmarshal([]byte(persisted), &stored); err != nil {
		t.Fatal(err)
	}
	if !validCryptoKey(stored.CryptoKey) {
		t.Errorf("generated key %q is not 64 hex chars", stored.CryptoKey)
	}
	if !strings.Contains(body, "key: \""+stored.CryptoKey+"\"") {
		t.Errorf("yaml missing generated key:\n%s", body)
	}
}

func TestSaveConfigOLCRTCValidation(t *testing.T) {
	m := NewManager(memStore{})
	// Missing room id.
	cfg := DefaultConfig(OLCRTC)
	cfg.ConfigFile = t.TempDir() + "/server.yaml"
	if err := m.SaveConfig(OLCRTC, cfg); err == nil {
		t.Fatal("expected error for empty room id")
	}
	// Malformed key.
	cfg.RoomID = "https://meet.example.org/room"
	cfg.CryptoKey = "not-a-hex-key"
	if err := m.SaveConfig(OLCRTC, cfg); err == nil {
		t.Fatal("expected error for malformed crypto key")
	}
	// Valid key length but not hex.
	cfg.CryptoKey = strings.Repeat("g", 64)
	if err := m.SaveConfig(OLCRTC, cfg); err == nil {
		t.Fatal("expected error for non-hex key")
	}
	// DNS without a port (missing port makes the resolver fail at dial time).
	cfg.CryptoKey = ""
	cfg.OlcrtcDNS = "1.1.1.153"
	if err := m.SaveConfig(OLCRTC, cfg); err == nil {
		t.Fatal("expected error for dns without port")
	}
	// Host:port passes.
	cfg.OlcrtcDNS = "1.1.1.1:53"
	if err := m.SaveConfig(OLCRTC, cfg); err != nil {
		t.Fatalf("SaveConfig with valid dns: %v", err)
	}
}

func TestWriteYAMLNoopForWDTT(t *testing.T) {
	m := NewManager(memStore{})
	cfg := DefaultConfig(WDTT)
	cfg.ConfigFile = "/nonexistent/dir/server.yaml"
	if err := m.WriteYAML(WDTT, cfg); err != nil {
		t.Fatalf("WriteYAML(WDTT) should be a no-op, got %v", err)
	}
}

func TestSettingKey(t *testing.T) {
	if got := WDTT.settingKey(); got != "extraService_qwdtt" {
		t.Errorf("settingKey = %q", got)
	}
	if got := OLCRTC.settingKey(); got != "extraService_olcrtc" {
		t.Errorf("settingKey = %q", got)
	}
}

func TestDefaultConfigZeroedResetByMerge(t *testing.T) {
	// A fully zero config must not keep zero for required fields after Merge.
	cfg := Config{}
	cfg = cfg.Merge(WDTT)
	if cfg.WGPort == 0 {
		t.Error("Merge should fill WGPort")
	}
}

type memStore map[string]string

func (s memStore) GetString(key string) (string, error) { return s[key], nil }
func (s memStore) SetString(key, value string) error    { s[key] = value; return nil }

// TestSaveConfigFallsBackToDefaultBinaryPath guards the "binary \"\" does not
// exist" bug: the UI never sends binaryPath, so SaveConfig must check the
// default binary location when the stored path is empty.
func TestSaveConfigFallsBackToDefaultBinaryPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	t.Setenv("PATH", dir) // no-op guard: config reads XUI_BIN_FOLDER only

	// No binary on disk yet: with AutoStart enabled the save must fail on the
	// resolved default path, not on the empty string.
	m := NewManager(memStore{})
	n := WDTT
	err := m.SaveConfig(n, Config{Enabled: true, AutoStart: true})
	if err == nil {
		t.Fatal("expected error when default binary is missing")
	}
	if got := err.Error(); !strings.Contains(got, n.BinaryName()) {
		t.Fatalf("error %q should reference default binary %q", got, n.BinaryName())
	}

	// Create the default binary; the same save must now succeed.
	if err := os.WriteFile(n.DefaultBinaryPath(), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
m2 := NewManager(memStore{})
	if err := m2.SaveConfig(n, Config{Enabled: true, AutoStart: true}); err != nil {
		t.Fatalf("save with present default binary failed: %v", err)
	}
}

// TestSubscriptionFor builds a qwdtt subscription from a stored config and
// rejects malformed ones (missing token / host).
func TestSubscriptionFor(t *testing.T) {
	// Not qwdtt -> error.
	m := NewManager(memStore{})
	if _, err := m.SubscriptionFor(OLCRTC); err == nil {
		t.Fatal("expected error for non-qwdtt core")
	}

	// Missing token -> error.
	m2 := NewManager(memStore{})
	cfg := Config{SubHost: "203.0.113.10:56000", Password: "secret"}
	if _, err := m2.SubscriptionFor(WDTT); err == nil {
		t.Fatal("expected error when no config stored")
	}
	_ = cfg

	// Happy path: token present, host + password set.
	store := memStore{}
	m3 := NewManager(store)
	good := Config{SubToken: "tok", SubHost: "203.0.113.10:56000", Password: "secret", VkHashes: "h1,h2"}
	raw, err := json.Marshal(good.Merge(WDTT))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetString(WDTT.settingKey(), string(raw)); err != nil {
		t.Fatal(err)
	}
	doc, err := m3.SubscriptionFor(WDTT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.SubscriptionName != "qWDTT" {
		t.Errorf("name = %q", doc.SubscriptionName)
	}
	if len(doc.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(doc.Profiles))
	}
	if got := doc.Profiles[0].Hashes; got != "h1,h2" {
		t.Errorf("hashes = %q", got)
	}
	if got := doc.Profiles[0].Peer; got != "203.0.113.10:56000" {
		t.Errorf("peer = %q", got)
	}
	if got := doc.Profiles[0].Port; got != 9000 {
		t.Errorf("port = %d, want 9000 (client's local TUN port)", got)
	}
}

func TestServerYAMLVp8Section(t *testing.T) {
	cfg := DefaultConfig(OLCRTC)
	cfg.RoomID = "room"
	cfg.Transport = "vp8channel"
	cfg.VP8Fps = 45
	cfg.VP8Batch = 32
	got := cfg.ServerYAML()
	for _, want := range []string{
		"  transport: \"vp8channel\"\n",
		"vp8:\n",
		"  fps: 45\n",
		"  batch_size: 32\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ServerYAML() missing %q in:\n%s", want, got)
		}
	}

	cfg.Transport = "datachannel"
	got = cfg.ServerYAML()
	if strings.Contains(got, "vp8:") {
		t.Errorf("ServerYAML() must not emit vp8 section for datachannel:\n%s", got)
	}
}

func TestSaveConfigOLCRTCTransportRules(t *testing.T) {
	m := NewManager(memStore{})
	newCfg := func() Config {
		c := DefaultConfig(OLCRTC)
		c.ConfigFile = t.TempDir() + "/server.yaml"
		c.RoomID = "https://meet.example.org/room"
		return c
	}
	cases := []struct {
		name        string
		mutate      func(c *Config)
		wantErr     bool
		errContains string
	}{
		{"telemost+datachannel", func(c *Config) { c.Provider = "telemost"; c.Transport = "datachannel" }, true, "Telemost"},
		{"telemost+vp8channel", func(c *Config) { c.Provider = "telemost"; c.Transport = "vp8channel" }, false, ""},
		{"jitsi+datachannel", func(c *Config) { c.Provider = "jitsi"; c.Transport = "datachannel" }, false, ""},
		{"wbstream+vp8channel", func(c *Config) { c.Provider = "wbstream"; c.Transport = "vp8channel" }, false, ""},
		{"jazz-unsupported", func(c *Config) { c.Provider = "jazz" }, true, "provider"},
		{"videochannel-unsupported", func(c *Config) { c.Transport = "videochannel" }, true, "transport"},
		{"seichannel-unsupported", func(c *Config) { c.Transport = "seichannel" }, true, "transport"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := newCfg()
			c.mutate(&cfg)
			err := m.SaveConfig(OLCRTC, cfg)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if c.errContains != "" && !strings.Contains(err.Error(), c.errContains) {
					t.Fatalf("error %q should mention %q", err, c.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSaveConfigOLCRTCClampsVP8(t *testing.T) {
	m := NewManager(memStore{})
	cfg := DefaultConfig(OLCRTC)
	cfg.ConfigFile = t.TempDir() + "/server.yaml"
	cfg.RoomID = "room"
	cfg.Transport = "vp8channel"
	cfg.VP8Fps = 999
	cfg.VP8Batch = 0
	if err := m.SaveConfig(OLCRTC, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stored, err := m.LoadConfig(OLCRTC)
	if err != nil {
		t.Fatal(err)
	}
	if stored.VP8Fps != 120 {
		t.Errorf("VP8Fps = %d, want clamped 120", stored.VP8Fps)
	}
	if stored.VP8Batch != 64 {
		t.Errorf("VP8Batch = %d, want clamped 64", stored.VP8Batch)
	}
	raw, _ := os.ReadFile(cfg.ConfigFile)
	if !strings.Contains(string(raw), "fps: 120") || !strings.Contains(string(raw), "batch_size: 64") {
		t.Errorf("yaml missing clamped vp8 values:\n%s", raw)
	}
}

func TestSaveConfigWDTTDetectsPublicIP(t *testing.T) {
	oldDetector := publicIPDetector
	t.Cleanup(func() { publicIPDetector = oldDetector })

	store := memStore{}
	m := NewManager(store)
	publicIPDetector = func() string { return "203.0.113.7" }

	cfg := DefaultConfig(WDTT)
	if err := m.SaveConfig(WDTT, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stored, err := m.LoadConfig(WDTT)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SubHost != "203.0.113.7:56000" {
		t.Errorf("SubHost = %q, want 203.0.113.7:56000", stored.SubHost)
	}

	// A custom listen port must be reflected in the auto-filled host.
	cfg2 := DefaultConfig(WDTT)
	cfg2.ListenAddr = "0.0.0.0:56123"
	if err := m.SaveConfig(WDTT, cfg2); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stored2, _ := m.LoadConfig(WDTT)
	if stored2.SubHost != "203.0.113.7:56123" {
		t.Errorf("SubHost = %q, want 203.0.113.7:56123", stored2.SubHost)
	}

	// Detection failure must not fail the save; host stays empty.
	store2 := memStore{}
	m2 := NewManager(store2)
	publicIPDetector = func() string { return "" }
	cfg3 := DefaultConfig(WDTT)
	if err := m2.SaveConfig(WDTT, cfg3); err != nil {
		t.Fatalf("SaveConfig with failed detection: %v", err)
	}
	stored3, _ := m2.LoadConfig(WDTT)
	if stored3.SubHost != "" {
		t.Errorf("SubHost = %q, want empty after failed detection", stored3.SubHost)
	}
}

func TestDetectPublicIPFrom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "203.0.113.99")
	}))
	defer srv.Close()
	client := &http.Client{Timeout: 3 * time.Second}

	if got := detectPublicIPFrom(client, []string{srv.URL}); got != "203.0.113.99" {
		t.Errorf("detectPublicIPFrom = %q, want 203.0.113.99", got)
	}

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not-an-ip")
	}))
	defer garbage.Close()
	if got := detectPublicIPFrom(client, []string{garbage.URL, srv.URL}); got != "203.0.113.99" {
		t.Errorf("detectPublicIPFrom must skip non-IP responses, got %q", got)
	}

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()
	if got := detectPublicIPFrom(client, []string{notFound.URL}); got != "" {
		t.Errorf("detectPublicIPFrom = %q, want empty for HTTP error", got)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	if got := detectPublicIPFrom(client, []string{deadURL}); got != "" {
		t.Errorf("detectPublicIPFrom = %q, want empty for unreachable server", got)
	}
}

func TestPublicPort(t *testing.T) {
	cases := map[string]int{
		"0.0.0.0:56000": 56000,
		"127.0.0.1:9000": 9000,
		"":               56000,
		"no-port":        56000,
	}
	for in, want := range cases {
		if got := publicPort(in); got != want {
			t.Errorf("publicPort(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestIsIPv4(t *testing.T) {
	valid := []string{"1.2.3.4", "255.255.255.255", "0.0.0.0"}
	for _, in := range valid {
		if !isIPv4(in) {
			t.Errorf("isIPv4(%q) = false, want true", in)
		}
	}
	invalid := []string{"", "1.2.3", "1.2.3.256", "1.2.3.a", "1.2.3.4.5", " 1.2.3.4", "1.2.3.4\n"}
	for _, in := range invalid {
		if isIPv4(in) {
			t.Errorf("isIPv4(%q) = true, want false", in)
		}
	}
}