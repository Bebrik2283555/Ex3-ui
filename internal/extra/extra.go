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
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// WDTTClient is one qwdtt client. Each client has its own subscription with
// its own name and description. The wdtt-server binary stores passwords in
// {ConfigDir}/passwords.json and re-reads it on SIGHUP.
type WDTTClient struct {
	Name                     string `json:"name"`
	SubscriptionName         string `json:"subscriptionName"`
	SubscriptionDescription  string `json:"subscriptionDescription"`
	Password                 string `json:"password"`
	VkHashes                 string `json:"vkHashes"`
	Enabled                  bool   `json:"enabled"`
	SubURI                   string `json:"subUri"`
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

	// qwdtt telegram bot (invites, usage reports); replaces the legacy
	// -admin/-bot-token ExtraArgs flags
	AdminID  string `json:"adminId"`
	BotToken string `json:"botToken"`

	// qwdtt per-client store (passthrough config field, synced to the server)
	Clients []WDTTClient `json:"clients,omitempty"`
	// qwdtt passwords the panel has ever managed, so a deleted client can be
	// purged even from stores whose entries lack the panel source marker.
	PanelPasswords []string `json:"panelPasswords,omitempty"`

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
		if c.AdminID != "" {
			args = append(args, "-admin", c.AdminID)
		}
		if c.BotToken != "" {
			args = append(args, "-bot-token", c.BotToken)
		}
		if c.DNS != "" {
			args = append(args, "-dns", c.DNS)
		}
		if c.ListenRaw != "" {
			args = append(args, "-listen-raw", c.ListenRaw)
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

// validateWDTTClients rejects client lists the wdtt-server store cannot
// represent: empty names/passwords, a client reusing the server password or
// two clients sharing one password.
func validateWDTTClients(cfg Config) error {
	seen := make(map[string]struct{}, len(cfg.Clients))
	for _, cl := range cfg.Clients {
		if strings.TrimSpace(cl.Name) == "" {
			return fmt.Errorf("%s: client name is required", WDTT.DisplayName())
		}
		pass := strings.TrimSpace(cl.Password)
		if pass == "" {
			return fmt.Errorf("%s: client %q needs a password", WDTT.DisplayName(), cl.Name)
		}
		if pass == strings.TrimSpace(cfg.Password) {
			return fmt.Errorf("%s: client %q cannot reuse the server password", WDTT.DisplayName(), cl.Name)
		}
		if _, dup := seen[pass]; dup {
			return fmt.Errorf("%s: duplicate client password (all clients share one port, passwords must differ)", WDTT.DisplayName())
		}
		seen[pass] = struct{}{}
	}
	return nil
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
	if n == WDTT {
		// Generate per-client subscription URIs for new clients.
		for i := range cfg.Clients {
			if strings.TrimSpace(cfg.Clients[i].SubURI) == "" {
				cfg.Clients[i].SubURI = randomSubURI()
			}
		}
		// The subscription endpoint is token-gated; a missing token would make
		// every subscription URL a dead link, so generate one on first save.
		if strings.TrimSpace(cfg.SubToken) == "" {
			cfg.SubToken = randomAlnum(16)
		}
		// Lift the legacy -admin/-bot-token flags out of ExtraArgs into the
		// explicit fields (older deployments stored them there). Unknown or
		// malformed flags are dropped: they would fail startup anyway.
		if (cfg.AdminID == "" || cfg.BotToken == "") && strings.TrimSpace(cfg.ExtraArgs) != "" {
			f := strings.Fields(cfg.ExtraArgs)
			for i := 0; i+1 < len(f); i += 2 {
				switch f[i] {
				case "-admin":
					if cfg.AdminID == "" {
						cfg.AdminID = strings.TrimSpace(f[i+1])
					}
				case "-bot-token":
					if cfg.BotToken == "" {
						cfg.BotToken = strings.TrimSpace(f[i+1])
					}
				}
			}
		}
		cfg.ExtraArgs = ""
		if err := validateWDTTClients(cfg); err != nil {
			return err
		}
		// Remember every password the panel has managed so a deleted client
		// can be purged even where its entry lacks the panel source marker.
		// Seed from both the incoming config and the stored one: clients that
		// never resend the field (older UI, other API consumers) must not
		// drop the ledger.
		prev, _ := m.LoadConfig(n)
		known := make(map[string]bool, len(cfg.PanelPasswords)+len(prev.PanelPasswords))
		for _, p := range append(append([]string{}, prev.PanelPasswords...), cfg.PanelPasswords...) {
			known[strings.TrimSpace(p)] = true
		}
		for _, cl := range cfg.Clients {
			if pass := strings.TrimSpace(cl.Password); pass != "" {
				known[pass] = true
			}
		}
		cfg.PanelPasswords = make([]string, 0, len(known))
		for p := range known {
			cfg.PanelPasswords = append(cfg.PanelPasswords, p)
		}
		sort.Strings(cfg.PanelPasswords)
	}
	old, _ := m.LoadConfig(n)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := m.store.SetString(m.settingKey(n), string(raw)); err != nil {
		return err
	}
	if n == WDTT {
		// Push the panel's clients into the server's password store and make
		// the running process re-read it. A sync failure must not break the
		// panel-side save.
		if err := m.SyncPasswords(n, cfg); err != nil {
			logger.Warningf("extra: %s password sync failed: %v", n.DisplayName(), err)
		}
	}
	if n == OLCRTC {
		if err := m.WriteYAML(n, cfg); err != nil {
			return err
		}
	}
	if old.Enabled && cfg.Enabled {
		// The password ledger and sub-token are bookkeeping: they must not
		// restart the core.
		old.PanelPasswords = nil
		cfg.PanelPasswords = nil
		old.SubToken = ""
		cfg.SubToken = ""
		if !reflect.DeepEqual(old, cfg) {
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
// Each client has its own subscription with its own name and description.
type Subscription struct {
	SubscriptionName string       `json:"subscriptionName"`
	Description      string       `json:"description,omitempty"`
	Profiles         []SubProfile `json:"profiles"`
}

// ClientSubscription builds the qwdtt subscription document for a specific client.
func (m *Manager) ClientSubscription(n Name, clientURI string) (Subscription, error) {
	if n != WDTT {
		return Subscription{}, fmt.Errorf("subscriptions are only supported for %s", WDTT.DisplayName())
	}
	cfg, err := m.LoadConfig(n)
	if err != nil {
		return Subscription{}, err
	}
	var client *WDTTClient
	for i := range cfg.Clients {
		if cfg.Clients[i].SubURI == clientURI {
			client = &cfg.Clients[i]
			break
		}
	}
	if client == nil {
		return Subscription{}, fmt.Errorf("client not found")
	}
	if !client.Enabled {
		return Subscription{}, fmt.Errorf("client is disabled")
	}
	host := strings.TrimSpace(cfg.SubHost)
	if host == "" {
		if ip := strings.TrimSpace(publicIPDetector()); ip != "" {
			host = ip + ":" + strconv.Itoa(publicPort(cfg.ListenAddr))
		}
	}
	if host == "" {
		return Subscription{}, fmt.Errorf("no public host configured for %s", n.DisplayName())
	}
	password := strings.TrimSpace(client.Password)
	if password == "" {
		return Subscription{}, fmt.Errorf("client has no password")
	}
	profile := SubProfile{
		Name:     strings.TrimSpace(client.Name),
		Peer:     host,
		Hashes:   strings.TrimSpace(client.VkHashes),
		Workers:  16,
		Port:     9000,
		Password: password,
	}
	subName := strings.TrimSpace(client.SubscriptionName)
	if subName == "" {
		subName = strings.TrimSpace(client.Name)
	}
	return Subscription{
		SubscriptionName: subName,
		Description:      strings.TrimSpace(client.SubscriptionDescription),
		Profiles:         []SubProfile{profile},
	}, nil
}

// WDTTLink renders the qwdtt://config?name=...&peer=...&hashes=...&workers=...&port=...&pass=... quick link
// for one client. Returns "" when the client has no password or no reachable
// host can be derived (wildcard listen addresses fall back to the public IP).
func (c Config) WDTTLink(cl WDTTClient) string {
	if strings.TrimSpace(cl.Password) == "" {
		return ""
	}
	host := strings.TrimSpace(c.SubHost)
	if host == "" {
		host = strings.TrimSpace(c.ListenAddr)
	}
	ip := host
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[:i], ":") {
		ip = host[:i]
	}
	if ip == "0.0.0.0" || ip == "::" || ip == "" {
		if detected := strings.TrimSpace(publicIPDetector()); detected != "" {
			ip = detected
		} else {
			return ""
		}
	}
	name := strings.TrimSpace(cl.Name)
	if name == "" {
		name = "Client"
	}
	port := publicPort(c.ListenAddr)
	if port == 0 {
		port = 56000
	}
	return fmt.Sprintf("qwdtt://config?name=%s&peer=%s:%d&hashes=%s&workers=16&port=9000&pass=%s",
		name, ip, port, strings.TrimSpace(cl.VkHashes), strings.TrimSpace(cl.Password))
}

// wdttPassEntry is the storage shape of one password in the server's
// passwords.json. The map-based merge below keeps unknown fields (device
// bindings created by the Telegram bot) intact.
type wdttPassEntry map[string]any

// SyncPasswords writes the panel's qwdtt clients into the server's password
// stores and signals every running server process to re-read them. The store
// location is discovered at runtime (existing copies plus /proc of running
// processes), because the server may read its passwords.json from a directory
// other than the panel's configured one — e.g. when it was started outside
// the panel. Bot-created entries are merged and survive as long as they are
// not panel-owned; entries of clients deleted or disabled in the panel are
// removed together with their Telegram device bindings.
func (m *Manager) SyncPasswords(n Name, cfg Config) error {
	if n != WDTT || strings.TrimSpace(cfg.ConfigDir) == "" {
		return nil
	}
	written := 0
	for _, dbFile := range wdttPasswordDBs(cfg) {
		if err := syncPasswordsFile(dbFile, cfg); err != nil {
			logger.Warningf("extra: qwdtt password sync to %s failed: %v", dbFile, err)
			continue
		}
		written++
	}
	if written == 0 {
		return fmt.Errorf("no writable qwdtt password store")
	}
	if runtime.GOOS != "windows" {
		// Reload every running server that owns one of the updated stores,
		// whether or not the panel launched it. The managed process also gets
		// its signal below; the duplicate is harmless.
		for _, pid := range wdttProcesses() {
			signalHUP(pid)
		}
	}
	p := m.GetProc(n)
	if p.IsRunning() && runtime.GOOS != "windows" {
		if err := p.Signal(syscall.SIGHUP); err != nil {
			return err
		}
	}
	return nil
}

// syncPasswordsFile merges the panel's enabled clients into one password
// store file and writes it back atomically.
func syncPasswordsFile(dbFile string, cfg Config) error {
	db := struct {
		MainPassword string                     `json:"main_password"`
		AdminID      json.RawMessage            `json:"admin_id,omitempty"`
		BotToken     json.RawMessage            `json:"bot_token,omitempty"`
		Passwords    map[string]json.RawMessage `json:"passwords"`
		Devices      json.RawMessage            `json:"devices,omitempty"`
	}{}
	if data, err := os.ReadFile(dbFile); err == nil {
		_ = json.Unmarshal(data, &db)
	}
	if db.Passwords == nil {
		db.Passwords = make(map[string]json.RawMessage)
	}
	db.MainPassword = strings.TrimSpace(cfg.Password)
	panelPasswords := make(map[string]bool)
	for _, cl := range cfg.Clients {
		if !cl.Enabled {
			// Disabled clients must not stay usable: the wdtt-server accepts
			// any password present in the file.
			continue
		}
		pass := strings.TrimSpace(cl.Password)
		if pass == "" {
			continue
		}
		panelPasswords[pass] = true
		entry := wdttPassEntry{}
		if raw, ok := db.Passwords[pass]; ok {
			_ = json.Unmarshal(raw, &entry)
		}
		entry["label"] = strings.TrimSpace(cl.Name)
		if hashes := strings.TrimSpace(cl.VkHashes); hashes != "" {
			entry["vk_hash"] = hashes
		} else {
			delete(entry, "vk_hash")
		}
		entry["source"] = "panel"
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		db.Passwords[pass] = raw
	}
	// Remove panel-sourced entries no longer in the enabled client list
	// (deleted or disabled clients) and their Telegram device bindings. The
	// password ledger covers entries the bot created before the panel took
	// the client over: their entries carry no source marker.
	panelKnown := make(map[string]bool, len(cfg.PanelPasswords))
	for _, p := range cfg.PanelPasswords {
		panelKnown[strings.TrimSpace(p)] = true
	}
	removed := make(map[string]bool)
	for pass, raw := range db.Passwords {
		if panelPasswords[pass] {
			continue
		}
		var entry wdttPassEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if src, _ := entry["source"].(string); src == "panel" || panelKnown[pass] {
			delete(db.Passwords, pass)
			removed[pass] = true
		}
	}
	db.Devices = purgeDeviceBindings(db.Devices, removed)
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dbFile, data, 0o600)
}

// wdttCommonDirs are the plausible locations of the wdtt-server data dir
// besides the panel's configured one.
var wdttCommonDirs = []string{
	"/etc/wdtt",
	"/usr/local/etc/wdtt",
	"/var/lib/wdtt",
	"/var/lib/qwdtt",
	"/opt/wdtt",
}

// wdttPasswordDBs returns the passwords.json paths to keep in sync: every
// existing copy found in the configured dir, the configured binary's dir, the
// common locations, the user's home and the running processes' dirs. When no
// copy exists yet, only the configured dir is returned so a fresh install
// behaves exactly as before.
func wdttPasswordDBs(cfg Config) []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	add(filepath.Join(strings.TrimSpace(cfg.ConfigDir), "passwords.json"))
	if bin := strings.TrimSpace(cfg.BinaryPath); bin != "" {
		add(filepath.Join(filepath.Dir(bin), "passwords.json"))
	}
	for _, d := range wdttCommonDirs {
		add(filepath.Join(d, "passwords.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{home, filepath.Join(home, ".wdtt"), filepath.Join(home, ".config", "wdtt")} {
			add(filepath.Join(d, "passwords.json"))
		}
	}
	for _, d := range wdttProcessDirs() {
		add(filepath.Join(d, "passwords.json"))
	}
	var existing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return paths[:1]
	}
	return existing
}

// wdttProcessDirs scans /proc (Linux) for running wdtt server processes and
// returns the directories that may hold their passwords.json: the working
// dir, the executable's dir and every open handle named passwords.json.
// Never matches on non-Linux. Package-level so tests can stub the tree.
var wdttProcessDirs = func() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	var out []string
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(cmdline) == 0 {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		base := strings.ToLower(filepath.Base(argv[0]))
		if !strings.Contains(base, "wdtt") && !strings.Contains(base, "qwdtt") {
			continue
		}
		addDir := func(d string) {
			if d = strings.TrimSpace(d); d != "" {
				out = append(out, d)
			}
		}
		if cwd, err := os.Readlink("/proc/" + e.Name() + "/cwd"); err == nil {
			addDir(cwd)
		}
		if exe, err := os.Readlink("/proc/" + e.Name() + "/exe"); err == nil {
			addDir(filepath.Dir(exe))
		}
		for i := 0; i+1 < len(argv); i++ {
			flag := strings.ToLower(argv[i])
			if strings.HasPrefix(flag, "-config") && !strings.HasPrefix(argv[i+1], "-") {
				addDir(argv[i+1])
			}
		}
		fds, err := os.ReadDir("/proc/" + e.Name() + "/fd")
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if target, err := os.Readlink("/proc/" + e.Name() + "/fd/" + fd.Name()); err == nil && filepath.Base(target) == "passwords.json" {
				addDir(filepath.Dir(target))
			}
		}
	}
	return out
}

