package service

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// sidecarMigrationMarker guards the one-shot move of the pre-1.4.2 global core
// settings into ordinary inbound rows.
const sidecarMigrationMarker = "sidecarMigratedV1"

const (
	legacyQWDTTKey  = "extraService_qwdtt"
	legacyOLCRTCKey = "extraService_olcrtc"
)

// legacyQWDTT is the shape stored under extraService_qwdtt before 1.4.2.
type legacyQWDTT struct {
	Enabled    bool   `json:"enabled"`
	AutoStart  bool   `json:"autoStart"`
	ListenAddr string `json:"listenAddr"`
	WGPort     int    `json:"wgPort"`
	Password   string `json:"password"`
	DNS        string `json:"dns"`
	ConfigDir  string `json:"configDir"`
	ListenRaw  string `json:"listenRaw"`
	VkHashes   string `json:"vkHashes"`
	Clients    []struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Enabled  bool   `json:"enabled"`
		VkHashes string `json:"vkHashes"`
	} `json:"clients"`
}

// legacyOLCRTC is the shape stored under extraService_olcrtc before 1.4.2.
type legacyOLCRTC struct {
	Enabled    bool   `json:"enabled"`
	AutoStart  bool   `json:"autoStart"`
	Provider   string `json:"provider"`
	RoomID     string `json:"roomId"`
	CryptoKey  string `json:"cryptoKey"`
	Transport  string `json:"transport"`
	DNS        string `json:"olcrtcDns"`
	VP8Fps     int    `json:"vp8Fps"`
	VP8Batch   int    `json:"vp8Batch"`
	DataDir    string `json:"dataDir"`
	ConfigFile string `json:"configFile"`
	Debug      bool   `json:"debug"`
}

// MigrateExtraServices converts the legacy global qWDTT/olcRTC configuration
// into regular inbounds, once. The marker is written only when every present
// source was converted or skipped because a matching row already exists; a
// failure leaves it unwritten so the next panel start retries, and the
// existing-row skip keeps that safe.
func MigrateExtraServices() {
	settingService := SettingService{}
	marker, err := settingService.GetRawSetting(sidecarMigrationMarker)
	if err != nil || strings.TrimSpace(marker) == "true" {
		return
	}

	ok := migrateLegacyQWDTT(&settingService)
	// Both sources are always attempted; one failure must not hide the other.
	if !migrateLegacyOLCRTC(&settingService) {
		ok = false
	}
	if !ok {
		return
	}
	_ = settingService.SetRawSetting(sidecarMigrationMarker, "true")
}

func migrateLegacyQWDTT(settingService *SettingService) bool {
	raw, err := settingService.GetRawSetting(legacyQWDTTKey)
	if err != nil {
		logger.Warning("sidecar migration: qWDTT config is invalid:", err)
		return false
	}
	if strings.TrimSpace(raw) == "" {
		return true
	}
	if sidecarInboundExists(model.QWDTT) {
		return true
	}
	legacy := legacyQWDTT{}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		logger.Warning("sidecar migration: qWDTT config is invalid:", err)
		return false
	}
	if _, _, err := (&InboundService{}).AddInbound(buildQWDTTInbound(&legacy)); err != nil {
		logger.Warning("sidecar migration: qWDTT:", err)
		return false
	}
	logger.Info("sidecar migration: qWDTT moved into an inbound")
	return true
}

func migrateLegacyOLCRTC(settingService *SettingService) bool {
	raw, err := settingService.GetRawSetting(legacyOLCRTCKey)
	if err != nil {
		logger.Warning("sidecar migration: olcRTC config is invalid:", err)
		return false
	}
	if strings.TrimSpace(raw) == "" {
		return true
	}
	if sidecarInboundExists(model.OLCRTC) {
		return true
	}
	legacy := legacyOLCRTC{}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		logger.Warning("sidecar migration: olcRTC config is invalid:", err)
		return false
	}
	if _, _, err := (&InboundService{}).AddInbound(buildOLCRTCInbound(&legacy)); err != nil {
		logger.Warning("sidecar migration: olcRTC:", err)
		return false
	}
	logger.Info("sidecar migration: olcRTC moved into an inbound")
	return true
}

