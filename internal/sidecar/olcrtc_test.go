package sidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const testHexKey64 = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// Validates the Ghostlane carrier/transport compatibility matrix and new
// token, ICE local IP and DTLS profile guards enforced before launch.
func TestValidateOLCRTCGhostlaneMatrix(t *testing.T) {
	base := func(provider, transport string, mutate func(*OLCRTCConfig)) *model.Inbound {
		cfg := OLCRTCConfig{
			Provider:  provider,
			Transport: transport,
			RoomID:    "https://meet.jit.si/test-room",
			CryptoKey: testHexKey64,
			DNS:       "8.8.8.8:53",
		}
		if mutate != nil {
			mutate(&cfg)
		}
		raw, err := json.Marshal(struct {
			Sidecar OLCRTCConfig `json:"sidecar"`
		}{Sidecar: cfg})
		if err != nil {
			t.Fatal(err)
		}
		return &model.Inbound{Id: 11, Protocol: model.OLCRTC, Settings: string(raw)}
	}

	validCases := []struct {
		name      string
		provider  string
		transport string
		mutate    func(*OLCRTCConfig)
	}{
		{"jitsi datachannel", "jitsi", "datachannel", nil},
		{"jitsi vp8channel", "jitsi", "vp8channel", nil},
		{"jitsi seichannel", "jitsi", "seichannel", nil},
		{"jitsi videochannel tile", "jitsi", "videochannel", func(c *OLCRTCConfig) { c.VideoCodec = "tile" }},
		{"telemost vp8channel", "telemost", "vp8channel", nil},
		{"telemost videochannel", "telemost", "videochannel", nil},
		{"wbstream vp8channel", "wbstream", "vp8channel", nil},
		{"wbstream seichannel", "wbstream", "seichannel", nil},
		{"wbstream videochannel", "wbstream", "videochannel", nil},
		{"wbstream datachannel with token", "wbstream", "datachannel", func(c *OLCRTCConfig) { c.AuthToken = "mod-token" }},
		{"salutejazz datachannel", "salutejazz", "datachannel", func(c *OLCRTCConfig) { c.RoomID = "abc123:pass" }},
		{"vkcalls vp8channel", "vkcalls", "vp8channel", func(c *OLCRTCConfig) { c.RoomID = "https://vk.ru/call/join/xyz" }},
		{"mtslink vp8channel", "mtslink", "vp8channel", func(c *OLCRTCConfig) { c.RoomID = "https://my.mts-link.ru/j/org/ev" }},
		{"max datachannel with token", "max", "datachannel", func(c *OLCRTCConfig) { c.AuthToken = "max-sess"; c.RoomID = "https://max.ru/joincall/abc" }},
		{"ice local ipv4 + dtls chrome138", "vkcalls", "vp8channel", func(c *OLCRTCConfig) {
			c.RoomID = "https://vk.ru/call/join/xyz"
			c.ICELocalIP = "198.51.100.10"
			c.DTLSProfile = "chrome-linux-138-compat-v1"
			c.UDPEnabled = true
		}},
		{"ice local ipv4-mapped + dtls off", "jitsi", "datachannel", func(c *OLCRTCConfig) {
			c.ICELocalIP = "::ffff:192.0.2.5"
			c.DTLSProfile = "off"
		}},
	}
	for _, tc := range validCases {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := Validate(base(tc.provider, tc.transport, tc.mutate)); err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}

	invalidCases := []struct {
		name      string
		provider  string
		transport string
		wantErr   string
		mutate    func(*OLCRTCConfig)
	}{
		{"unknown provider", "zoom", "datachannel", "provider must be one of", nil},
		{"unknown transport", "jitsi", "audio", "transport must be one of", nil},
		{"empty room", "jitsi", "datachannel", "room id is required", func(c *OLCRTCConfig) { c.RoomID = "   " }},
		{"telemost datachannel", "telemost", "datachannel", "not supported by provider telemost", nil},
		{"telemost seichannel", "telemost", "seichannel", "not supported by provider telemost", nil},
		{"wbstream datachannel without token", "wbstream", "datachannel", "wbstream datachannel requires auth token", nil},
		{"salutejazz vp8channel", "salutejazz", "vp8channel", "not supported by provider salutejazz", nil},
		{"salutejazz videochannel", "salutejazz", "videochannel", "not supported by provider salutejazz", nil},
		{"vkcalls datachannel", "vkcalls", "datachannel", "not supported by provider vkcalls", nil},
		{"vkcalls seichannel", "vkcalls", "seichannel", "not supported by provider vkcalls", nil},
		{"vkcalls videochannel", "vkcalls", "videochannel", "not supported by provider vkcalls", nil},
		{"mtslink datachannel", "mtslink", "datachannel", "not supported by provider mtslink", nil},
		{"mtslink videochannel", "mtslink", "videochannel", "not supported by provider mtslink", nil},
		{"max without token", "max", "datachannel", "max requires auth token", nil},
		{"max vp8channel with token", "max", "vp8channel", "not supported by provider max", func(c *OLCRTCConfig) { c.AuthToken = "tok" }},
		{"bad video codec", "jitsi", "videochannel", "video codec", func(c *OLCRTCConfig) { c.VideoCodec = "h265" }},
		{"short key", "jitsi", "datachannel", "64 hex", func(c *OLCRTCConfig) { c.CryptoKey = "abcd" }},
		{"non-hex key", "jitsi", "datachannel", "64 hex", func(c *OLCRTCConfig) { c.CryptoKey = strings.Repeat("z", 64) }},
		{"bad dns", "jitsi", "datachannel", "host:port", func(c *OLCRTCConfig) { c.DNS = "8.8.8.8" }},
		{"ice loopback", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "127.0.0.1" }},
		{"ice unspecified", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "0.0.0.0" }},
		{"ice multicast", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "224.0.0.1" }},
		{"ice link-local", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "169.254.1.1" }},
		{"ice broadcast", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "255.255.255.255" }},
		{"ice ipv6", "jitsi", "datachannel", "unicast IPv4", func(c *OLCRTCConfig) { c.ICELocalIP = "2001:db8::1" }},
		{"unknown dtls profile", "jitsi", "datachannel", "dtls profile", func(c *OLCRTCConfig) { c.DTLSProfile = "firefox-1" }},
		{"vkcalls bare hash", "vkcalls", "vp8channel", "https://vk.ru/call/join/", func(c *OLCRTCConfig) { c.RoomID = "clmq6eant_barehash" }},
		{"vkcalls query string", "vkcalls", "vp8channel", "https://vk.ru/call/join/", func(c *OLCRTCConfig) { c.RoomID = "https://vk.ru/call/join/xyz?foo=1" }},
		{"salutejazz invalid format", "salutejazz", "datachannel", "<code>:<password>", func(c *OLCRTCConfig) { c.RoomID = "ABC:pass" }},
		{"mtslink missing j path", "mtslink", "vp8channel", "/j/<owner>/<event>", func(c *OLCRTCConfig) { c.RoomID = "https://my.mts-link.ru/room1" }},
		{"jitsi missing room", "jitsi", "datachannel", "host/room", func(c *OLCRTCConfig) { c.RoomID = "meet.jit.si" }},
		{"max invalid room", "max", "datachannel", "join link", func(c *OLCRTCConfig) { c.AuthToken = "tok"; c.RoomID = "bad:room" }},
		{"wbstream any room", "wbstream", "vp8channel", "room id is required", func(c *OLCRTCConfig) { c.RoomID = "any" }},
	}
	for _, tc := range invalidCases {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			err := Validate(base(tc.provider, tc.transport, tc.mutate))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateQWDTTCSQTTAndCommonGuards(t *testing.T) {
	remote := 3
	cases := []struct {
		name    string
		ib      *model.Inbound
		wantErr string
	}{
		{
			name:    "node inbound rejected",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 56000, NodeID: &remote, Settings: `{}`},
			wantErr: "local panel node",
		},
		{
			name:    "qwdtt port required",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 0, Settings: `{"sidecar":{"wgPort":56001}}`},
			wantErr: "listen port is required",
		},
		{
			name:    "qwdtt invalid wgPort",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 56000, Settings: `{"sidecar":{"wgPort":70000}}`},
			wantErr: "wg port must be between",
		},
		{
			name:    "qwdtt wgPort collides with port",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 56001, Settings: `{"sidecar":{"wgPort":56001}}`},
			wantErr: "wg port must differ",
		},
		{
			name:    "qwdtt rawPort collides with port",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 56000, Settings: `{"sidecar":{"wgPort":56001,"rawPort":56000}}`},
			wantErr: "raw port conflicts",
		},
		{
			name:    "csqtt port required",
			ib:      &model.Inbound{Protocol: model.CSQTT, Port: 0, Settings: `{"sidecar":{"webPort":46002}}`},
			wantErr: "listen port is required",
		},
		{
			name:    "csqtt webPort collides",
			ib:      &model.Inbound{Protocol: model.CSQTT, Port: 46002, Settings: `{"sidecar":{"webPort":46002}}`},
			wantErr: "web port must differ",
		},
		{
			name:    "csqtt invalid webPort",
			ib:      &model.Inbound{Protocol: model.CSQTT, Port: 46000, Settings: `{"sidecar":{"webPort":99999}}`},
			wantErr: "web port must be between",
		},
		{
			name:    "missing client email",
			ib:      &model.Inbound{Protocol: model.CSQTT, Port: 46000, Settings: `{"clients":[{"enable":true,"csqttPassword":"p"}],"sidecar":{"webPort":46002}}`},
			wantErr: "client email is required",
		},
		{
			name:    "valid qwdtt",
			ib:      &model.Inbound{Protocol: model.QWDTT, Port: 56000, Settings: `{"clients":[{"email":"u@x","enable":true}],"sidecar":{"wgPort":56001,"rawPort":56003}}`},
			wantErr: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.ib)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("Validate() err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeSidecarsAndErrors(t *testing.T) {
	if _, err := Normalize(nil); err == nil {
		t.Fatal("Normalize(nil) succeeded")
	}
	if _, err := Normalize(&model.Inbound{Protocol: model.VLESS, Settings: `{}`}); err == nil {
		t.Fatal("Normalize(vless) succeeded")
	}
	if _, err := Normalize(&model.Inbound{Protocol: model.OLCRTC, Settings: `{bad`}); err == nil {
		t.Fatal("Normalize(bad json) succeeded")
	}
	if _, err := Normalize(&model.Inbound{Protocol: model.OLCRTC, Settings: `null`}); err == nil {
		t.Fatal("Normalize(null root) succeeded")
	}
	if _, err := Normalize(&model.Inbound{Protocol: model.OLCRTC, Settings: `{"sidecar":123}`}); err == nil {
		t.Fatal("Normalize(non-object sidecar) succeeded")
	}

	// Omitting transport on vp8-only carrier picks vp8channel; cryptoKey + routeXrayPort auto-fill.
	vkIb := &model.Inbound{
		Id:       4,
		Protocol: model.OLCRTC,
		Settings: `{"sidecar":{"provider":"vkcalls","roomId":" https://vk.ru/call/join/1 ","authToken":" tok ","iceLocalIp":" 192.0.2.7 "}}`,
	}
	normalized, err := Normalize(vkIb)
	if err != nil {
		t.Fatalf("Normalize(vkcalls): %v", err)
	}
	var parsed struct {
		Sidecar OLCRTCConfig `json:"sidecar"`
	}
	if err := json.Unmarshal([]byte(normalized), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Sidecar.Transport != "vp8channel" {
		t.Fatalf("vkcalls default transport = %q, want vp8channel", parsed.Sidecar.Transport)
	}
	if len(parsed.Sidecar.CryptoKey) != 64 || !parsed.Sidecar.RouteThroughXray || parsed.Sidecar.RouteXrayPort <= 0 {
		t.Fatalf("missing auto-generated key/bridge: %+v", parsed.Sidecar)
	}
	if parsed.Sidecar.RoomID != "https://vk.ru/call/join/1" || parsed.Sidecar.AuthToken != "tok" || parsed.Sidecar.ICELocalIP != "192.0.2.7" {
		t.Fatalf("strings were not trimmed: %+v", parsed.Sidecar)
	}

	// CSQTT sets defaults and forces secureCookie=false for local child API compatibility.
	csqttIb := &model.Inbound{Id: 5, Protocol: model.CSQTT, Port: 46123, Settings: `{"sidecar":{"secureCookie":true}}`}
	out, err := Normalize(csqttIb)
	if err != nil {
		t.Fatalf("Normalize(csqtt): %v", err)
	}
	var csqttParsed struct {
		Sidecar CSQTTConfig `json:"sidecar"`
	}
	if err := json.Unmarshal([]byte(out), &csqttParsed); err != nil {
		t.Fatal(err)
	}
	if csqttParsed.Sidecar.SecureCookie || csqttParsed.Sidecar.WebUser != "ex3ui" || csqttParsed.Sidecar.ListenAddr != "0.0.0.0:46123" {
		t.Fatalf("csqtt normalize = %+v", csqttParsed.Sidecar)
	}

	// Invalid listener format is rejected.
	badListen := &model.Inbound{Protocol: model.QWDTT, Settings: `{"sidecar":{"listenAddr":"invalid"}}`}
	if _, err := Normalize(badListen); err == nil {
		t.Fatal("expected listenAddr format error")
	}
	badPort := &model.Inbound{Protocol: model.QWDTT, Settings: `{"sidecar":{"listenAddr":"0.0.0.0:99999"}}`}
	if _, err := Normalize(badPort); err == nil {
		t.Fatal("expected listenAddr port error")
	}
}

func TestPrepareOLCRTCYAMLAndDictionaries(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	customNames := filepath.Join(dataDir, olcrtcNamesName)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customNames, []byte("custom-alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "nested", "server.yaml")
	inst := Instance{
		Protocol:         model.OLCRTC,
		RouteThroughXray: true,
		RouteXrayPort:    49123,
		OLCRTC: &OLCRTCConfig{
			Provider:         "mtslink",
			AuthToken:        "tok\"1",
			RoomID:           "https://my.mts-link.ru/j/org/ev?ice=both",
			CryptoKey:        testHexKey64,
			Transport:        "vp8channel",
			VP8Fps:           200,
			VP8Batch:         0,
			DNS:              "1.1.1.1:53",
			ICELocalIP:       "192.0.2.42",
			DTLSProfile:      "chrome-linux-138-compat-v1",
			UDPEnabled:       true,
			DataDir:          dataDir,
			ConfigFile:       cfgPath,
			Debug:            true,
			RouteThroughXray: true,
			RouteXrayPort:    49123,
		},
	}
	if err := inst.Prepare(); err != nil {
		t.Fatalf("Prepare(): %v", err)
	}
	yamlBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	yamlText := string(yamlBytes)
	for _, want := range []string{
		"mode: srv",
		"provider: \"mtslink\"",
		"token: \"tok\\\"1\"",
		"id: \"https://my.mts-link.ru/j/org/ev?ice=both\"",
		"transport: \"vp8channel\"",
		"ice_local_ip: \"192.0.2.42\"",
		"profile: \"chrome-linux-138-compat-v1\"",
		"udp:\n  enabled: true",
		"fps: 120",
		"batch_size: 1",
		"proxy_port: 49123",
		"debug: true",
	} {
		if !strings.Contains(yamlText, want) {
			t.Fatalf("server.yaml missing %q:\n%s", want, yamlText)
		}
	}

	// Custom names file must be preserved while missing surnames gets seeded.
	gotNames, _ := os.ReadFile(customNames)
	if string(gotNames) != "custom-alice\n" {
		t.Fatalf("names overwritten: %q", string(gotNames))
	}
	if _, err := os.Stat(filepath.Join(dataDir, olcrtcSurnamesName)); err != nil {
		t.Fatalf("surnames not seeded: %v", err)
	}

	// Also verify seichannel + videochannel YAML formatting and path traversal guard.
	seiYAML := olcrtcYAML(&OLCRTCConfig{Provider: "wbstream", Transport: "seichannel", SeiFps: 30, SeiBatch: 16, SeiFrag: 900, SeiAck: 1500})
	if !strings.Contains(seiYAML, "fragment_size: 900") || !strings.Contains(seiYAML, "ack_timeout_ms: 1500") {
		t.Fatalf("sei YAML unexpected:\n%s", seiYAML)
	}
	vidYAML := olcrtcYAML(&OLCRTCConfig{Provider: "telemost", Transport: "videochannel", VideoW: 1280, VideoH: 720, VideoFps: 30, VideoCodec: "tile"})
	if !strings.Contains(vidYAML, "codec: \"tile\"") || !strings.Contains(vidYAML, "width: 1280") {
		t.Fatalf("video YAML unexpected:\n%s", vidYAML)
	}
	bad := Instance{Protocol: model.OLCRTC, OLCRTC: &OLCRTCConfig{ConfigFile: dir + "/../evil.yaml"}}
	if err := bad.Prepare(); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestOLCRTCLinksAndHelpers(t *testing.T) {
	settings := `{
		"clients": [
			{"email": "Alice@Example.com", "password": "pw1", "enable": true},
			{"email": "disabled@example.com", "password": "pw2", "enable": false}
		],
		"sidecar": {
			"provider": "vkcalls",
			"authToken": "secret-should-not-leak",
			"roomId": "https://vk.ru/call/join/room1",
			"cryptoKey": "` + testHexKey64 + `",
			"transport": "vp8channel",
			"vp8Fps": 45,
			"vp8Batch": 32,
			"iceLocalIp": "192.0.2.1",
			"dtlsProfile": "chrome-linux-138-compat-v1",
			"udpEnabled": true
		}
	}`
	ib := &model.Inbound{Id: 12, Remark: "vk-main", Protocol: model.OLCRTC, Settings: settings}
	links := LinkFor(ib, "alice@example.com")
	want := "olcrtc://vkcalls?vp8channel<vp8-fps=45&vp8-batch=32>@https://vk.ru/call/join/room1#" + testHexKey64 + "$vk-main"
	if !slices.Equal(links, []string{want}) {
		t.Fatalf("LinkFor = %v, want [%s]", links, want)
	}
	if strings.Contains(links[0], "secret-should-not-leak") {
		t.Fatalf("share link leaked authToken: %s", links[0])
	}
	if got := (Instance{Protocol: model.OLCRTC, Remark: "my room $ #1", OLCRTC: &OLCRTCConfig{
		Provider: "jitsi", Transport: "datachannel", RoomID: "https://meet.jit.si/r", CryptoKey: testHexKey64,
	}}).Links(model.Client{Email: "u@x", Enable: true}); len(got) != 1 || !strings.HasSuffix(got[0], "#"+testHexKey64+"$my%20room%20%24%20%231") {
		t.Fatalf("encoded remark link = %v", got)
	}
	if got := LinkFor(ib, "disabled@example.com"); got != nil {
		t.Fatalf("disabled client link = %v, want nil", got)
	}
	if got := LinkFor(ib, "missing@example.com"); got != nil {
		t.Fatalf("missing client link = %v, want nil", got)
	}

	// Exercise remaining transport link formats + helpers.
	seiCfg := &OLCRTCConfig{Provider: "jitsi", Transport: "seichannel", RoomID: "r", CryptoKey: testHexKey64, SeiFps: 30, SeiBatch: 16, SeiFrag: 900, SeiAck: 2000}
	if got := (Instance{Protocol: model.OLCRTC, OLCRTC: seiCfg}).Links(model.Client{Enable: true}); !slices.Equal(got, []string{"olcrtc://jitsi?seichannel<fps=30&batch=16&frag=900&ack-ms=2000>@r#" + testHexKey64 + "$client"}) {
		t.Fatalf("seichannel link = %v", got)
	}
	vidCfg := &OLCRTCConfig{Provider: "telemost", Transport: "videochannel", RoomID: "r", CryptoKey: testHexKey64, VideoW: 1080, VideoH: 1080, VideoFps: 30}
	if got := (Instance{Protocol: model.OLCRTC, OLCRTC: vidCfg}).Links(model.Client{Enable: true}); !slices.Equal(got, []string{"olcrtc://telemost?videochannel<video-w=1080&video-h=1080&video-fps=30&video-codec=qrcode>@r#" + testHexKey64 + "$client"}) {
		t.Fatalf("videochannel link = %v", got)
	}

	if DisplayName(model.OLCRTC) != "olcRTC" || DisplayName(model.VLESS) != "vless" {
		t.Fatalf("unexpected DisplayName")
	}
	if TunGateway(model.OLCRTC, 5) != "" || TunGateway(model.QWDTT, 300) == "" || TunGateway(model.CSQTT, 2) != "10.252.2.1/30" {
		t.Fatalf("unexpected TunGateway")
	}

	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}
	first := filepath.Join(binDir, "extra-olcrtc"+exeSuffix)
	second := filepath.Join(binDir, "extra-olcrtc-"+runtime.GOARCH+exeSuffix)
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := BinaryPath(model.OLCRTC); got != second {
		t.Fatalf("BinaryPath skipped directory and returned %q, want %q", got, second)
	}
	if BinaryPath(model.VLESS) != "" {
		t.Fatalf("BinaryPath(vless) = %q, want empty", BinaryPath(model.VLESS))
	}

	// CSQTT hashes cap at 6 tokens and join with '+'; qWDTT listenAddr overrides port.
	csqttInst := Instance{
		Protocol: model.CSQTT,
		Listen:   "0.0.0.0",
		Port:     46000,
		CSQTT:    &CSQTTConfig{Hashes: "h1, h2 h3;h4\th5\nh6\rh7"},
	}
	csqttLinks := csqttInst.Links(model.Client{Email: "u@x", CsqttPassword: "pw", Enable: true})
	if len(csqttLinks) != 1 || !strings.Contains(csqttLinks[0], "&hashes=h1+h2+h3+h4+h5+h6") || strings.Contains(csqttLinks[0], "h7") {
		t.Fatalf("csqtt multi-hash link = %v", csqttLinks)
	}
	qwdttOverride := &model.Inbound{
		Id:       31,
		Protocol: model.QWDTT,
		Listen:   "0.0.0.0",
		Port:     56000,
		Settings: `{"sidecar":{"listenAddr":"192.0.2.9:56111","wgPort":56112,"rawPort":56113}}`,
	}
	if qi, err := InstanceFromInbound(qwdttOverride); err != nil || qi.Listen != "192.0.2.9" || qi.Port != 56111 {
		t.Fatalf("qwdtt listenAddr override = (%+v, %v)", qi, err)
	}
}
