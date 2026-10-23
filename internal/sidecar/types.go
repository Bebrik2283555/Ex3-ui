// Package sidecar supervises the external cores that back the qWDTT, olcRTC
// and CSQTT inbound protocols. Each inbound owns one process and one config
// directory, so deleting an inbound stops exactly its own sidecar.
package sidecar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Default ports used when an inbound is created without explicit values.
const (
	DefaultQWDTTWGPort     = 56001
	DefaultQWDTTRawPort    = 56003
	DefaultQWDTTListenAddr = "0.0.0.0:56000"
	DefaultCSQTTListenAddr = "0.0.0.0:46000"
	DefaultCSQTTWebPort    = 46002
	DefaultOLCRTCDataDir   = "data"
)

// Supported reports whether p is served by this package.
func Supported(p model.Protocol) bool { return model.IsSidecarProtocol(p) }

// DisplayName returns the human-readable protocol name.
func DisplayName(p model.Protocol) string {
	switch p {
	case model.QWDTT:
		return "qWDTT"
	case model.OLCRTC:
		return "olcRTC"
	case model.CSQTT:
		return "CSQTT"
	default:
		return string(p)
	}
}

// QWDTTConfig mirrors the qwdtt-server CLI flags the panel controls.
type QWDTTConfig struct {
	ListenAddr string `json:"listenAddr,omitempty"`
	WGPort     int    `json:"wgPort"`
	RawPort    int    `json:"rawPort"`
	ListenRaw  string `json:"listenRaw,omitempty"`
	ListenDir  string `json:"listenDirect,omitempty"`
	Hashes     string `json:"hashes,omitempty"`
	Workers    int    `json:"workers,omitempty"`
	ClientPort int    `json:"clientPort,omitempty"`
	ConfigDir  string `json:"configDir,omitempty"`
	MainPass   string `json:"mainPassword,omitempty"`
	DNS        string `json:"dns,omitempty"`
	PublicHost string `json:"publicHost,omitempty"`
	SubHost    string `json:"subHost,omitempty"`
	// HashesByEmail overrides Hashes per client (VK call hashes are issued
	// per device, so one global value is not enough).
	HashesByEmail map[string]string `json:"hashesByEmail,omitempty"`

	RouteThroughXray bool   `json:"routeThroughXray,omitempty"`
	OutboundTag      string `json:"outboundTag,omitempty"`
	RouteXrayPort    int    `json:"routeXrayPort,omitempty"`
}

