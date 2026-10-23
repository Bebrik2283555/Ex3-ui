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

const sidecarMigrationMarker = "sidecarMigratedV1"

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
		VkHashes string `json:"vkHashes"`
		Enabled  bool   `json:"enabled"`
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

// MigrateExtraServices converts the pre-1.4.2 global qWDTT/olcRTC settings
// into regular sidecar inbounds. Existing rows are left untouched, so a
// half-finished migration can be retried safely.
func MigrateExtraServices() {
	settingService := SettingService{}
	if marker, _ := settingService.GetRawSetting(sidecarMigrationMarker); marker == "true" {
		return
	}
	inboundService := &InboundService{}
	ok := true

	if raw, _ := settingService.GetRawSetting("extraService_qwdtt"); strings.TrimSpace(raw) != "" {
		var legacy legacyQWDTT
		if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
			logger.Warning("sidecar migration: qWDTT config is invalid:", err)
			ok = false
		} else if !sidecarInboundExists(model.QWDTT) {
			if _, _, err := inboundService.AddInbound(buildQWDTTInbound(&legacy)); err != nil {
				logger.Warning("sidecar migration: qWDTT:", err)
				ok = false
			} else {
				logger.Info("sidecar migration: qWDTT moved into an inbound")
			}
		}
	}

	if raw, _ := settingService.GetRawSetting("extraService_olcrtc"); strings.TrimSpace(raw) != "" {
		var legacy legacyOLCRTC
		if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
			logger.Warning("sidecar migration: olcRTC config is invalid:", err)
			ok = false
		} else if !sidecarInboundExists(model.OLCRTC) {
			if _, _, err := inboundService.AddInbound(buildOLCRTCInbound(&legacy)); err != nil {
				logger.Warning("sidecar migration: olcRTC:", err)
				ok = false
			} else {
				logger.Info("sidecar migration: olcRTC moved into an inbound")
			}
		}
	}

	if ok {
		_ = settingService.SetRawSetting(sidecarMigrationMarker, "true")
	}
}

func sidecarInboundExists(protocol model.Protocol) bool {
	var count int64
	if err := database.GetDB().Model(model.Inbound{}).Where("protocol = ?", protocol).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func buildQWDTTInbound(legacy *legacyQWDTT) *model.Inbound {
	host, port := splitLegacyListen(legacy.ListenAddr, 56000)
	settings := map[string]any{
		"listenAddr":       net.JoinHostPort(host, strconv.Itoa(port)),
		"wgPort":           defaultInt(legacy.WGPort, 56001),
		"listenRaw":        legacy.ListenRaw,
		"mainPassword":     legacy.Password,
		"dns":              legacy.DNS,
		"workers":          16,
		"hashes":           legacy.VkHashes,
		"routeThroughXray": true,
	}
	if strings.TrimSpace(legacy.ConfigDir) != "" {
		settings["configDir"] = legacy.ConfigDir
	}
	hashesByEmail := map[string]string{}
	var clients []model.Client
	for i, cl := range legacy.Clients {
		name := strings.TrimSpace(cl.Name)
		if name == "" {
			name = fmt.Sprintf("client-%d", i+1)
		}
		password := strings.TrimSpace(cl.Password)
		if password == "" {
			continue
		}
		clients = append(clients, model.Client{
			Email:    name,
			Password: password,
			Enable:   cl.Enabled,
			SubID:    uuid.NewString(),
		})
		if strings.TrimSpace(cl.VkHashes) != "" {
			hashesByEmail[name] = cl.VkHashes
		}
	}
	if len(hashesByEmail) > 0 {
		settings["hashesByEmail"] = hashesByEmail
	}
	body := map[string]any{"clients": clients, "sidecar": settings}
	raw, _ := json.MarshalIndent(body, "", "  ")
	return &model.Inbound{
		UserId:   1,
		Protocol: model.QWDTT,
		Listen:   host,
		Port:     port,
		Enable:   legacy.Enabled,
		Settings: string(raw),
	}
}

func buildOLCRTCInbound(legacy *legacyOLCRTC) *model.Inbound {
	settings := map[string]any{
		"provider":  defaultString(legacy.Provider, "jitsi"),
		"roomId":    legacy.RoomID,
		"cryptoKey": legacy.CryptoKey,
		"transport": defaultString(legacy.Transport, "datachannel"),
		"vp8Fps":    defaultInt(legacy.VP8Fps, 30),
		"vp8Batch":  defaultInt(legacy.VP8Batch, 64),
		"dns":       legacy.DNS,
		"dataDir":   legacy.DataDir,
		"debug":     legacy.Debug,
	}
	if strings.TrimSpace(legacy.ConfigFile) != "" {
		settings["configFile"] = legacy.ConfigFile
	}
	// olcRTC has no per-client credential: one synthetic client carries the
	// share link so the connection appears in subscriptions like any other.
	clients := []model.Client{{
		Email:    "default",
		Password: uuid.NewString(),
		Enable:   true,
		SubID:    uuid.NewString(),
	}}
	body := map[string]any{"clients": clients, "sidecar": settings}
	raw, _ := json.MarshalIndent(body, "", "  ")
	return &model.Inbound{
		UserId:   1,
		Protocol: model.OLCRTC,
		Listen:   "0.0.0.0",
		Port:     0,
		Enable:   legacy.Enabled,
		Settings: string(raw),
	}
}

func splitLegacyListen(addr string, fallback int) (string, int) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "0.0.0.0", fallback
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		port = fallback
	}
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	return host, port
}

func defaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
