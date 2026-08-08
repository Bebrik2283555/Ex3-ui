// Package extra manages sidecar proxy cores that run next to the Xray core:
// qwdtt (WireGuard-over-TURN tunnel) and olcRTC (TCP-over-WebRTC tunnel).
// Each core is an external binary; the package stores its config in the panel
// settings table, supervises its process and auto-restarts it on crash.
package extra

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Name identifies a managed extra core.
type Name string

const (
	// WDTT is the qwdtt tunnel server binary.
	WDTT Name = "qwdtt"
	// OLCRTC is the olcRTC tunnel server binary.
	OLCRTC Name = "olcrtc"
)

// All returns the supported service names in display order.
func All() []Name {
	return []Name{WDTT, OLCRTC}
}

// DisplayName returns the human-readable name of the service.
func (n Name) DisplayName() string {
	switch n {
	case WDTT:
		return "qWDTT"
	case OLCRTC:
		return "olcRTC"
	default:
		return string(n)
	}
}

// Valid reports whether n is one of the supported service names.
func (n Name) Valid() bool {
	return n == WDTT || n == OLCRTC
}

// Config holds the runtime configuration of one extra core.
type Config struct {
	Enabled    bool   `json:"enabled"`
	AutoStart  bool   `json:"autoStart"`
	BinaryPath string `json:"binaryPath"`

	// qwdtt
	ListenAddr string `json:"listenAddr"`
	WGPort     int    `json:"wgPort"`
	Password   string `json:"password"`
	DNS        string `json:"dns"`
	ConfigDir  string `json:"configDir"`
	// qwdtt raw-IP mode. Empty means the server does not listen on it.
	ListenRaw string `json:"listenRaw"`

	// qwdtt subscription (public HTTPS JSON, distributed to clients)
	SubToken string `json:"subToken"`
	SubHost  string `json:"subHost"`
	VkHashes string `json:"vkHashes"`

	// olcrtc
	ConfigFile string `json:"configFile"`
	DataDir    string `json:"dataDir"`
	Provider   string `json:"provider"`   // auth.provider: jitsi | telemost | wbstream
	RoomID     string `json:"roomId"`     // room.id: full room URL for the provider
	CryptoKey  string `json:"cryptoKey"`  // crypto.key: 64 hex chars (32 bytes), shared with the client
	Transport  string `json:"transport"`  // net.transport: datachannel | vp8channel
	OlcrtcDNS  string `json:"olcrtcDns"`  // net.dns: resolver in host:port form
	VP8Fps     int    `json:"vp8Fps"`     // vp8.fps: frames per second (1..120)
	VP8Batch   int    `json:"vp8Batch"`   // vp8.batch_size: frames per batch (1..64)
	Debug      bool   `json:"debug"`      // verbose logging

	// Generic
	ExtraArgs string `json:"extraArgs"`
}

// DefaultConfig returns sensible defaults for the given core.
func DefaultConfig(name Name) Config {
	switch name {
	case WDTT:
		return Config{
			ListenAddr: "0.0.0.0:56000",
			WGPort:     56001,
			DNS:        "8.8.8.8",
			ConfigDir:  "/etc/wdtt",
			ListenRaw:  "0.0.0.0:56003",
		}
	case OLCRTC:
		return Config{
			ConfigFile: "/etc/olcrtc/server.yaml",
			DataDir:    "/etc/olcrtc/data",
			Provider:   "jitsi",
			Transport:  "datachannel",
			OlcrtcDNS:  "8.8.8.8:53",
			VP8Fps:     60,
			VP8Batch:   64,
		}
	default:
		return Config{}
	}
}

