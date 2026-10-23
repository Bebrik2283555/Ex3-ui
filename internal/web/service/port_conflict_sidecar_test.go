package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// qWDTT's WireGuard listener is a UDP socket separate from the inbound port.
func TestCheckPortConflict_SidecarWGPort(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "udp-56001", "0.0.0.0", 56001, model.Hysteria, ``, `{}`)

	ib := &model.Inbound{
		Tag:      "qwdtt-56000",
		Listen:   "0.0.0.0",
		Port:     56000,
		Protocol: model.QWDTT,
		Settings: `{"clients":[],"sidecar":{"wgPort":56001,` +
			`"rawPort":56003,"listenRaw":"127.0.0.1:56003"}}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(ib, 0)
	if err != nil {
		t.Fatalf("checkPortConflict: %v", err)
	}
	if conflict == nil || conflict.CandidateRole != "wgPort" || conflict.Port != 56001 {
		t.Fatalf("conflict = %+v, want the qWDTT wgPort collision", conflict)
	}
}

// CSQTT's HTTPS admin listener is a TCP socket separate from the peer port.
func TestCheckPortConflict_SidecarWebPort(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "tcp-46002", "0.0.0.0", 46002, model.VLESS, `{"network":"tcp"}`, `{}`)

	ib := &model.Inbound{
		Tag:      "csqtt-46000",
		Listen:   "0.0.0.0",
		Port:     46000,
		Protocol: model.CSQTT,
		Settings: `{"clients":[],"sidecar":{"webPort":46002}}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(ib, 0)
	if err != nil {
		t.Fatalf("checkPortConflict: %v", err)
	}
	if conflict == nil || conflict.CandidateRole != "webPort" || conflict.Port != 46002 {
		t.Fatalf("conflict = %+v, want the CSQTT webPort collision", conflict)
	}
}

// The reverse direction: an ordinary inbound must not take a sidecar's web port.
func TestCheckPortConflict_MainPortVsExistingWebPort(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "csqtt-46000", "0.0.0.0", 46000, model.CSQTT,
		``, `{"clients":[],"sidecar":{"webPort":46002}}`)

	ib := &model.Inbound{
		Tag:            "vless-46002",
		Listen:         "0.0.0.0",
		Port:           46002,
		Protocol:       model.VLESS,
		StreamSettings: `{"network":"tcp"}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(ib, 0)
	if err != nil {
		t.Fatalf("checkPortConflict: %v", err)
	}
	if conflict == nil || conflict.Role != "webPort" || conflict.InboundID == 0 {
		t.Fatalf("conflict = %+v, want the existing CSQTT webPort", conflict)
	}
}

// olcRTC's loopback SOCKS bridge is injected by Xray; it must not reuse an
// inbound port, otherwise Xray logs a warning and silently skips the bridge.
func TestCheckPortConflict_OLCRTCBridgePort(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "vless-51000", "0.0.0.0", 51000, model.VLESS, `{"network":"tcp"}`, `{}`)

	ib := &model.Inbound{
		Tag:      "olcrtc-bridge",
		Listen:   "0.0.0.0",
		Port:     52000,
		Protocol: model.OLCRTC,
		Settings: `{"clients":[],"sidecar":{"routeThroughXray":true,"routeXrayPort":51000}}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(ib, 0)
	if err != nil {
		t.Fatalf("checkPortConflict: %v", err)
	}
	if conflict == nil || conflict.CandidateRole != "routeXrayPort" || conflict.Port != 51000 {
		t.Fatalf("conflict = %+v, want the olcRTC bridge collision", conflict)
	}
}

// A routed mtproto inbound must not put its loopback bridge on its own public
// listener port.
func TestCheckPortConflict_MTProtoBridgeOnOwnPort(t *testing.T) {
	setupConflictDB(t)
	ib := &model.Inbound{
		Tag:      "mtproto-443",
		Listen:   "0.0.0.0",
		Port:     443,
		Protocol: model.MTProto,
		Settings: `{"clients":[],"routeThroughXray":true,"routeXrayPort":443}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(ib, 0)
	if err == nil {
		t.Fatalf("checkPortConflict = %+v, want a self-conflict error", conflict)
	}
}