// wdttProcesses returns the PIDs of running wdtt server processes (Linux
// only), used to deliver SIGHUP regardless of how they were launched.
func wdttProcesses() []int {
	if runtime.GOOS != "linux" {
		return nil
	}
	var pids []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(cmdline) == 0 {
			continue
		}
		base := strings.ToLower(filepath.Base(strings.SplitN(string(cmdline), "\x00", 2)[0]))
		if strings.Contains(base, "wdtt") || strings.Contains(base, "qwdtt") {
			pids = append(pids, pid)
		}
	}
	return pids
}

// purgeDeviceBindings drops Telegram device entries that reference any of the
// removed passwords, so clients deleted or disabled in the panel also vanish
// from the bot's device list. Best-effort: an opaque devices shape is returned
// unchanged. The bot stores bindings as an object of device key → entry, where
// the entry names the bound password either in "password" or "pass", or is the
// password itself.
func purgeDeviceBindings(devices json.RawMessage, removed map[string]bool) json.RawMessage {
	if len(removed) == 0 || len(devices) == 0 || string(devices) == "null" {
		return devices
	}
	var byKey map[string]map[string]any
	if err := json.Unmarshal(devices, &byKey); err != nil {
		var flat map[string]string
		if err := json.Unmarshal(devices, &flat); err != nil {
			return devices
		}
		changed := false
		for key, pass := range flat {
			if removed[pass] {
				delete(flat, key)
				changed = true
			}
		}
		if !changed {
			return devices
		}
		raw, err := json.Marshal(flat)
		if err != nil {
			return devices
		}
		return raw
	}
	changed := false
	for key, entry := range byKey {
		if entry == nil {
			continue
		}
		pass, _ := entry["password"].(string)
		if pass == "" {
			pass, _ = entry["pass"].(string)
		}
		if removed[pass] {
			delete(byKey, key)
			changed = true
		}
	}
	if !changed {
		return devices
	}
	raw, err := json.Marshal(byKey)
	if err != nil {
		return devices
	}
	return raw
}

// SubscriptionToken matches the configured secret token against a caller.
func (m *Manager) SubscriptionToken(n Name) string {
	cfg, _ := m.LoadConfig(n)
	return strings.TrimSpace(cfg.SubToken)
}

// randomAlnum returns n random lowercase alphanumeric characters.
func randomAlnum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		r := make([]byte, 1)
		_, _ = rand.Read(r)
		b[i] = chars[int(r[0])%len(chars)]
	}
	return string(b)
}

// randomSubURI returns a 7-character random alphanumeric string for
// per-client subscription URIs.
func randomSubURI() string {
	return randomAlnum(7)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
