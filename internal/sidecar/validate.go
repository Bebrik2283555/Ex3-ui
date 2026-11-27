package sidecar

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Validate checks an inbound against the rules of its sidecar protocol.
func Validate(ib *model.Inbound) error {
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		return err
	}
	name := DisplayName(ib.Protocol)
	if ib.NodeID != nil {
		return fmt.Errorf("%s: available only on the local panel node", name)
	}

	switch ib.Protocol {
	case model.QWDTT:
		if err := validateQWDTT(inst, name); err != nil {
			return err
		}
	case model.OLCRTC:
		if err := validateOLCRTC(inst, name); err != nil {
			return err
		}
	case model.CSQTT:
		if err := validateCSQTT(inst, name); err != nil {
			return err
		}
	}

	for _, client := range inst.Clients {
		if strings.TrimSpace(client.Email) == "" {
			return fmt.Errorf("%s: client email is required", name)
		}
	}
	return nil
}

func validateQWDTT(inst Instance, name string) error {
	cfg := inst.QWDTT
	if cfg == nil {
		return fmt.Errorf("%s: settings are missing", name)
	}
	if inst.Port < 1 || inst.Port > 65535 {
		return fmt.Errorf("%s: listen port is required", name)
	}
	if cfg.WGPort < 1 || cfg.WGPort > 65535 {
		return fmt.Errorf("%s: wg port must be between 1 and 65535", name)
	}
	if cfg.RawPort > 0 {
		if cfg.RawPort > 65535 || cfg.RawPort == inst.Port || cfg.RawPort == cfg.WGPort {
			return fmt.Errorf("%s: raw port conflicts with another qWDTT port", name)
		}
	}
	if cfg.WGPort == inst.Port {
		return fmt.Errorf("%s: wg port must differ from the listen port", name)
	}
	return nil
}

var saluteJazzRoomRE = regexp.MustCompile(`^[a-z0-9]{4,16}:[a-z0-9]{4,16}$`)

func validateOLCRTC(inst Instance, name string) error {
	cfg := inst.OLCRTC
	if cfg == nil {
		return fmt.Errorf("%s: settings are missing", name)
	}
	if strings.TrimSpace(cfg.RoomID) == "" {
		return fmt.Errorf("%s: room id is required", name)
	}
	switch cfg.Provider {
	case "jitsi", "telemost", "wbstream", "salutejazz", "vkcalls", "mtslink", "max":
	default:
		return fmt.Errorf("%s: provider must be one of jitsi, telemost, wbstream, salutejazz, vkcalls, mtslink, max", name)
	}
	if cfg.Provider == "max" && strings.TrimSpace(cfg.AuthToken) == "" {
		return fmt.Errorf("%s: max requires auth token", name)
	}
	switch cfg.Transport {
	case "datachannel", "vp8channel", "seichannel", "videochannel":
	default:
		return fmt.Errorf("%s: transport must be one of datachannel, vp8channel, seichannel, videochannel", name)
	}
	if err := validateOLCRTCTransport(cfg, name); err != nil {
		return err
	}
	if err := validateOLCRTCRoomID(cfg.Provider, cfg.RoomID, name); err != nil {
		return err
	}
	if cfg.Transport == "videochannel" {
		switch cfg.VideoCodec {
		case "", "qrcode", "tile":
		default:
			return fmt.Errorf("%s: video codec must be one of qrcode, tile", name)
		}
	}
	if len(cfg.CryptoKey) != 64 {
		return fmt.Errorf("%s: crypto key must be 64 hex characters", name)
	}
	if _, err := hex.DecodeString(cfg.CryptoKey); err != nil {
		return fmt.Errorf("%s: crypto key must be 64 hex characters", name)
	}
	if _, _, err := net.SplitHostPort(cfg.DNS); err != nil {
		return fmt.Errorf("%s: dns must be host:port", name)
	}
	if raw := strings.TrimSpace(cfg.ICELocalIP); raw != "" {
		parsed, err := netip.ParseAddr(raw)
		addr := parsed.Unmap()
		if err != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() ||
			addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) || addr.IsLinkLocalUnicast() {
			return fmt.Errorf("%s: ice local ip must be one unicast IPv4 address", name)
		}
	}
	switch strings.TrimSpace(cfg.DTLSProfile) {
	case "", "off", "chrome-linux-138-compat-v1":
	default:
		return fmt.Errorf("%s: dtls profile must be off or chrome-linux-138-compat-v1", name)
	}
	return nil
}