// Merge fills any zero fields of c from the defaults so partial updates from
// the UI never wipe a field that the client did not send.
func (c Config) Merge(name Name) Config {
	def := DefaultConfig(name)
	if c.ListenAddr == "" {
		c.ListenAddr = def.ListenAddr
	}
	if c.WGPort == 0 {
		c.WGPort = def.WGPort
	}
	if c.DNS == "" {
		c.DNS = def.DNS
	}
	if c.ConfigDir == "" {
		c.ConfigDir = def.ConfigDir
	}
	if c.ListenRaw == "" {
		c.ListenRaw = def.ListenRaw
	}
	if c.ConfigFile == "" {
		c.ConfigFile = def.ConfigFile
	}
	if c.DataDir == "" {
		c.DataDir = def.DataDir
	}
	if c.Provider == "" {
		c.Provider = def.Provider
	}
	if c.Transport == "" {
		c.Transport = def.Transport
	}
	if c.OlcrtcDNS == "" {
		c.OlcrtcDNS = def.OlcrtcDNS
	}
	if c.VP8Fps == 0 {
		c.VP8Fps = def.VP8Fps
	}
	if c.VP8Batch == 0 {
		c.VP8Batch = def.VP8Batch
	}
	return c
}

// BuildArgs converts the config into the argv passed to the binary.
func (c Config) BuildArgs(name Name) []string {
	switch name {
	case WDTT:
		args := []string{
			"-listen", c.ListenAddr,
			"-wg-port", fmt.Sprintf("%d", c.WGPort),
			"-config-dir", c.ConfigDir,
		}
		if c.Password != "" {
			args = append(args, "-password", c.Password)
		}
		if c.DNS != "" {
			args = append(args, "-dns", c.DNS)
		}
		if c.ListenRaw != "" {
			args = append(args, "-listen-raw", c.ListenRaw)
		}
		if c.ExtraArgs != "" {
			args = append(args, strings.Fields(c.ExtraArgs)...)
		}
		return args
	case OLCRTC:
		// olcrtc accepts exactly one argument: the path to the YAML config
		// (upstream has no CLI flags; extra args would fail startup).
		if c.ConfigFile == "" {
			return nil
		}
		return []string{c.ConfigFile}
	default:
		return nil
	}
}

// BinaryName returns the binary filename for the current platform.
func (n Name) BinaryName() string {
	name := "extra-" + string(n)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// ServerYAML renders the olcRTC server config the way the upstream binary
// expects it (single YAML file, sole CLI argument). Schema mirrors
// docs/examples/server/*.yaml of the upstream repository.
func (c Config) ServerYAML() string {
	var b strings.Builder
	b.WriteString("mode: srv\n")
	b.WriteString("auth:\n")
	b.WriteString("  provider: " + yamlString(c.Provider) + "\n")
	b.WriteString("room:\n")
	b.WriteString("  id: " + yamlString(c.RoomID) + "\n")
	if c.CryptoKey != "" {
		b.WriteString("crypto:\n")
		b.WriteString("  key: " + yamlString(c.CryptoKey) + "\n")
	}
	b.WriteString("net:\n")
	b.WriteString("  transport: " + yamlString(c.Transport) + "\n")
	if c.OlcrtcDNS != "" {
		b.WriteString("  dns: " + yamlString(c.OlcrtcDNS) + "\n")
	}
	if c.Transport == "vp8channel" {
		// vp8channel refuses to start without vp8.fps / vp8.batch_size.
		b.WriteString("vp8:\n")
		b.WriteString("  fps: " + strconv.Itoa(c.VP8Fps) + "\n")
		b.WriteString("  batch_size: " + strconv.Itoa(c.VP8Batch) + "\n")
	}
	b.WriteString("liveness:\n")
	b.WriteString("  interval: 10s\n")
	b.WriteString("  timeout: 5s\n")
	b.WriteString("  failures: 3\n")
	if c.DataDir != "" {
		b.WriteString("data: " + yamlString(c.DataDir) + "\n")
	}
	b.WriteString("debug: " + strconv.FormatBool(c.Debug) + "\n")
	return b.String()
}

// OlcrtcURI renders the olcrtc:// connect URI the olcbox clients import
// (grammar mirrored from the client's ConfigShareService.olcRtcUri).
func (c Config) OlcrtcURI() string {
	if strings.TrimSpace(c.RoomID) == "" || strings.TrimSpace(c.CryptoKey) == "" {
		return ""
	}
	transport := strings.TrimSpace(c.Transport)
	switch transport {
	case "vp8channel":
		transport = fmt.Sprintf("vp8channel<vp8-fps=%d&vp8-batch=%d>", c.VP8Fps, c.VP8Batch)
	case "":
		transport = "datachannel"
	}
	return fmt.Sprintf("olcrtc://%s?%s@%s#%s",
		strings.TrimSpace(c.Provider), transport, strings.TrimSpace(c.RoomID), strings.TrimSpace(c.CryptoKey))
}

// yamlString double-quotes a scalar and escapes embedded quotes/newlines so a
// user-supplied value cannot break out of its string token.
func yamlString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return "\"" + s + "\""
}