// OLCRTCConfig mirrors the olcrtc server YAML the panel renders.
type OLCRTCConfig struct {
	Provider   string `json:"provider,omitempty"`
	RoomID     string `json:"roomId,omitempty"`
	CryptoKey  string `json:"cryptoKey,omitempty"`
	Transport  string `json:"transport,omitempty"`
	VP8Fps     int    `json:"vp8Fps,omitempty"`
	VP8Batch   int    `json:"vp8Batch,omitempty"`
	SeiFps     int    `json:"seiFps,omitempty"`
	SeiBatch   int    `json:"seiBatch,omitempty"`
	SeiFrag    int    `json:"seiFrag,omitempty"`
	SeiAck     int    `json:"seiAck,omitempty"`
	VideoW     int    `json:"videoW,omitempty"`
	VideoH     int    `json:"videoH,omitempty"`
	VideoFps   int    `json:"videoFps,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	DNS        string `json:"dns,omitempty"`
	DataDir    string `json:"dataDir,omitempty"`
	ConfigFile string `json:"configFile,omitempty"`
	PublicHost string `json:"publicHost,omitempty"`
	SubHost    string `json:"subHost,omitempty"`
	Debug      bool   `json:"debug,omitempty"`

	RouteThroughXray bool   `json:"routeThroughXray,omitempty"`
	OutboundTag      string `json:"outboundTag,omitempty"`
	RouteXrayPort    int    `json:"routeXrayPort,omitempty"`
}

// CSQTTConfig mirrors the csqtt server CLI flags the panel controls.
type CSQTTConfig struct {
	ListenAddr    string            `json:"listenAddr,omitempty"`
	WebPort       int               `json:"webPort"`
	WebUser       string            `json:"webUser,omitempty"`
	WebPass       string            `json:"webPass,omitempty"`
	PublicIP      string            `json:"publicIp,omitempty"`
	DNS           string            `json:"dns,omitempty"`
	ConfigDir     string            `json:"configDir,omitempty"`
	MainPass      string            `json:"mainPassword,omitempty"`
	SecureCookie  bool              `json:"secureCookie,omitempty"`
	WebSecretPath string            `json:"webSecretPath,omitempty"`
	Hashes        string            `json:"hashes,omitempty"`
	HashesByEmail map[string]string `json:"hashesByEmail,omitempty"`
	SubHost       string            `json:"subHost,omitempty"`

	RouteThroughXray bool   `json:"routeThroughXray,omitempty"`
	OutboundTag      string `json:"outboundTag,omitempty"`
	RouteXrayPort    int    `json:"routeXrayPort,omitempty"`
}

// Instance is one resolved sidecar inbound, ready to be supervised.
type Instance struct {
	InboundID int
	Tag       string
	Remark    string
	Protocol  model.Protocol
	Listen    string
	Port      int
	Clients   []model.Client

	QWDTT  *QWDTTConfig
	OLCRTC *OLCRTCConfig
	CSQTT  *CSQTTConfig

	// RouteThroughXray marks a sidecar whose client subnet is steered into an
	// Xray TUN (qWDTT/CSQTT) or a loopback SOCKS bridge (olcRTC).
	RouteThroughXray bool
	TunName          string
	RouteTable       int
	RouteIfaces      []string
	OutboundTag      string
	RouteXrayPort    int

	Fingerprint string
}

// Settings returns the protocol-specific settings block.
func (i Instance) Settings() any {
	switch i.Protocol {
	case model.QWDTT:
		return i.QWDTT
	case model.OLCRTC:
		return i.OLCRTC
	case model.CSQTT:
		return i.CSQTT
	default:
		return nil
	}
}

// DataDir returns the panel-owned directory for this inbound's runtime files.
func DataDir(inboundID int, protocol model.Protocol) string {
	return filepath.Join(config.GetDBFolderPath(), "sidecar", fmt.Sprintf("%s-%d", protocol, inboundID))
}

// BinaryPath resolves the sidecar binary for the current architecture. The
// release archive ships both an unsuffixed copy and an architecture-suffixed
// one; the plain name wins when it exists.
func BinaryPath(p model.Protocol) string {
	bin := config.GetBinFolderPath()
	names := binaryNames(p)
	for _, name := range names {
		path := filepath.Join(bin, name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	if len(names) == 0 {
		return ""
	}
	return filepath.Join(bin, names[0])
}

// BinaryCandidates lists every file name the sidecar binary may use on this
// platform, most preferred first.
func BinaryCandidates(p model.Protocol) []string {
	return binaryNames(p)
}

func binaryNames(p model.Protocol) []string {
	arch := runtime.GOARCH
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	switch p {
	case model.QWDTT:
		return []string{"extra-qwdtt" + exe, "extra-qwdtt-" + arch + exe}
	case model.OLCRTC:
		return []string{"extra-olcrtc" + exe, "extra-olcrtc-" + arch + exe}
	case model.CSQTT:
		return []string{"csqtt-linux-" + arch, "csqtt" + exe, "csqtt-linux-amd64"}
	default:
		return nil
	}
}

// InstanceFromInbound parses and validates the inbound's settings.
func InstanceFromInbound(ib *model.Inbound) (Instance, error) {
	if ib == nil || !Supported(ib.Protocol) {
		return Instance{}, fmt.Errorf("not a sidecar inbound")
	}
	var raw struct {
		Clients []model.Client  `json:"clients"`
		Sidecar json.RawMessage `json:"sidecar"`
	}
	if strings.TrimSpace(ib.Settings) != "" {
		if err := json.Unmarshal([]byte(ib.Settings), &raw); err != nil {
			return Instance{}, fmt.Errorf("%s: invalid settings: %w", DisplayName(ib.Protocol), err)
		}
	}
	clients := raw.Clients
	if attached, ok := attachedClients(ib.Id); ok {
		clients = attached
	}
	inst := Instance{
		InboundID: ib.Id,
		Tag:       ib.Tag,
		Remark:    ib.Remark,
		Protocol:  ib.Protocol,
		Listen:    defaultListen(ib.Listen),
		Port:      ib.Port,
		Clients:   clients,
	}
	base := DataDir(ib.Id, ib.Protocol)
	switch ib.Protocol {
	case model.QWDTT:
		cfg := QWDTTConfig{
			WGPort:    DefaultQWDTTWGPort,
			RawPort:   DefaultQWDTTRawPort,
			Workers:   16,
			ConfigDir: filepath.Join(base, "config"),
		}
		if len(raw.Sidecar) > 0 {
			if err := json.Unmarshal(raw.Sidecar, &cfg); err != nil {
				return Instance{}, fmt.Errorf("qWDTT: invalid sidecar settings: %w", err)
			}
		}
		if cfg.ConfigDir == "" {
			cfg.ConfigDir = filepath.Join(base, "config")
		}
		if err := validateConfigDir(cfg.ConfigDir); err != nil {
			return Instance{}, fmt.Errorf("qWDTT: %w", err)
		}
		cfg.DNS = normalizeDNS(cfg.DNS)
		if cfg.Workers <= 0 {
			cfg.Workers = 16
		}
		if cfg.ClientPort <= 0 {
			cfg.ClientPort = 9000
		}
		applyListenAddr(cfg.ListenAddr, &inst)
		if strings.TrimSpace(cfg.ListenRaw) == "" && cfg.RawPort > 0 {
			cfg.ListenRaw = net.JoinHostPort(inst.Listen, strconv.Itoa(cfg.RawPort))
		} else if port := portOf(cfg.ListenRaw); port > 0 {
			cfg.RawPort = port
		}
		inst.QWDTT = &cfg
		inst.RouteThroughXray = cfg.RouteThroughXray
		inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
		inst.TunName = TunName(ib.Id)
		inst.RouteTable = qwdttRouteTable(ib.Id)
		inst.RouteIfaces = []string{"wdtt0"}
		if cfg.RawPort > 0 {
			inst.RouteIfaces = append(inst.RouteIfaces, "wdttraw0")
		}
	case model.OLCRTC:
		cfg := OLCRTCConfig{
			Provider:   "jitsi",
			Transport:  "datachannel",
			VP8Fps:     60,
			VP8Batch:   64,
			SeiFps:     30,
			SeiBatch:   64,
			SeiFrag:    900,
			SeiAck:     2000,
			VideoW:     1080,
			VideoH:     1080,
			VideoFps:   30,
			VideoCodec: "qrcode",
			DNS:        "8.8.8.8:53",
			DataDir:    filepath.Join(base, DefaultOLCRTCDataDir),
			ConfigFile: filepath.Join(base, "server.yaml"),
		}
		if len(raw.Sidecar) > 0 {
			if err := json.Unmarshal(raw.Sidecar, &cfg); err != nil {
				return Instance{}, fmt.Errorf("olcRTC: invalid sidecar settings: %w", err)
			}
		}
		if cfg.ConfigFile == "" {
			cfg.ConfigFile = filepath.Join(base, "server.yaml")
		}
		if cfg.DataDir == "" {
			cfg.DataDir = filepath.Join(base, DefaultOLCRTCDataDir)
		}
		cfg = mergeOLCRTCDefaults(cfg)
		inst.OLCRTC = &cfg
		inst.RouteThroughXray = cfg.RouteThroughXray
		inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
		inst.RouteXrayPort = cfg.RouteXrayPort
	case model.CSQTT:
		cfg := CSQTTConfig{
			WebPort:   DefaultCSQTTWebPort,
			ConfigDir: filepath.Join(base, "config"),
		}
		if len(raw.Sidecar) > 0 {
			if err := json.Unmarshal(raw.Sidecar, &cfg); err != nil {
				return Instance{}, fmt.Errorf("CSQTT: invalid sidecar settings: %w", err)
			}
		}
		if cfg.ConfigDir == "" {
			cfg.ConfigDir = filepath.Join(base, "config")
		}
		if err := validateConfigDir(cfg.ConfigDir); err != nil {
			return Instance{}, fmt.Errorf("CSQTT: %w", err)
		}
		cfg.DNS = normalizeCSQTTDNS(cfg.DNS)
		applyListenAddr(cfg.ListenAddr, &inst)
		inst.CSQTT = &cfg
		inst.RouteThroughXray = cfg.RouteThroughXray
		inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
		inst.TunName = TunName(ib.Id)
		inst.RouteTable = csqttRouteTable(ib.Id)
		inst.RouteIfaces = []string{"csqtt1"}
	}
	inst.Fingerprint = fingerprint(inst)
	return inst, nil
}

// Normalize fills missing secrets, ports and paths in the inbound settings and
// returns the rewritten JSON. Called before the inbound is persisted.
func Normalize(ib *model.Inbound) (string, error) {
	if ib == nil || !Supported(ib.Protocol) {
		return "", fmt.Errorf("not a sidecar inbound")
	}
	var root map[string]json.RawMessage
	if strings.TrimSpace(ib.Settings) == "" {
		root = map[string]json.RawMessage{}
	} else if err := json.Unmarshal([]byte(ib.Settings), &root); err != nil {
		return "", fmt.Errorf("%s: invalid settings: %w", DisplayName(ib.Protocol), err)
	}
	var side map[string]any
	if raw, ok := root["sidecar"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &side)
	}
	if side == nil {
		side = map[string]any{}
	}
	switch ib.Protocol {
	case model.QWDTT:
		setDefault(side, "wgPort", DefaultQWDTTWGPort)
		setDefault(side, "rawPort", DefaultQWDTTRawPort)
		setDefault(side, "listenRaw", "0.0.0.0:56003")
		setDefault(side, "workers", 16)
		setDefault(side, "clientPort", 9000)
		setDefault(side, "mainPassword", randomPassword())
		setDefault(side, "dns", "1.1.1.1")
		if v, _ := side["dns"].(string); strings.TrimSpace(v) != "" {
			side["dns"] = normalizeDNS(v)
		}
		setDefault(side, "routeThroughXray", true)
		if err := normalizeListenAddr(side, ib, DefaultQWDTTListenAddr); err != nil {
			return "", err
		}
		if raw, _ := side["listenRaw"].(string); strings.TrimSpace(raw) != "" {
			if port := portOf(raw); port > 0 {
				side["rawPort"] = port
			}
		}
	case model.OLCRTC:
		setDefault(side, "provider", "jitsi")
		setDefault(side, "transport", "datachannel")
		setDefault(side, "vp8Fps", 60)
		setDefault(side, "vp8Batch", 64)
		setDefault(side, "seiFps", 30)
		setDefault(side, "seiBatch", 64)
		setDefault(side, "seiFrag", 900)
		setDefault(side, "seiAck", 2000)
		setDefault(side, "videoW", 1080)
		setDefault(side, "videoH", 1080)
		setDefault(side, "videoFps", 30)
		setDefault(side, "videoCodec", "qrcode")
		setDefault(side, "dns", "8.8.8.8:53")
		setDefault(side, "routeThroughXray", true)
		if s, _ := side["cryptoKey"].(string); strings.TrimSpace(s) == "" {
			key, err := randomHex(32)
			if err != nil {
				return "", err
			}
			side["cryptoKey"] = key
		}
		// olcRTC reaches Xray through a loopback SOCKS bridge, so it needs a
		// stable port persisted with the inbound.
		if routed, _ := side["routeThroughXray"].(bool); routed {
			if port, _ := side["routeXrayPort"].(float64); port <= 0 {
				if allocated, err := freeLocalPort(); err == nil {
					side["routeXrayPort"] = allocated
				}
			}
		}
	case model.CSQTT:
		setDefault(side, "webPort", DefaultCSQTTWebPort)
		setDefault(side, "mainPassword", randomPassword())
		setDefault(side, "webUser", "ex3ui")
		setDefault(side, "webPass", randomPassword())
		setDefault(side, "dns", "77.88.8.8,77.88.8.1")
		setDefault(side, "routeThroughXray", true)
		if err := normalizeListenAddr(side, ib, DefaultCSQTTListenAddr); err != nil {
			return "", err
		}
		// The admin API is bound to loopback by the panel, so a secure cookie
		// would be rejected over plain HTTP; keep it off.
		side["secureCookie"] = false
	}
	if ib.Protocol == model.QWDTT || ib.Protocol == model.CSQTT {
		dir, _ := side["configDir"].(string)
		if err := validateConfigDir(dir); err != nil {
			return "", fmt.Errorf("%s: %w", DisplayName(ib.Protocol), err)
		}
	}
	rawSide, err := json.Marshal(side)
	if err != nil {
		return "", err
	}
	root["sidecar"] = rawSide
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Validate checks the values the sidecar binaries cannot start without.
func Validate(ib *model.Inbound) error {
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		return err
	}
	if ib.NodeID != nil {
		return fmt.Errorf("%s is available only on the local panel node", DisplayName(ib.Protocol))
	}
	switch inst.Protocol {
	case model.QWDTT:
		cfg := inst.QWDTT
		if inst.Port <= 0 || inst.Port > 65535 {
			return fmt.Errorf("qWDTT: listen port is required")
		}
		if cfg.WGPort <= 0 || cfg.WGPort > 65535 {
			return fmt.Errorf("qWDTT: wg port must be between 1 and 65535")
		}
		if cfg.RawPort > 0 && (cfg.RawPort > 65535 || cfg.RawPort == inst.Port || cfg.RawPort == cfg.WGPort) {
			return fmt.Errorf("qWDTT: raw port conflicts with another qWDTT port")
		}
		if cfg.WGPort == inst.Port {
			return fmt.Errorf("qWDTT: wg port must differ from the listen port")
		}
	case model.OLCRTC:
		cfg := inst.OLCRTC
		if strings.TrimSpace(cfg.RoomID) == "" {
			return fmt.Errorf("olcRTC: room id is required")
		}
		switch cfg.Provider {
		case "jitsi", "telemost", "wbstream":
		default:
			return fmt.Errorf("olcRTC: provider must be jitsi, telemost or wbstream")
		}
		switch cfg.Transport {
		case "datachannel", "vp8channel", "seichannel", "videochannel":
		default:
			return fmt.Errorf("olcRTC: transport must be datachannel, vp8channel, seichannel or videochannel")
		}
		if !olcrtcTransportOK(cfg.Provider, cfg.Transport) {
			return fmt.Errorf("olcRTC: %s does not support the %s transport", cfg.Provider, cfg.Transport)
		}
		if cfg.Transport == "videochannel" {
			switch cfg.VideoCodec {
			case "", "qrcode", "tile":
			default:
				return fmt.Errorf("olcRTC: video codec must be qrcode or tile")
			}
		}
		if !validHexKey(cfg.CryptoKey) {
			return fmt.Errorf("olcRTC: crypto key must be 64 hex characters")
		}
		if _, _, err := net.SplitHostPort(cfg.DNS); err != nil {
			return fmt.Errorf("olcRTC: dns must be host:port")
		}
	case model.CSQTT:
		cfg := inst.CSQTT
		if inst.Port <= 0 || inst.Port > 65535 {
			return fmt.Errorf("CSQTT: listen port is required")
		}
		if cfg.WebPort <= 0 || cfg.WebPort > 65535 || cfg.WebPort == inst.Port {
			return fmt.Errorf("CSQTT: web port must differ from the listen port")
		}
	}
	for _, cl := range inst.Clients {
		if strings.TrimSpace(cl.Email) == "" {
			return fmt.Errorf("%s: client email is required", DisplayName(inst.Protocol))
		}
		// A client without its own password is a share-only member: qWDTT links
		// fall back to the inbound's shared password, while CSQTT and olcRTC
		// have no shared client fallback.
	}
	return nil
}

// BuildArgs returns the argv for the sidecar binary.
func (i Instance) BuildArgs() []string {
	switch i.Protocol {
	case model.QWDTT:
		c := i.QWDTT
		args := []string{
			"-listen", net.JoinHostPort(i.Listen, strconv.Itoa(i.Port)),
			"-wg-port", strconv.Itoa(c.WGPort),
			"-config-dir", c.ConfigDir,
		}
		if c.MainPass != "" {
			args = append(args, "-password", c.MainPass)
		}
		if c.DNS != "" {
			args = append(args, "-dns", c.DNS)
		}
		if raw := strings.TrimSpace(c.ListenRaw); raw != "" {
			args = append(args, "-listen-raw", raw)
		}
		if direct := strings.TrimSpace(c.ListenDir); direct != "" {
			args = append(args, "-listen-direct", direct)
		}
		return args
	case model.OLCRTC:
		if i.OLCRTC.ConfigFile == "" {
			return nil
		}
		return []string{i.OLCRTC.ConfigFile}
	case model.CSQTT:
		c := i.CSQTT
		args := []string{
			"--listen", net.JoinHostPort(i.Listen, strconv.Itoa(i.Port)),
			"--web-port", strconv.Itoa(c.WebPort),
			"--config-dir", c.ConfigDir,
		}
		if c.WebUser != "" {
			args = append(args, "--web-user", c.WebUser)
		}
		if c.WebPass != "" {
			args = append(args, "--web-pass", c.WebPass)
		}
		if c.MainPass != "" {
			args = append(args, "--password", c.MainPass)
		}
		if c.DNS != "" {
			args = append(args, "--dns", c.DNS)
		}
		return args
	default:
		return nil
	}
}

// Links returns the share links for one client.
func (i Instance) Links(client model.Client) []string {
	if !client.Enable {
		return nil
	}
	switch i.Protocol {
	case model.QWDTT:
		if link := i.qwdttURI(client); link != "" {
			return []string{link}
		}
	case model.CSQTT:
		if link := i.csqttURI(client); link != "" {
			return []string{link}
		}
	case model.OLCRTC:
		if link := i.olcrtcURI(); link != "" {
			return []string{link}
		}
	}
	return nil
}

func (i Instance) shareName(fallback string) string {
	for _, candidate := range []string{i.Remark, fallback} {
		if name := strings.TrimSpace(candidate); name != "" {
			return name
		}
	}
	return "client"
}

// firstQWDTTClientPassword returns the shared credential of an enabled qWDTT
// client; generic Password stays as the legacy fallback for old records.
func firstQWDTTClientPassword(clients []model.Client) string {
	for _, cl := range clients {
		if !cl.Enable {
			continue
		}
		pass := strings.TrimSpace(cl.QwdttPassword)
		if pass == "" {
			pass = strings.TrimSpace(cl.Password)
		}
		if pass != "" {
			return pass
		}
	}
	return ""
}

func (i Instance) qwdttHashes(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	if v := strings.TrimSpace(i.QWDTT.HashesByEmail[client.Email]); v != "" {
		return v
	}
	return strings.TrimSpace(i.QWDTT.Hashes)
}

func (i Instance) qwdttPeer() string {
	c := i.QWDTT
	if c == nil {
		return ""
	}
	host := i.shareHost(c.SubHost)
	if host == "" || i.Port <= 0 {
		return ""
	}
	if explicit := strings.TrimSpace(c.SubHost); explicit != "" {
		if _, _, err := net.SplitHostPort(explicit); err == nil {
			return explicit
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(i.Port))
}

func (i Instance) qwdttPassword(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	pass := strings.TrimSpace(client.QwdttPassword)
	if pass == "" {
		pass = strings.TrimSpace(client.Password)
	}
	if pass == "" {
		pass = strings.TrimSpace(i.QWDTT.MainPass)
	}
	return pass
}

// csqttPassword returns the client's own CSQTT credential. The inbound main
// password and the generic client password are not client fallbacks.
func (i Instance) csqttPassword(client model.Client) string {
	if i.CSQTT == nil {
		return ""
	}
	return strings.TrimSpace(client.CsqttPassword)
}

func (i Instance) qwdttURI(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	peer := i.qwdttPeer()
	pass := i.qwdttPassword(client)
	if peer == "" || pass == "" {
		return ""
	}
	workers := i.QWDTT.Workers
	if workers <= 0 {
		workers = 16
	}
	clientPort := i.QWDTT.ClientPort
	if clientPort <= 0 {
		clientPort = 9000
	}
	q := url.Values{}
	q.Set("name", i.shareName(client.Email))
	q.Set("peer", peer)
	if hashes := i.qwdttHashes(client); hashes != "" {
		q.Set("hashes", hashes)
	}
	q.Set("workers", strconv.Itoa(workers))
	q.Set("port", strconv.Itoa(clientPort))
	q.Set("pass", pass)
	return "qwdtt://config?" + q.Encode()
}

// LegacyURI renders the classic wdtt://ip:dtls:wg:local:pass:hash form.
func (i Instance) LegacyURI() string {
	if i.QWDTT == nil {
		return ""
	}
	peer := i.qwdttPeer()
	pass := firstQWDTTClientPassword(i.Clients)
	if pass == "" {
		pass = strings.TrimSpace(i.QWDTT.MainPass)
	}
	hash := firstHash(i.QWDTT.Hashes)
	if peer == "" || pass == "" || hash == "" {
		return ""
	}
	host := peer
	if h, _, err := net.SplitHostPort(peer); err == nil {
		host = h
	}
	clientPort := i.QWDTT.ClientPort
	if clientPort <= 0 {
		clientPort = 9000
	}
	return fmt.Sprintf("wdtt://%s:%d:%d:%d:%s:%s",
		host, i.Port, i.QWDTT.WGPort, clientPort, pass, hash)
}

// QwdttSubProfile is one profile in the SpaceNeuroX JSON subscription.
type QwdttSubProfile struct {
	Name     string `json:"name"`
	Peer     string `json:"peer"`
	Hashes   string `json:"hashes"`
	Workers  int    `json:"workers"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// QwdttSubscription is the public JSON document the Android client imports.
type QwdttSubscription struct {
	SubscriptionName string            `json:"subscriptionName"`
	Description      string            `json:"description,omitempty"`
	Version          int               `json:"version"`
	UpdatedAt        string            `json:"updatedAt"`
	Profiles         []QwdttSubProfile `json:"profiles"`
}

// SubJSON returns the qWDTT Android subscription document.
func (i Instance) SubJSON(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	peer := i.qwdttPeer()
	pass := i.qwdttPassword(client)
	if peer == "" || pass == "" {
		return ""
	}
	name := i.shareName(client.Email)
	workers := i.QWDTT.Workers
	if workers <= 0 {
		workers = 16
	}
	clientPort := i.QWDTT.ClientPort
	if clientPort <= 0 {
		clientPort = 9000
	}
	doc := QwdttSubscription{
		SubscriptionName: name,
		Description:      "qWDTT tunnel via " + peer,
		Version:          1,
		UpdatedAt:        time.Now().Format("2006-01-02"),
		Profiles: []QwdttSubProfile{{
			Name:     name,
			Peer:     peer,
			Hashes:   i.qwdttHashes(client),
			Workers:  workers,
			Port:     clientPort,
			Password: pass,
		}},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

func firstHash(raw string) string {
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\r'
	}) {
		if hash := strings.TrimSpace(part); hash != "" {
			return hash
		}
	}
	return ""
}

func csqttHashList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';'
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
			if len(out) == 6 {
				break
			}
		}
	}
	return out
}