func validateOLCRTCRoomID(provider, raw, name string) error {
	room := strings.TrimSpace(raw)
	if room == "" {
		return fmt.Errorf("%s: room id is required", name)
	}
	if strings.ContainsAny(room, "\r\n") {
		return fmt.Errorf("%s: room id must not contain newlines", name)
	}
	switch provider {
	case "vkcalls":
		u, err := url.Parse(room)
		if err != nil || u.Scheme != "https" || (u.Host != "vk.ru" && u.Host != "vk.com") ||
			u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s: vkcalls room id must be https://vk.ru/call/join/<id>", name)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "call" || parts[1] != "join" || parts[2] == "" {
			return fmt.Errorf("%s: vkcalls room id must be https://vk.ru/call/join/<id>", name)
		}
	case "salutejazz":
		if !saluteJazzRoomRE.MatchString(room) {
			return fmt.Errorf("%s: salutejazz room id must be <code>:<password> (lowercase alphanumeric)", name)
		}
	case "mtslink":
		u, err := url.Parse(room)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%s: mtslink room id must be an event link like https://my.mts-link.ru/j/<owner>/<event>", name)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		idx := -1
		for i, p := range parts {
			if p == "j" {
				idx = i
				break
			}
		}
		if idx < 0 || len(parts) < idx+3 || parts[idx+1] == "" || parts[idx+2] == "" {
			return fmt.Errorf("%s: mtslink room id must be an event link like https://my.mts-link.ru/j/<owner>/<event>", name)
		}
	case "jitsi":
		trimmed := strings.TrimPrefix(strings.TrimPrefix(room, "https://"), "http://")
		slash := strings.IndexByte(trimmed, '/')
		if slash <= 0 || strings.Trim(trimmed[slash+1:], "/") == "" {
			return fmt.Errorf("%s: jitsi room id must be host/room or https://host/room", name)
		}
	case "max":
		hash := room
		if idx := strings.LastIndex(hash, "/joincall/"); idx >= 0 {
			hash = hash[idx+len("/joincall/"):]
		}
		if j := strings.IndexAny(hash, "/?#"); j >= 0 {
			hash = hash[:j]
		}
		if hash == "" || strings.ContainsAny(hash, ":@ ") {
			return fmt.Errorf("%s: max room id must be a join link https://max.ru/joincall/<hash> or hash", name)
		}
	case "wbstream":
		if room == "any" {
			return fmt.Errorf("%s: room id is required", name)
		}
	}
	return nil
}

func validateOLCRTCTransport(cfg *OLCRTCConfig, name string) error {
	switch cfg.Transport {
	case "datachannel":
		switch cfg.Provider {
		case "jitsi", "salutejazz", "max":
		case "wbstream":
			if strings.TrimSpace(cfg.AuthToken) == "" {
				return fmt.Errorf("%s: wbstream datachannel requires auth token", name)
			}
		default:
			return fmt.Errorf("%s: transport datachannel is not supported by provider %s", name, cfg.Provider)
		}
	case "vp8channel":
		switch cfg.Provider {
		case "jitsi", "telemost", "wbstream", "vkcalls", "mtslink":
		default:
			return fmt.Errorf("%s: transport vp8channel is not supported by provider %s", name, cfg.Provider)
		}
	case "seichannel":
		if cfg.Provider != "jitsi" && cfg.Provider != "wbstream" {
			return fmt.Errorf("%s: transport seichannel is not supported by provider %s", name, cfg.Provider)
		}
	case "videochannel":
		switch cfg.Provider {
		case "jitsi", "telemost", "wbstream":
		default:
			return fmt.Errorf("%s: transport videochannel is not supported by provider %s", name, cfg.Provider)
		}
	}
	return nil
}

func validateCSQTT(inst Instance, name string) error {
	cfg := inst.CSQTT
	if cfg == nil {
		return fmt.Errorf("%s: settings are missing", name)
	}
	if inst.Port < 1 || inst.Port > 65535 {
		return fmt.Errorf("%s: listen port is required", name)
	}
	if cfg.WebPort < 1 || cfg.WebPort > 65535 {
		return fmt.Errorf("%s: web port must be between 1 and 65535", name)
	}
	if cfg.WebPort == inst.Port {
		return fmt.Errorf("%s: web port must differ from the listen port", name)
	}
	return nil
}
