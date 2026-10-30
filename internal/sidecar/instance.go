package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Defaults pre-seeded into a sidecar settings block when it is decoded.
const (
	DefaultQWDTTWGPort     = 56001
	DefaultQWDTTRawPort    = 56003
	DefaultQWDTTListenAddr = "0.0.0.0:56000"
	DefaultCSQTTListenAddr = "0.0.0.0:46000"
	DefaultCSQTTWebPort    = 46002
	DefaultOLCRTCDataDir   = "data"
)

var errNotSidecarInbound = errors.New("not a sidecar inbound")

// QWDTTConfig is the qWDTT block of an inbound's settings JSON.
type QWDTTConfig struct {
	ListenAddr    string            `json:"listenAddr,omitempty"`
	WGPort        int               `json:"wgPort"`
	RawPort       int               `json:"rawPort"`
	ListenRaw     string            `json:"listenRaw,omitempty"`
	ListenDir     string            `json:"listenDirect,omitempty"`
	Hashes        string            `json:"hashes,omitempty"`
	Workers       int               `json:"workers,omitempty"`
	ClientPort    int               `json:"clientPort,omitempty"`
	ConfigDir     string            `json:"configDir,omitempty"`
	MainPass      string            `json:"mainPassword,omitempty"`
	DNS           string            `json:"dns,omitempty"`
	PublicHost    string            `json:"publicHost,omitempty"`
	SubHost       string            `json:"subHost,omitempty"`
	HashesByEmail map[string]string `json:"hashesByEmail,omitempty"`

	RouteThroughXray bool   `json:"routeThroughXray,omitempty"`
	OutboundTag      string `json:"outboundTag,omitempty"`
	RouteXrayPort    int    `json:"routeXrayPort,omitempty"`
}

// OLCRTCConfig is the olcRTC block of an inbound's settings JSON.
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

// CSQTTConfig is the CSQTT block of an inbound's settings JSON.
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