func (i Instance) csqttURI(client model.Client) string {
	c := i.CSQTT
	host := i.shareHost(c.SubHost)
	pass := i.csqttPassword(client)
	if host == "" || i.Port <= 0 || pass == "" {
		return ""
	}
	hashes := c.Hashes
	if v := strings.TrimSpace(c.HashesByEmail[client.Email]); v != "" {
		hashes = v
	}
	link := fmt.Sprintf("csqtt://connect?v=2&host=%s&peer=%d&password=%s",
		urlEncode(host), i.Port, urlEncode(pass))
	parts := csqttHashList(hashes)
	if len(parts) > 0 {
		encoded := make([]string, 0, len(parts))
		for _, part := range parts {
			encoded = append(encoded, url.QueryEscape(part))
		}
		link += "&hashes=" + strings.Join(encoded, "+")
	}
	return link
}

func (i Instance) olcrtcURI() string {
	c := i.OLCRTC
	if strings.TrimSpace(c.RoomID) == "" || strings.TrimSpace(c.CryptoKey) == "" {
		return ""
	}
	transport := c.Transport
	switch transport {
	case "vp8channel":
		transport = fmt.Sprintf("vp8channel<vp8-fps=%d&vp8-batch=%d>", c.VP8Fps, c.VP8Batch)
	case "seichannel":
		transport = fmt.Sprintf("seichannel<fps=%d&batch=%d&frag=%d&ack-ms=%d>", c.SeiFps, c.SeiBatch, c.SeiFrag, c.SeiAck)
	case "videochannel":
		codec := c.VideoCodec
		if codec == "" {
			codec = "qrcode"
		}
		transport = fmt.Sprintf("videochannel<video-w=%d&video-h=%d&video-fps=%d&video-codec=%s>", c.VideoW, c.VideoH, c.VideoFps, codec)
	case "", "datachannel":
		transport = "datachannel"
	}
	return fmt.Sprintf("olcrtc://%s?%s@%s#%s", c.Provider, transport, c.RoomID, c.CryptoKey)
}