// validCryptoKey reports whether s is a 64-char hex string (32 bytes).
func validCryptoKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// randomCryptoKey returns 32 random bytes as a 64-char hex string.
func randomCryptoKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// publicPort extracts the listen port from a listen address like
// "0.0.0.0:56000", falling back to 56000.
func publicPort(listenAddr string) int {
	if i := strings.LastIndex(listenAddr, ":"); i >= 0 {
		if p, err := strconv.Atoi(listenAddr[i+1:]); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return 56000
}

// defaultIPURLs are public IPv4 echo services used to discover the server's
// own address (same set as install.sh's IP auto-detection).
var defaultIPURLs = []string{
	"https://api4.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://v4.api.ipinfo.io/ip",
	"https://ipv4.myexternalip.com/raw",
	"https://4.ident.me",
	"https://check-host.net/ip",
}

// detectPublicIPFrom asks each URL in turn for the server's public IPv4
// address. The first plain-IP response wins; "" means none answered.
func detectPublicIPFrom(client *http.Client, urls []string) string {
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if isIPv4(ip) {
			return ip
		}
	}
	return ""
}

// isIPv4 reports whether s is a dotted-quad IPv4 address.
func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// publicIPDetector returns the server's public IPv4 address, or "" when the
// echo services are unreachable. Package-level so tests can stub the network.
var publicIPDetector = func() string {
	return detectPublicIPFrom(&http.Client{Timeout: 5 * time.Second}, defaultIPURLs)
}

// DefaultBinaryPath returns where the core binary is expected on this platform.
func (n Name) DefaultBinaryPath() string {
	return filepath.Join(config.GetBinFolderPath(), n.BinaryName())
}

func (n Name) settingKey() string {
	return "extraService_" + string(n)
}

// SettingsStore is the minimal key/value persistence surface the manager needs.
type SettingsStore interface {
	GetString(key string) (string, error)
	SetString(key, value string) error
}

// Manager supervises the extra core processes.
type Manager struct {
	store SettingsStore

	mu    sync.Mutex
	procs map[Name]*Proc
}

// NewManager returns a manager backed by the given settings store.
func NewManager(store SettingsStore) *Manager {
	return &Manager{store: store, procs: make(map[Name]*Proc)}
}

var (
	instanceMu sync.Mutex
	instance   *Manager
)

// Instance returns the shared manager, creating it with the given store the
// first time it is called. Backend and cron job reference the same manager so
// both observe the same process table.
func Instance(store SettingsStore) *Manager {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instance == nil {
		instance = NewManager(store)
	}
	return instance
}

// ManagerSingleton returns the shared manager, or nil before Instance is called.
func ManagerSingleton() *Manager {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	return instance
}

// settingKey returns the settings key for a config blob.
func (m *Manager) settingKey(n Name) string { return n.settingKey() }

