package sidecar

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Normalize rewrites an inbound's settings JSON, filling the defaults the
// external cores need, and returns the new settings string.
func Normalize(ib *model.Inbound) (string, error) {
	if ib == nil || !Supported(ib.Protocol) {
		return "", errNotSidecarInbound
	}
	name := DisplayName(ib.Protocol)
	root, err := normalizeRoot(ib.Settings)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	sidecar, err := normalizeSidecarMap(root["sidecar"], name)
	if err != nil {
		return "", err
	}

	switch ib.Protocol {
	case model.QWDTT:
		if err := normalizeQWDTT(ib, sidecar); err != nil {
			return "", err
		}
	case model.OLCRTC:
		if err := normalizeOLCRTC(sidecar, name); err != nil {
			return "", err
		}
	case model.CSQTT:
		if err := normalizeCSQTT(ib, sidecar); err != nil {
			return "", err
		}
	}

	raw, err := json.Marshal(sidecar)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	root["sidecar"] = raw
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

func normalizeRoot(settings string) (map[string]json.RawMessage, error) {
	if strings.TrimSpace(settings) == "" {
		return map[string]json.RawMessage{}, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settings), &root); err != nil {
		return nil, fmt.Errorf("invalid settings: %w", err)
	}
	if root == nil {
		return nil, fmt.Errorf("settings must be a JSON object")
	}
	return root, nil
}

func normalizeSidecarMap(raw json.RawMessage, name string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var sidecar map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return nil, fmt.Errorf("%s: invalid sidecar settings: %w", name, err)
	}
	if sidecar == nil {
		return map[string]json.RawMessage{}, nil
	}
	return sidecar, nil
}

// setDefault applies the insertion rule: absent, null or a whitespace-only
// string is replaced; every other stored value is kept untouched.
func setDefault(sidecar map[string]json.RawMessage, key string, value any) {
	if raw, ok := sidecar[key]; ok {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed != "" && trimmed != "null" {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return
			}
			if strings.TrimSpace(text) != "" {
				return
			}
		}
	}
	sidecar[key] = mustRaw(value)
}

func mustRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}

func rawString(sidecar map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := sidecar[key]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

func rawInt(sidecar map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := sidecar[key]
	if !ok {
		return 0, false
	}
	var number int
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}
	return number, true
}

func rawBool(sidecar map[string]json.RawMessage, key string) (bool, bool) {
	raw, ok := sidecar[key]
	if !ok {
		return false, false
	}
	var flag bool
	if err := json.Unmarshal(raw, &flag); err != nil {
		return false, false
	}
	return flag, true
}

func normalizeQWDTT(ib *model.Inbound, sidecar map[string]json.RawMessage) error {
	name := DisplayName(ib.Protocol)
	setDefault(sidecar, "wgPort", DefaultQWDTTWGPort)
	setDefault(sidecar, "rawPort", DefaultQWDTTRawPort)
	setDefault(sidecar, "listenRaw", "0.0.0.0:56003")
	setDefault(sidecar, "workers", 16)
	setDefault(sidecar, "clientPort", 9000)
	setDefault(sidecar, "mainPassword", randomPassword())
	setDefault(sidecar, "dns", "1.1.1.1")
	setDefault(sidecar, "routeThroughXray", true)

	if dns, ok := rawString(sidecar, "dns"); ok {
		if token := firstToken(dns); token != "" {
			sidecar["dns"] = mustRaw(token)
		}
	}

	canonical, err := canonicalListenAddr(ib, sidecar, DefaultQWDTTListenAddr, name)
	if err != nil {
		return err
	}
	sidecar["listenAddr"] = canonical

	if raw, ok := rawString(sidecar, "listenRaw"); ok {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			if _, port, err := net.SplitHostPort(trimmed); err == nil {
				if parsed, convErr := strconv.Atoi(port); convErr == nil && parsed > 0 {
					sidecar["rawPort"] = mustRaw(parsed)
				}
			}
		}
	}
	return nil
}