// shareHost prefers the explicit share host and otherwise falls back to the
// inbound listen/public address.
func (i Instance) shareHost(explicit string) string {
	if h := strings.TrimSpace(explicit); h != "" {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return h
	}
	return i.publicHost()
}

func portOf(hostPort string) int {
	_, port, err := net.SplitHostPort(strings.TrimSpace(hostPort))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

// applyListenAddr mirrors the sidecar's own listener into the instance fields
// used by links, port-conflict checks and the firewall.
func applyListenAddr(raw string, inst *Instance) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return
	}
	if host != "" {
		inst.Listen = host
	}
	if n, err := strconv.Atoi(port); err == nil && n > 0 && n <= 65535 {
		inst.Port = n
	}
}

// normalizeListenAddr canonicalises settings.sidecar.listenAddr and keeps
// inbound.Listen/inbound.Port in sync so the rest of the panel sees the real
// sidecar listener. Older inbounds without the field keep their stored port.
func normalizeListenAddr(side map[string]any, ib *model.Inbound, fallback string) error {
	raw, _ := side["listenAddr"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if ib.Port > 0 {
			raw = net.JoinHostPort(defaultListen(ib.Listen), strconv.Itoa(ib.Port))
		} else {
			raw = fallback
		}
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s: listener must be host:port", DisplayName(ib.Protocol))
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s: listener port must be between 1 and 65535", DisplayName(ib.Protocol))
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = defaultListen(ib.Listen)
	}
	side["listenAddr"] = net.JoinHostPort(host, strconv.Itoa(n))
	ib.Listen = defaultListen(host)
	ib.Port = n
	return nil
}