// LoadConfig reads and decodes the stored config for the given core.
func (m *Manager) LoadConfig(n Name) (Config, error) {
	cfg := DefaultConfig(n)
	raw, err := m.store.GetString(m.settingKey(n))
	if err != nil {
		// No stored row yet — treat as fresh defaults, not an error.
		if strings.Contains(err.Error(), "not in defaultValueMap") || strings.Contains(err.Error(), "record not found") {
			return cfg, nil
		}
		return cfg, nil
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		logger.Warningf("extra: corrupt %s config: %v", n, err)
		return DefaultConfig(n), nil
	}
	return cfg.Merge(n), nil
}

// SaveConfig persists the config for the given core and applies it to the
// running process (restarting it when its settings changed).
func (m *Manager) SaveConfig(n Name, cfg Config) error {
	cfg = cfg.Merge(n)
	if n == OLCRTC {
		if strings.TrimSpace(cfg.RoomID) == "" {
			return fmt.Errorf("%s: room id is required", n.DisplayName())
		}
		switch cfg.Provider {
		case "jitsi", "telemost", "wbstream":
		default:
			return fmt.Errorf("%s: provider %q is not supported by the olcrtc binary (jitsi | telemost | wbstream)", n.DisplayName(), cfg.Provider)
		}
		// The olcbox client (Android) exposes only these two transports; the
		// Telemost provider accepts only VP8 there.
		switch cfg.Transport {
		case "datachannel", "vp8channel":
		default:
			return fmt.Errorf("%s: transport %q is not supported by the olcbox client (datachannel | vp8channel)", n.DisplayName(), cfg.Transport)
		}
		if cfg.Provider == "telemost" && cfg.Transport != "vp8channel" {
			return fmt.Errorf("%s: Telemost supports only the vp8channel transport", n.DisplayName())
		}
		if cfg.Transport == "vp8channel" {
			// Match the client's sanitization (LocationConfig.sanitizeVp8*).
			cfg.VP8Fps = clampInt(cfg.VP8Fps, 1, 120)
			cfg.VP8Batch = clampInt(cfg.VP8Batch, 1, 64)
		}
		// The resolver dials this string directly (net.Dialer), so a port is
		// mandatory (host:port): a bare IP would fail with "missing port".
		if strings.TrimSpace(cfg.OlcrtcDNS) == "" {
			return fmt.Errorf("%s: dns must be set (host:port)", n.DisplayName())
		}
		if _, _, err := net.SplitHostPort(cfg.OlcrtcDNS); err != nil {
			return fmt.Errorf("%s: dns must be in host:port form (e.g. 8.8.8.8:53): %w", n.DisplayName(), err)
		}
		if strings.TrimSpace(cfg.CryptoKey) == "" {
			key, err := randomCryptoKey()
			if err != nil {
				return fmt.Errorf("%s: generate crypto key: %w", n.DisplayName(), err)
			}
			cfg.CryptoKey = key
		} else if !validCryptoKey(strings.TrimSpace(cfg.CryptoKey)) {
			return fmt.Errorf("%s: crypto key must be 64 hex chars (openssl rand -hex 32)", n.DisplayName())
		}
	}
	if n == WDTT && strings.TrimSpace(cfg.SubHost) == "" {
		if ip := strings.TrimSpace(publicIPDetector()); ip != "" {
			cfg.SubHost = ip + ":" + strconv.Itoa(publicPort(cfg.ListenAddr))
		}
	}
	old, _ := m.LoadConfig(n)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := m.store.SetString(m.settingKey(n), string(raw)); err != nil {
		return err
	}
	if n == OLCRTC {
		if err := m.WriteYAML(n, cfg); err != nil {
			return err
		}
	}
	if old.Enabled && cfg.Enabled {
		if old != cfg {
			return m.Restart(n)
		}
	}
	if cfg.Enabled && cfg.AutoStart {
		bin := cfg.BinaryPath
		if bin == "" {
			bin = n.DefaultBinaryPath()
		}
		if !fileExists(bin) {
			return fmt.Errorf("binary %q does not exist", bin)
		}
	}
	return nil
}