func normalizeCSQTT(ib *model.Inbound, sidecar map[string]json.RawMessage) error {
	name := DisplayName(ib.Protocol)
	setDefault(sidecar, "webPort", DefaultCSQTTWebPort)
	setDefault(sidecar, "mainPassword", randomPassword())
	setDefault(sidecar, "webUser", "ex3ui")
	setDefault(sidecar, "webPass", randomPassword())
	setDefault(sidecar, "dns", "77.88.8.8,77.88.8.1")
	setDefault(sidecar, "routeThroughXray", true)

	canonical, err := canonicalListenAddr(ib, sidecar, DefaultCSQTTListenAddr, name)
	if err != nil {
		return err
	}
	sidecar["listenAddr"] = canonical
	sidecar["secureCookie"] = mustRaw(false)
	return nil
}

func normalizeOLCRTC(sidecar map[string]json.RawMessage, name string) error {
	setDefault(sidecar, "provider", "jitsi")
	setDefault(sidecar, "transport", "datachannel")
	setDefault(sidecar, "vp8Fps", 60)
	setDefault(sidecar, "vp8Batch", 64)
	setDefault(sidecar, "seiFps", 30)
	setDefault(sidecar, "seiBatch", 64)
	setDefault(sidecar, "seiFrag", 900)
	setDefault(sidecar, "seiAck", 2000)
	setDefault(sidecar, "videoW", 1080)
	setDefault(sidecar, "videoH", 1080)
	setDefault(sidecar, "videoFps", 30)
	setDefault(sidecar, "videoCodec", "qrcode")
	setDefault(sidecar, "dns", "8.8.8.8:53")
	setDefault(sidecar, "routeThroughXray", true)

	if key, ok := rawString(sidecar, "cryptoKey"); !ok || strings.TrimSpace(key) == "" {
		generated, err := randomHex(32)
		if err != nil {
			return fmt.Errorf("%s: generate crypto key: %w", name, err)
		}
		sidecar["cryptoKey"] = mustRaw(generated)
	}

	if enabled, _ := rawBool(sidecar, "routeThroughXray"); enabled {
		if port, ok := rawInt(sidecar, "routeXrayPort"); !ok || port <= 0 {
			if allocated, err := freeLoopbackPort(); err == nil {
				sidecar["routeXrayPort"] = mustRaw(allocated)
			}
		}
	}
	return nil
}

// canonicalListenAddr validates listenAddr and writes the effective endpoint
// back onto the inbound record.
func canonicalListenAddr(ib *model.Inbound, sidecar map[string]json.RawMessage, fallback, name string) (json.RawMessage, error) {
	value, _ := rawString(sidecar, "listenAddr")
	value = strings.TrimSpace(value)
	if value == "" {
		if ib.Port > 0 {
			host := strings.TrimSpace(ib.Listen)
			if host == "" {
				host = "0.0.0.0"
			}
			value = net.JoinHostPort(host, strconv.Itoa(ib.Port))
		} else {
			value = fallback
		}
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return nil, fmt.Errorf("%s: listener must be host:port", name)
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		return nil, fmt.Errorf("%s: listener port must be between 1 and 65535", name)
	}
	host = trimBrackets(host)
	if host == "" {
		host = strings.TrimSpace(ib.Listen)
		if host == "" {
			host = "0.0.0.0"
		}
	}
	ib.Listen = host
	ib.Port = parsed
	return mustRaw(net.JoinHostPort(host, strconv.Itoa(parsed))), nil
}

func randomPassword() string {
	data := make([]byte, 12)
	if _, err := crand.Read(data); err != nil {
		return strings.Repeat("0", 16)
	}
	return hex.EncodeToString(data)
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := crand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func freeLoopbackPort() (int, error) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %v", listener.Addr())
	}
	return addr.Port, nil
}