func mergeOLCRTCDefaults(c OLCRTCConfig) OLCRTCConfig {
	if c.Provider == "" {
		c.Provider = "jitsi"
	}
	if c.Transport == "" {
		c.Transport = "datachannel"
	}
	if c.DNS == "" {
		c.DNS = "8.8.8.8:53"
	}
	if c.VP8Fps == 0 {
		c.VP8Fps = 60
	}
	if c.VP8Batch == 0 {
		c.VP8Batch = 64
	}
	if c.SeiFps == 0 {
		c.SeiFps = 30
	}
	if c.SeiBatch == 0 {
		c.SeiBatch = 64
	}
	if c.SeiFrag == 0 {
		c.SeiFrag = 900
	}
	if c.SeiAck == 0 {
		c.SeiAck = 2000
	}
	if c.VideoW == 0 {
		c.VideoW = 1080
	}
	if c.VideoH == 0 {
		c.VideoH = 1080
	}
	if c.VideoFps == 0 {
		c.VideoFps = 30
	}
	if c.VideoCodec == "" {
		c.VideoCodec = "qrcode"
	}
	return c
}

func olcrtcTransportOK(provider, transport string) bool {
	switch transport {
	case "datachannel":
		return provider == "jitsi"
	case "vp8channel", "videochannel":
		return provider == "jitsi" || provider == "telemost" || provider == "wbstream"
	case "seichannel":
		return provider == "jitsi" || provider == "wbstream"
	default:
		return false
	}
}