// WriteYAML renders the olcRTC server config and writes it to the configured
// config file (0600 — it contains the shared tunnel key). No-op for qwdtt.
func (m *Manager) WriteYAML(n Name, cfg Config) error {
	if n != OLCRTC || cfg.ConfigFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o755); err != nil {
		return fmt.Errorf("%s: create config dir: %w", n.DisplayName(), err)
	}
	if err := os.WriteFile(cfg.ConfigFile, []byte(cfg.ServerYAML()), 0o600); err != nil {
		return fmt.Errorf("%s: write config: %w", n.DisplayName(), err)
	}
	return nil
}

// BinaryPathExists reports whether the configured binary is present.
func (c Config) BinaryPathExists() bool {
	if c.BinaryPath == "" {
		return false
	}
	info, err := os.Stat(c.BinaryPath)
	return err == nil && !info.IsDir()
}

// GetProc returns (creating if needed) the supervised process for a core.
func (m *Manager) GetProc(n Name) *Proc {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.procs[n]; ok {
		return p
	}
	p := NewProc(n)
	m.procs[n] = p
	return p
}

// Start launches the service if it is not already running.
func (m *Manager) Start(n Name) error {
	cfg, err := m.LoadConfig(n)
	if err != nil {
		return err
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin = n.DefaultBinaryPath()
	}
	if !fileExists(bin) {
		return fmt.Errorf("%s binary not found at %s", n.DisplayName(), bin)
	}
	if runtime.GOOS != "windows" {
		// Copies made over scp/zip/Windows never carry the exec bit; without
		// it exec fails with EACCES. Fix it here so upload/install order
		// does not matter.
		if err := os.Chmod(bin, 0o755); err != nil {
			return fmt.Errorf("%s: make %s executable: %w", n.DisplayName(), bin, err)
		}
	}
	args := cfg.BuildArgs(n)
	if len(args) == 0 {
		return fmt.Errorf("%s: no launch arguments (config file missing?)", n.DisplayName())
	}
	p := m.GetProc(n)
	if err := p.Start(bin, args); err != nil {
		return fmt.Errorf("%s: %w", n.DisplayName(), err)
	}
	return nil
}

// Stop terminates the service process if running.
func (m *Manager) Stop(n Name) error {
	p := m.GetProc(n)
	return p.Stop()
}

// Restart stops and starts the service.
func (m *Manager) Restart(n Name) error {
	_ = m.Stop(n)
	return m.Start(n)
}