// sidecarInboundExists reports whether any row of the protocol exists, enabled
// or not.
func sidecarInboundExists(protocol model.Protocol) bool {
	db := database.GetDB()
	if db == nil {
		return false
	}
	var count int64
	if err := db.Model(model.Inbound{}).Where("protocol = ?", protocol).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// buildQWDTTInbound renders the legacy qWDTT configuration as a qWDTT inbound.
// The row keeps the legacy listener; the ordinary create path adds the rest.
func buildQWDTTInbound(legacy *legacyQWDTT) *model.Inbound {
	host, port := splitLegacyListen(legacy.ListenAddr, 56000)
	block := map[string]any{
		"listenAddr":       net.JoinHostPort(host, strconv.Itoa(port)),
		"wgPort":           defaultInt(legacy.WGPort, 56001),
		"workers":          16,
		"routeThroughXray": true,
		"mainPassword":     legacy.Password,
		"dns":              legacy.DNS,
		"hashes":           legacy.VkHashes,
		"listenRaw":        legacy.ListenRaw,
	}
	if strings.TrimSpace(legacy.ConfigDir) != "" {
		block["configDir"] = legacy.ConfigDir
	}

	var clients []model.Client
	hashesByEmail := make(map[string]string, len(legacy.Clients))
	for i, legacyClient := range legacy.Clients {
		password := strings.TrimSpace(legacyClient.Password)
		if password == "" {
			// A client without a password has no credential to run with.
			continue
		}
		email := strings.TrimSpace(legacyClient.Name)
		if email == "" {
			email = fmt.Sprintf("client-%d", i+1)
		}
		clients = append(clients, model.Client{
			Email:    email,
			Password: password,
			Enable:   legacyClient.Enabled,
			SubID:    uuid.NewString(),
		})
		if hashes := strings.TrimSpace(legacyClient.VkHashes); hashes != "" {
			hashesByEmail[email] = hashes
		}
	}
	if len(hashesByEmail) > 0 {
		block["hashesByEmail"] = hashesByEmail
	}
	return legacyInbound(model.QWDTT, legacy.Enabled, host, port, clients, block)
}

// buildOLCRTCInbound renders the legacy olcRTC configuration as an olcRTC
// inbound. The core dials out, so the row listens on nothing and the ordinary
// create path allocates the loopback bridge port.
func buildOLCRTCInbound(legacy *legacyOLCRTC) *model.Inbound {
	block := map[string]any{
		"provider":         defaultString(legacy.Provider, "jitsi"),
		"roomId":           legacy.RoomID,
		"cryptoKey":        legacy.CryptoKey,
		"transport":        defaultString(legacy.Transport, "datachannel"),
		"dns":              defaultString(legacy.DNS, "8.8.8.8:53"),
		"vp8Fps":           defaultInt(legacy.VP8Fps, 30),
		"vp8Batch":         defaultInt(legacy.VP8Batch, 64),
		"dataDir":          legacy.DataDir,
		"debug":            legacy.Debug,
		"routeThroughXray": true,
	}
	if strings.TrimSpace(legacy.ConfigFile) != "" {
		block["configFile"] = legacy.ConfigFile
	}

	clients := []model.Client{{
		Email:    "default",
		Password: uuid.NewString(),
		Enable:   true,
		SubID:    uuid.NewString(),
	}}
	return legacyInbound(model.OLCRTC, legacy.Enabled, "0.0.0.0", 0, clients, block)
}

// legacyInbound assembles the settings document of a migrated inbound. Tags and
// remarks are left empty: the create path generates the tag and the operator
// sees the row by protocol or id.
func legacyInbound(protocol model.Protocol, enable bool, listen string, port int, clients []model.Client, block map[string]any) *model.Inbound {
	settings, err := json.MarshalIndent(map[string]any{
		"clients": clients,
		"sidecar": block,
	}, "", "  ")
	if err != nil {
		settings = []byte("{}")
	}
	return &model.Inbound{
		UserId:   1,
		Enable:   enable,
		Listen:   listen,
		Port:     port,
		Protocol: protocol,
		Settings: string(settings),
	}
}

// splitLegacyListen splits a legacy host:port. Anything unparsable falls back to
// the wildcard address and the protocol's default port.
func splitLegacyListen(addr string, fallback int) (string, int) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "0.0.0.0", fallback
	}
	port, convErr := strconv.Atoi(portText)
	if convErr != nil || port <= 0 {
		return "0.0.0.0", fallback
	}
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	return host, port
}

func defaultInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func defaultString(v, fallback string) string {
	if trimmed := strings.TrimSpace(v); trimmed != "" {
		return trimmed
	}
	return fallback
}