// validateConfigDir keeps a sidecar's state folder inside the panel data dir.
// LucX does the same; an outside path would be silently ignored on restart.
func validateConfigDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("config dir is invalid: %w", err)
	}
	root, err := filepath.Abs(config.GetDBFolderPath())
	if err != nil {
		return fmt.Errorf("config dir is invalid: %w", err)
	}
	if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("config dir must be inside %s", root)
}

func normalizeCSQTTDNS(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "77.88.8.8,77.88.8.1"
	}
	return v
}

func qwdttRouteTable(inboundID int) int { return 1900 + inboundID }

func csqttRouteTable(inboundID int) int { return 2100 + inboundID }

// TunName is the Xray TUN device paired with one routed sidecar inbound.
func TunName(inboundID int) string { return "tun" + strconv.Itoa(inboundID) }

// TunGateway returns the /30 address Xray assigns to that TUN. The ranges sit
// outside the client subnets the qWDTT/CSQTT binaries claim.
func TunGateway(p model.Protocol, inboundID int) string {
	base := "10.253."
	if p == model.CSQTT {
		base = "10.252."
	}
	if inboundID >= 1 && inboundID < 254 {
		return base + strconv.Itoa(inboundID) + ".1/30"
	}
	return "10.251." + strconv.Itoa((inboundID%253)+1) + ".1/30"
}