// Status describes the current runtime state of one core.
type Status struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"displayName"`
	Enabled      bool     `json:"enabled"`
	AutoStart    bool     `json:"autoStart"`
	Running      bool     `json:"running"`
	BinaryExists bool     `json:"binaryExists"`
	BinaryPath   string   `json:"binaryPath"`
	LastLog      string   `json:"lastLog"`
	Logs         []string `json:"logs"`
	Config       Config   `json:"config"`
	ConnectURI   string   `json:"connectUri"`
}

// StatusOf returns the status snapshot of the given core.
func (m *Manager) StatusOf(n Name) Status {
	cfg, _ := m.LoadConfig(n)
	p := m.GetProc(n)
	bin := cfg.BinaryPath
	if bin == "" {
		bin = n.DefaultBinaryPath()
	}
	st := Status{
		Name:         string(n),
		DisplayName:  n.DisplayName(),
		Enabled:      cfg.Enabled,
		AutoStart:    cfg.AutoStart,
		Running:      p.IsRunning(),
		BinaryExists: fileExists(bin),
		BinaryPath:   bin,
		LastLog:      p.LastLine(),
		Logs:         p.Lines(200),
		Config:       cfg,
		ConnectURI:   cfg.OlcrtcURI(),
	}
	return st
}

// AllStatuses returns status snapshots for every supported core.
func (m *Manager) AllStatuses() []Status {
	out := make([]Status, 0, len(All()))
	for _, n := range All() {
		out = append(out, m.StatusOf(n))
	}
	return out
}

// Reconcile starts every enabled core whose binary exists and that is not
// already running. Called periodically and after a process crash.
func (m *Manager) Reconcile() {
	for _, n := range All() {
		cfg, err := m.LoadConfig(n)
		if err != nil {
			continue
		}
		if !cfg.Enabled || !cfg.AutoStart {
			continue
		}
		bin := cfg.BinaryPath
		if bin == "" {
			bin = n.DefaultBinaryPath()
		}
		if !fileExists(bin) {
			continue
		}
		p := m.GetProc(n)
		if p.IsRunning() {
			continue
		}
		if err := m.Start(n); err != nil {
			logger.Warningf("extra: failed to start %s: %v", n.DisplayName(), err)
		}
	}
}

// Logs returns the most recent output lines of the managed process.
func (m *Manager) Logs(n Name, lines int) []string {
	p := m.GetProc(n)
	return p.Lines(lines)
}

// SubProfile is one qwdtt profile in a subscription document.
type SubProfile struct {
	Name     string `json:"name"`
	Peer     string `json:"peer"`
	Hashes   string `json:"hashes"`
	Workers  int    `json:"workers"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// Subscription is the public JSON document the qwdtt Android client imports.
// Shape follows https://github.com/SpaceNeuroX/proxy-turn-vk-android (subscriptions).
type Subscription struct {
	SubscriptionName string       `json:"subscriptionName"`
	Description      string       `json:"description,omitempty"`
	Version          int          `json:"version"`
	UpdatedAt        string       `json:"updatedAt"`
	Profiles         []SubProfile `json:"profiles"`
}

// SubscriptionFor builds the qwdtt subscription document from the stored config.
// It returns an error when the core is not qwdtt or the document cannot be built
// (missing token, host or password).
func (m *Manager) SubscriptionFor(n Name) (Subscription, error) {
	if n != WDTT {
		return Subscription{}, fmt.Errorf("subscriptions are only supported for %s", WDTT.DisplayName())
	}
	cfg, err := m.LoadConfig(n)
	if err != nil {
		return Subscription{}, err
	}
	if cfg.SubToken == "" {
		return Subscription{}, fmt.Errorf("no subscription token configured for %s", n.DisplayName())
	}
	host := strings.TrimSpace(cfg.SubHost)
	if host == "" {
		// Config was saved before the IP could be detected (offline save) —
		// retry now so the client link keeps working.
		if ip := strings.TrimSpace(publicIPDetector()); ip != "" {
			host = ip + ":" + strconv.Itoa(publicPort(cfg.ListenAddr))
		}
	}
	if host == "" {
		return Subscription{}, fmt.Errorf("no public host configured for %s", n.DisplayName())
	}
	password := strings.TrimSpace(cfg.Password)
	if password == "" {
		return Subscription{}, fmt.Errorf("no password configured for %s", n.DisplayName())
	}
	// Port is the client's local TUN port (on the phone), not a server port.
	profiles := []SubProfile{{
		Name:     n.DisplayName(),
		Peer:     host,
		Hashes:   strings.TrimSpace(cfg.VkHashes),
		Workers:  16,
		Port:     9000,
		Password: password,
	}}
	return Subscription{
		SubscriptionName: n.DisplayName(),
		Description:      "qwdtt tunnel via " + host,
		Version:          1,
		UpdatedAt:        time.Now().Format("2006-01-02"),
		Profiles:         profiles,
	}, nil
}

// SubscriptionToken matches the configured secret token against a caller.
func (m *Manager) SubscriptionToken(n Name) string {
	cfg, _ := m.LoadConfig(n)
	return strings.TrimSpace(cfg.SubToken)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
