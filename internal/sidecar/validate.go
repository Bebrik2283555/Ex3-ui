package sidecar

import (
	"encoding/hex"
	"fmt"
	"net"
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

func validateOLCRTC(inst Instance, name string) error {
	cfg := inst.OLCRTC
	if cfg == nil {
		return fmt.Errorf("%s: settings are missing", name)
	}
	if strings.TrimSpace(cfg.RoomID) == "" {
		return fmt.Errorf("%s: room id is required", name)
	}
	switch cfg.Provider {
	case "jitsi", "telemost", "wbstream":
	default:
		return fmt.Errorf("%s: provider must be one of jitsi, telemost, wbstream", name)
	}
	switch cfg.Transport {
	case "datachannel", "vp8channel", "seichannel", "videochannel":
	default:
		return fmt.Errorf("%s: transport must be one of datachannel, vp8channel, seichannel, videochannel", name)
	}
	if err := validateOLCRTCTransport(cfg, name); err != nil {
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
	return nil
}

func validateOLCRTCTransport(cfg *OLCRTCConfig, name string) error {
	switch cfg.Transport {
	case "datachannel":
		if cfg.Provider != "jitsi" {
			return fmt.Errorf("%s: transport datachannel is not supported by provider %s", name, cfg.Provider)
		}
	case "seichannel":
		if cfg.Provider != "jitsi" && cfg.Provider != "wbstream" {
			return fmt.Errorf("%s: transport seichannel is not supported by provider %s", name, cfg.Provider)
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