// freeLocalPort asks the kernel for an unused loopback port.
func freeLocalPort() (int, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// publicHost returns the host clients should dial. Explicit settings win;
// wildcard listens fall back to the configured/detected public address.
func (i Instance) publicHost() string {
	var explicit string
	switch i.Protocol {
	case model.QWDTT:
		explicit = i.QWDTT.PublicHost
	case model.CSQTT:
		explicit = i.CSQTT.PublicIP
	case model.OLCRTC:
		explicit = i.OLCRTC.PublicHost
	}
	if h := strings.TrimSpace(explicit); h != "" {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return h
	}
	if host := nonWildcardHost(i.Listen); host != "" {
		return host
	}
	return PublicIP()
}

func nonWildcardHost(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(listen); err == nil {
		listen = host
	}
	switch listen {
	case "0.0.0.0", "::", "[::]", "127.0.0.1", "localhost":
		return ""
	}
	return listen
}

func defaultListen(listen string) string {
	if strings.TrimSpace(listen) == "" {
		return "0.0.0.0"
	}
	return listen
}

func normalizeDNS(v string) string {
	for _, part := range strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if server := strings.TrimSpace(part); server != "" {
			return server
		}
	}
	return "1.1.1.1"
}

func setDefault(m map[string]any, key string, value any) {
	if _, ok := m[key]; ok {
		if s, isStr := m[key].(string); isStr && strings.TrimSpace(s) == "" {
			m[key] = value
		}
		if m[key] == nil {
			m[key] = value
		}
		return
	}
	m[key] = value
}

func fingerprint(i Instance) string {
	payload := struct {
		Protocol model.Protocol
		Listen   string
		Port     int
		Clients  []model.Client
		Settings any
	}{i.Protocol, i.Listen, i.Port, i.Clients, i.Settings()}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func randomPassword() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", 16)
	}
	return hex.EncodeToString(b)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validHexKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func urlEncode(s string) string {
	return url.QueryEscape(strings.TrimSpace(s))
}