// QwdttSubProfile is one profile of a qWDTT subscription document.
type QwdttSubProfile struct {
	Name     string `json:"name"`
	Peer     string `json:"peer"`
	Hashes   string `json:"hashes"`
	Workers  int    `json:"workers"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// QwdttSubscription is the qWDTT subscription document.
type QwdttSubscription struct {
	SubscriptionName string            `json:"subscriptionName"`
	Description      string            `json:"description,omitempty"`
	Version          int               `json:"version"`
	UpdatedAt        string            `json:"updatedAt"`
	Profiles         []QwdttSubProfile `json:"profiles"`
}

// Instance is the resolved runtime form of one sidecar inbound.
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

	RouteThroughXray bool
	TunName          string
	RouteTable       int
	RouteIfaces      []string
	OutboundTag      string
	RouteXrayPort    int

	Fingerprint string
}

// Supported reports whether p is served by an external sidecar core.
func Supported(p model.Protocol) bool {
	return p == model.QWDTT || p == model.OLCRTC || p == model.CSQTT
}

// DisplayName renders the human-readable name of a sidecar protocol.
func DisplayName(p model.Protocol) string {
	switch p {
	case model.QWDTT:
		return "qWDTT"
	case model.OLCRTC:
		return "olcRTC"
	case model.CSQTT:
		return "CSQTT"
	}
	return string(p)
}

// DataDir returns the panel-owned data directory of one inbound.
func DataDir(inboundID int, protocol model.Protocol) string {
	return filepath.Join(config.GetDBFolderPath(), "sidecar", fmt.Sprintf("%s-%d", protocol, inboundID))
}

// BinaryCandidates lists the candidate binary paths for a protocol, in order.
func BinaryCandidates(p model.Protocol) []string {
	dir := config.GetBinFolderPath()
	var names []string
	switch p {
	case model.QWDTT:
		names = withExeOnWindows("extra-qwdtt", "extra-qwdtt-"+runtime.GOARCH)
	case model.OLCRTC:
		names = withExeOnWindows("extra-olcrtc", "extra-olcrtc-"+runtime.GOARCH)
	case model.CSQTT:
		names = []string{"csqtt-linux-" + runtime.GOARCH, "csqtt", "csqtt-linux-amd64"}
		if runtime.GOOS == "windows" {
			names[1] = "csqtt.exe"
		}
	default:
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func withExeOnWindows(names ...string) []string {
	if runtime.GOOS != "windows" {
		return names
	}
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name + ".exe"
	}
	return out
}

// BinaryPath returns the first existing candidate, or the first candidate when
// none exists yet. Empty only for unsupported protocols.
func BinaryPath(p model.Protocol) string {
	candidates := BinaryCandidates(p)
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			continue
		} else if err == nil {
			return candidate
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// TunName returns the TUN device name used by an inbound's Xray bridge.
func TunName(inboundID int) string {
	return fmt.Sprintf("tun%d", inboundID)
}

// TunGateway returns the gateway address of an inbound's Xray TUN device.
func TunGateway(p model.Protocol, inboundID int) string {
	switch p {
	case model.QWDTT:
		if inboundID >= 1 && inboundID <= 253 {
			return fmt.Sprintf("10.253.%d.1/30", inboundID)
		}
	case model.CSQTT:
		if inboundID >= 1 && inboundID <= 253 {
			return fmt.Sprintf("10.252.%d.1/30", inboundID)
		}
	default:
		return ""
	}
	return fmt.Sprintf("10.251.%d.1/30", inboundID%253+1)
}

// Settings returns the protocol-specific settings block or nil.
func (i Instance) Settings() any {
	switch i.Protocol {
	case model.QWDTT:
		return i.QWDTT
	case model.OLCRTC:
		return i.OLCRTC
	case model.CSQTT:
		return i.CSQTT
	}
	return nil
}

// InstanceFromInbound resolves an inbound record into a runtime instance.
func InstanceFromInbound(ib *model.Inbound) (Instance, error) {
	if ib == nil || !Supported(ib.Protocol) {
		return Instance{}, errNotSidecarInbound
	}
	name := DisplayName(ib.Protocol)
	root, err := decodeSettingsObject(ib.Settings)
	if err != nil {
		return Instance{}, fmt.Errorf("%s: invalid settings: %w", name, err)
	}

	inst := Instance{
		InboundID: ib.Id,
		Tag:       ib.Tag,
		Remark:    ib.Remark,
		Protocol:  ib.Protocol,
		Listen:    effectiveListen(ib.Listen),
		Port:      ib.Port,
	}
	inst.Clients = resolveClients(ib, root)
	dataDir := DataDir(ib.Id, ib.Protocol)
	rawSidecar := sidecarBlock(root)

	switch ib.Protocol {
	case model.QWDTT:
		cfg := seedQWDTT()
		if err := decodeSidecarBlock(rawSidecar, cfg, name); err != nil {
			return Instance{}, err
		}
		inst.QWDTT = cfg
		if err := resolveQWDTT(&inst, dataDir); err != nil {
			return Instance{}, err
		}
	case model.OLCRTC:
		cfg := seedOLCRTC()
		if err := decodeSidecarBlock(rawSidecar, cfg, name); err != nil {
			return Instance{}, err
		}
		inst.OLCRTC = cfg
		if err := resolveOLCRTC(&inst, dataDir); err != nil {
			return Instance{}, err
		}
	case model.CSQTT:
		cfg := seedCSQTT()
		if err := decodeSidecarBlock(rawSidecar, cfg, name); err != nil {
			return Instance{}, err
		}
		inst.CSQTT = cfg
		if err := resolveCSQTT(&inst, dataDir); err != nil {
			return Instance{}, err
		}
	}

	inst.Fingerprint = fingerprint(inst)
	return inst, nil
}

func seedQWDTT() *QWDTTConfig {
	return &QWDTTConfig{
		WGPort:     DefaultQWDTTWGPort,
		RawPort:    DefaultQWDTTRawPort,
		Workers:    16,
		ClientPort: 9000,
		DNS:        "1.1.1.1",
	}
}

func seedOLCRTC() *OLCRTCConfig {
	return &OLCRTCConfig{
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
	}
}

func seedCSQTT() *CSQTTConfig {
	return &CSQTTConfig{
		WebPort: DefaultCSQTTWebPort,
		DNS:     "77.88.8.8,77.88.8.1",
	}
}

func decodeSettingsObject(settings string) (map[string]json.RawMessage, error) {
	if strings.TrimSpace(settings) == "" {
		return map[string]json.RawMessage{}, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settings), &root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("settings must be a JSON object")
	}
	return root, nil
}

// sidecarBlock returns the raw sidecar object, nil when absent, empty or null.
func sidecarBlock(root map[string]json.RawMessage) json.RawMessage {
	raw, ok := root["sidecar"]
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return raw
}

func decodeSidecarBlock(raw json.RawMessage, target any, name string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%s: invalid sidecar settings: %w", name, err)
	}
	return nil
}

func effectiveListen(listen string) string {
	if listen = strings.TrimSpace(listen); listen == "" {
		return "0.0.0.0"
	}
	return trimBrackets(listen)
}

// resolveClients prefers the normalised clients table and falls back to the
// settings snapshot when the table is unavailable or the query fails.
func resolveClients(ib *model.Inbound, root map[string]json.RawMessage) []model.Client {
	if ib.Id > 0 {
		if db := database.GetDB(); db != nil {
			var rows []model.ClientRecord
			err := db.Model(&model.ClientRecord{}).
				Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
				Where("client_inbounds.inbound_id = ?", ib.Id).
				Order("clients.id ASC").
				Find(&rows).Error
			if err == nil {
				clients := make([]model.Client, 0, len(rows))
				for idx := range rows {
					clients = append(clients, *rows[idx].ToClient())
				}
				return clients
			}
		}
	}
	return snapshotClients(root)
}

func snapshotClients(root map[string]json.RawMessage) []model.Client {
	raw, ok := root["clients"]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var clients []model.Client
	if err := json.Unmarshal(raw, &clients); err != nil {
		return nil
	}
	return clients
}

func resolveQWDTT(inst *Instance, dataDir string) error {
	cfg := inst.QWDTT
	name := DisplayName(inst.Protocol)
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = filepath.Join(dataDir, "config")
	}
	if err := guardConfigDir(cfg.ConfigDir, name); err != nil {
		return err
	}
	if dns := firstToken(cfg.DNS); dns != "" {
		cfg.DNS = dns
	} else {
		cfg.DNS = "1.1.1.1"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 16
	}
	if cfg.ClientPort <= 0 {
		cfg.ClientPort = 9000
	}
	applyListenerOverride(inst, cfg.ListenAddr)

	if strings.TrimSpace(cfg.ListenRaw) == "" {
		if cfg.RawPort > 0 {
			cfg.ListenRaw = net.JoinHostPort(inst.Listen, strconv.Itoa(cfg.RawPort))
		}
	} else if _, port, err := net.SplitHostPort(strings.TrimSpace(cfg.ListenRaw)); err == nil {
		if parsed, convErr := strconv.Atoi(port); convErr == nil && parsed > 0 {
			cfg.RawPort = parsed
		}
	}

	inst.TunName = TunName(inst.InboundID)
	inst.RouteTable = 1900 + inst.InboundID
	inst.RouteIfaces = []string{"wdtt0"}
	if cfg.RawPort > 0 {
		inst.RouteIfaces = append(inst.RouteIfaces, "wdttraw0")
	}
	inst.RouteThroughXray = cfg.RouteThroughXray
	inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
	inst.RouteXrayPort = 0
	return nil
}

func resolveOLCRTC(inst *Instance, dataDir string) error {
	cfg := inst.OLCRTC
	name := DisplayName(inst.Protocol)
	if cfg.ConfigFile == "" {
		cfg.ConfigFile = filepath.Join(dataDir, "server.yaml")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(dataDir, DefaultOLCRTCDataDir)
	}
	// The panel writes both paths as root, so an operator-supplied value must
	// stay inside the panel data folder instead of overwriting arbitrary files.
	if err := guardConfigDir(filepath.Dir(cfg.ConfigFile), name); err != nil {
		return err
	}
	if err := guardConfigDir(cfg.DataDir, name); err != nil {
		return err
	}
	if cfg.VP8Fps == 0 {
		cfg.VP8Fps = 60
	}
	if cfg.VP8Batch == 0 {
		cfg.VP8Batch = 64
	}
	if cfg.SeiFps == 0 {
		cfg.SeiFps = 30
	}
	if cfg.SeiBatch == 0 {
		cfg.SeiBatch = 64
	}
	if cfg.SeiFrag == 0 {
		cfg.SeiFrag = 900
	}
	if cfg.SeiAck == 0 {
		cfg.SeiAck = 2000
	}
	if cfg.VideoW == 0 {
		cfg.VideoW = 1080
	}
	if cfg.VideoH == 0 {
		cfg.VideoH = 1080
	}
	if cfg.VideoFps == 0 {
		cfg.VideoFps = 30
	}

	inst.TunName = ""
	inst.RouteTable = 0
	inst.RouteIfaces = nil
	inst.RouteThroughXray = cfg.RouteThroughXray
	inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
	inst.RouteXrayPort = cfg.RouteXrayPort
	return nil
}

func resolveCSQTT(inst *Instance, dataDir string) error {
	cfg := inst.CSQTT
	name := DisplayName(inst.Protocol)
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = filepath.Join(dataDir, "config")
	}
	if err := guardConfigDir(cfg.ConfigDir, name); err != nil {
		return err
	}
	if dns := strings.TrimSpace(cfg.DNS); dns != "" {
		cfg.DNS = dns
	} else {
		cfg.DNS = "77.88.8.8,77.88.8.1"
	}
	applyListenerOverride(inst, cfg.ListenAddr)

	inst.TunName = TunName(inst.InboundID)
	inst.RouteTable = 2100 + inst.InboundID
	inst.RouteIfaces = []string{"csqtt1"}
	inst.RouteThroughXray = cfg.RouteThroughXray
	inst.OutboundTag = strings.TrimSpace(cfg.OutboundTag)
	inst.RouteXrayPort = 0
	return nil
}

// applyListenerOverride lets a parsable listenAddr replace the instance
// endpoint; blank or malformed input is ignored here.
func applyListenerOverride(inst *Instance, listenAddr string) {
	trimmed := strings.TrimSpace(listenAddr)
	if trimmed == "" {
		return
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return
	}
	parsed, err := strconv.Atoi(port)
	if err != nil {
		return
	}
	if host != "" {
		inst.Listen = trimBrackets(host)
	}
	if parsed >= 1 && parsed <= 65535 {
		inst.Port = parsed
	}
}

func guardConfigDir(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	absValue, err := filepath.Abs(value)
	if err != nil {
		return fmt.Errorf("%s: config dir must be inside the panel data folder: %w", name, err)
	}
	absRoot, err := filepath.Abs(config.GetDBFolderPath())
	if err != nil {
		return fmt.Errorf("%s: config dir must be inside the panel data folder: %w", name, err)
	}
	if absValue == absRoot || strings.HasPrefix(absValue, absRoot+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("%s: config dir must be inside the panel data folder", name)
}

func fingerprint(inst Instance) string {
	payload := struct {
		Protocol model.Protocol `json:"protocol"`
		Listen   string         `json:"listen"`
		Port     int            `json:"port"`
		Clients  []model.Client `json:"clients"`
		Settings any            `json:"settings"`
	}{
		Protocol: inst.Protocol,
		Listen:   inst.Listen,
		Port:     inst.Port,
		Clients:  inst.Clients,
		Settings: fingerprintSettings(inst),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fingerprintSettings copies the typed block without the routing fields, which
// never influence the child process.
func fingerprintSettings(inst Instance) any {
	switch inst.Protocol {
	case model.QWDTT:
		if inst.QWDTT == nil {
			return nil
		}
		cfg := *inst.QWDTT
		cfg.RouteThroughXray = false
		cfg.OutboundTag = ""
		cfg.RouteXrayPort = 0
		return cfg
	case model.OLCRTC:
		if inst.OLCRTC == nil {
			return nil
		}
		cfg := *inst.OLCRTC
		cfg.RouteThroughXray = false
		cfg.OutboundTag = ""
		cfg.RouteXrayPort = 0
		return cfg
	case model.CSQTT:
		if inst.CSQTT == nil {
			return nil
		}
		cfg := *inst.CSQTT
		cfg.RouteThroughXray = false
		cfg.OutboundTag = ""
		cfg.RouteXrayPort = 0
		return cfg
	}
	return nil
}

// BuildArgs renders the launch arguments for the protocol's binary.
func (i Instance) BuildArgs() []string {
	switch i.Protocol {
	case model.QWDTT:
		cfg := i.QWDTT
		if cfg == nil {
			return nil
		}
		args := []string{
			"-listen", net.JoinHostPort(i.Listen, strconv.Itoa(i.Port)),
			"-wg-port", strconv.Itoa(cfg.WGPort),
			"-config-dir", cfg.ConfigDir,
		}
		if cfg.MainPass != "" {
			// The password goes through a 0600 file: argv is world-readable in
			// /proc/<pid>/cmdline and ps.
			args = append(args, "-password-file", qwdttOwnerPasswordPath(cfg.ConfigDir))
		}
		if cfg.DNS != "" {
			args = append(args, "-dns", cfg.DNS)
		}
		if raw := strings.TrimSpace(cfg.ListenRaw); raw != "" {
			args = append(args, "-listen-raw", raw)
		}
		if direct := strings.TrimSpace(cfg.ListenDir); direct != "" {
			args = append(args, "-listen-direct", direct)
		}
		return args
	case model.CSQTT:
		cfg := i.CSQTT
		if cfg == nil {
			return nil
		}
		args := []string{
			"--listen", net.JoinHostPort(i.Listen, strconv.Itoa(i.Port)),
			"--web-port", strconv.Itoa(cfg.WebPort),
			"--config-dir", cfg.ConfigDir,
		}
		// Credentials travel in the child's environment (CSQTT_*), never argv:
		// /proc/<pid>/cmdline and ps expose arguments to every local user.
		if cfg.DNS != "" {
			args = append(args, "--dns", cfg.DNS)
		}
		return args
	case model.OLCRTC:
		cfg := i.OLCRTC
		if cfg == nil || strings.TrimSpace(cfg.ConfigFile) == "" {
			return nil
		}
		return []string{cfg.ConfigFile}
	}
	return nil
}

// SecretEnv returns the child's secret environment. CSQTT reads its
// credentials from CSQTT_* variables, which keeps them out of argv.
func (i Instance) SecretEnv() []string {
	if i.Protocol != model.CSQTT || i.CSQTT == nil {
		return nil
	}
	cfg := i.CSQTT
	env := make([]string, 0, 3)
	if cfg.WebUser != "" {
		env = append(env, "CSQTT_WEB_USER="+cfg.WebUser)
	}
	if cfg.WebPass != "" {
		env = append(env, "CSQTT_WEB_PASS="+cfg.WebPass)
	}
	if cfg.MainPass != "" {
		env = append(env, "CSQTT_MAIN_PASSWORD="+cfg.MainPass)
	}
	return env
}

func trimBrackets(host string) string {
	return strings.Trim(strings.TrimSpace(host), "[]")
}

func firstToken(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexAny(value, ", \t\r\n;"); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}
