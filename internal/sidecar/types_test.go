package sidecar

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// qWDTT renders one resolver into the WireGuard config; legacy comma-separated
// values must collapse to the first address on save.
func TestNormalizeQwdttSingleDNS(t *testing.T) {
	ib := &model.Inbound{
		Id:       1,
		Protocol: model.QWDTT,
		Settings: `{"sidecar":{"dns":"1.1.1.1,1.0.0.1"}}`,
	}
	out, err := Normalize(ib)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	var root struct {
		Sidecar struct {
			DNS string `json:"dns"`
		} `json:"sidecar"`
	}
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("unmarshal normalized settings: %v", err)
	}
	if root.Sidecar.DNS != "1.1.1.1" {
		t.Fatalf("dns = %q, want the first resolver only", root.Sidecar.DNS)
	}
}

// The shipped csqtt-linux binary has no --device-id flag: passing it makes the
// process exit before it can serve the app.
func TestCSQTTBuildArgsHasNoDeviceID(t *testing.T) {
	inst := Instance{
		Protocol: model.CSQTT,
		Listen:   "0.0.0.0",
		Port:     46000,
		CSQTT: &CSQTTConfig{
			WebPort:   46002,
			ConfigDir: "/tmp/csqtt",
			WebUser:   "ex3ui",
			WebPass:   "web-pass",
			MainPass:  "main-pass",
		},
	}
	args := inst.BuildArgs()
	if slices.Contains(args, "--device-id") {
		t.Fatalf("BuildArgs must not pass --device-id: %v", strings.Join(args, " "))
	}
	// Credentials belong in the child's environment: argv is readable by every
	// local user through /proc/<pid>/cmdline.
	for _, flag := range []string{"--password", "--web-user", "--web-pass"} {
		if slices.Contains(args, flag) {
			t.Fatalf("BuildArgs must not pass %s: %v", flag, strings.Join(args, " "))
		}
	}
	env := inst.SecretEnv()
	for _, want := range []string{"CSQTT_MAIN_PASSWORD=main-pass", "CSQTT_WEB_USER=ex3ui", "CSQTT_WEB_PASS=web-pass"} {
		if !slices.Contains(env, want) {
			t.Fatalf("SecretEnv = %v, want %s", env, want)
		}
	}
}

// The stored main password is independent of client credentials; editing a
// client must not silently rotate the server credential.
func TestCSQTTKeepsStoredMainPassword(t *testing.T) {
	ib := &model.Inbound{
		Id:       5,
		Protocol: model.CSQTT,
		Listen:   "0.0.0.0",
		Port:     46000,
		Settings: `{"clients":[{"email":"a@x","csqttPassword":"client-pass","enable":true}],` +
			`"sidecar":{"mainPassword":"old-main","subHost":"1.2.3.4"}}`,
	}
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		t.Fatalf("InstanceFromInbound: %v", err)
	}
	if got := inst.CSQTT.MainPass; got != "old-main" {
		t.Fatalf("MainPass = %q, want the stored main password", got)
	}
	args := inst.BuildArgs()
	if strings.Contains(strings.Join(args, " "), "old-main") {
		t.Fatalf("BuildArgs leaks the main password: %v", args)
	}
	if !slices.Contains(inst.SecretEnv(), "CSQTT_MAIN_PASSWORD=old-main") {
		t.Fatalf("SecretEnv = %v, want the stored main password", inst.SecretEnv())
	}
	links := inst.Links(inst.Clients[0])
	if len(links) != 1 || !strings.Contains(links[0], "password=client-pass") {
		t.Fatalf("links = %v, want the client password", links)
	}
}

// A disabled client must not keep the server running on its password.
func TestCSQTTDisabledClientDoesNotChangeMainPassword(t *testing.T) {
	ib := &model.Inbound{
		Id:       5,
		Protocol: model.CSQTT,
		Listen:   "0.0.0.0",
		Port:     46000,
		Settings: `{"clients":[{"email":"a@x","csqttPassword":"client-pass","enable":false}],` +
			`"sidecar":{"mainPassword":"main-pass"}}`,
	}
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		t.Fatalf("InstanceFromInbound: %v", err)
	}
	if got := inst.CSQTT.MainPass; got != "main-pass" {
		t.Fatalf("MainPass = %q, want the stored main password", got)
	}
}

// Each qWDTT client must get its own password in both link forms, not the
// inbound-level fallback that used to be shared by everybody.
func TestQWDTTPerClientPasswords(t *testing.T) {
	ib := &model.Inbound{
		Id:       7,
		Protocol: model.QWDTT,
		Listen:   "0.0.0.0",
		Port:     56000,
		Settings: `{"clients":[` +
			`{"email":"a@x","qwdttPassword":"q-pass-a","enable":true},` +
			`{"email":"b@x","qwdttPassword":"q-pass-b","enable":true}],` +
			`"sidecar":{"mainPassword":"fallback","subHost":"vpn.example.com:56000","hashes":"abc"}}`,
	}
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		t.Fatalf("InstanceFromInbound: %v", err)
	}
	for i, want := range []string{"q-pass-a", "q-pass-b"} {
		links := inst.Links(inst.Clients[i])
		if len(links) != 1 {
			t.Fatalf("client %d links = %v, want one qWDTT link", i, links)
		}
		if !strings.Contains(links[0], "pass="+want) {
			t.Fatalf("client %d link = %q, want pass=%s", i, links[0], want)
		}
		var sub QwdttSubscription
		if err := json.Unmarshal([]byte(inst.SubJSON(inst.Clients[i])), &sub); err != nil {
			t.Fatalf("client %d SubJSON: %v", i, err)
		}
		if len(sub.Profiles) != 1 || sub.Profiles[0].Password != want {
			t.Fatalf("client %d SubJSON password = %+v, want %s", i, sub.Profiles, want)
		}
	}
}

// CSQTT links are strictly per-client: generic and main passwords are not
// alternate credentials for a client without its own CSQTT password.
func TestCSQTTPerClientPasswords(t *testing.T) {
	ib := &model.Inbound{
		Id:       8,
		Protocol: model.CSQTT,
		Listen:   "0.0.0.0",
		Port:     46000,
		Settings: `{"clients":[` +
			`{"email":"a@x","csqttPassword":"c-pass-a","enable":true},` +
			`{"email":"b@x","csqttPassword":"c-pass-b","enable":true},` +
			`{"email":"legacy@x","password":"legacy-pass","enable":true}],` +
			`"sidecar":{"mainPassword":"fallback","subHost":"vpn.example.com"}}`,
	}
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		t.Fatalf("InstanceFromInbound: %v", err)
	}
	if got := inst.CSQTT.MainPass; got != "fallback" {
		t.Fatalf("MainPass = %q, want the stored main password", got)
	}
	for i, want := range []string{"c-pass-a", "c-pass-b"} {
		links := inst.Links(inst.Clients[i])
		if len(links) != 1 {
			t.Fatalf("client %d links = %v, want one CSQTT link", i, links)
		}
		if !strings.Contains(links[0], "password="+want) {
			t.Fatalf("client %d link = %q, want password=%s", i, links[0], want)
		}
		if !strings.Contains(links[0], "&name="+urlEncode(inst.Clients[i].Email)) {
			t.Fatalf("client %d link = %q, want a profile name", i, links[0])
		}
	}
	if links := inst.Links(inst.Clients[2]); len(links) != 0 {
		t.Fatalf("client without CsqttPassword got links %v", links)
	}
	if got := csqttPasswordForClient(model.Client{CsqttPassword: "own"}); got != "own" {
		t.Fatalf("csqttPasswordForClient(own) = %q", got)
	}
	if got := csqttPasswordForClient(model.Client{Password: "generic"}); got != "" {
		t.Fatalf("csqttPasswordForClient(legacy) = %q, want empty", got)
	}
}

// A 1.4.5+ core persists its diagnostics secret in diag.key: both share
// payloads must carry the derived token so the client's check works.
func TestQWDTTDiagKeyInSharePayloads(t *testing.T) {
	dir := t.TempDir()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, qwdttDiagKeyName), []byte(hex.EncodeToString(secret)+"\n"), 0o600); err != nil {
		t.Fatalf("write diag.key: %v", err)
	}
	inst := Instance{
		Protocol: model.QWDTT,
		Listen:   "203.0.113.5",
		Port:     56000,
		QWDTT:    &QWDTTConfig{ConfigDir: dir, WGPort: 56001, Hashes: "abc"},
		Clients:  []model.Client{{Email: "a@x", QwdttPassword: "pass-a", Enable: true}},
	}
	want := base64.RawURLEncoding.EncodeToString(secret)
	links := inst.Links(inst.Clients[0])
	if len(links) != 1 || !strings.Contains(links[0], "dk="+want) {
		t.Fatalf("link = %v, want dk=%s", links, want)
	}
	var sub QwdttSubscription
	if err := json.Unmarshal([]byte(inst.SubJSON(inst.Clients[0])), &sub); err != nil {
		t.Fatalf("SubJSON: %v", err)
	}
	if len(sub.Profiles) != 1 || sub.Profiles[0].DiagKey != want {
		t.Fatalf("SubJSON diagKey = %+v, want %s", sub.Profiles, want)
	}
}

// A missing or malformed diag.key must leave working links without a token.
func TestQWDTTDiagKeyAbsent(t *testing.T) {
	for name, content := range map[string]string{
		"missing":   "",
		"malformed": "not-a-key\n",
	} {
		dir := t.TempDir()
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, qwdttDiagKeyName), []byte(content), 0o600); err != nil {
				t.Fatalf("%s: write diag.key: %v", name, err)
			}
		}
		inst := Instance{
			Protocol: model.QWDTT,
			Listen:   "203.0.113.5",
			Port:     56000,
			QWDTT:    &QWDTTConfig{ConfigDir: dir, WGPort: 56001, Hashes: "abc"},
			Clients:  []model.Client{{Email: "a@x", QwdttPassword: "pass-a", Enable: true}},
		}
		links := inst.Links(inst.Clients[0])
		if len(links) != 1 || strings.Contains(links[0], "dk=") {
			t.Fatalf("%s: link = %v, want no dk", name, links)
		}
		var sub QwdttSubscription
		if err := json.Unmarshal([]byte(inst.SubJSON(inst.Clients[0])), &sub); err != nil {
			t.Fatalf("%s: SubJSON: %v", name, err)
		}
		if len(sub.Profiles) != 1 || sub.Profiles[0].DiagKey != "" {
			t.Fatalf("%s: SubJSON diagKey = %+v, want empty", name, sub.Profiles)
		}
	}
}

// CSQTT/olcRTC instances carry no qWDTT config; the shared link payloads
// must stay empty instead of dereferencing a nil pointer.
func TestQWDTTOnlyHelpersAreNilSafe(t *testing.T) {
	inst := Instance{Protocol: model.CSQTT, CSQTT: &CSQTTConfig{}}
	if got := inst.LegacyURI(); got != "" {
		t.Fatalf("LegacyURI = %q, want empty", got)
	}
	if got := inst.SubJSON(model.Client{}); got != "" {
		t.Fatalf("SubJSON = %q, want empty", got)
	}
	if got := inst.qwdttHashes(model.Client{}); got != "" {
		t.Fatalf("qwdttHashes = %q, want empty", got)
	}
	if got := inst.qwdttPeer(); got != "" {
		t.Fatalf("qwdttPeer = %q, want empty", got)
	}
	if got := inst.qwdttPassword(model.Client{}); got != "" {
		t.Fatalf("qwdttPassword = %q, want empty", got)
	}
}

// The panel writes the olcRTC config as root: an operator-supplied path outside
// the panel data folder must be rejected instead of overwriting a system file.
func TestOLCRTCPathsMustStayInPanelData(t *testing.T) {
	for name, settings := range map[string]string{
		"config file": `{"sidecar":{"configFile":"/etc/passwd"}}`,
		"data dir":    `{"sidecar":{"dataDir":"/etc"}}`,
	} {
		ib := &model.Inbound{
			Id:       9,
			Protocol: model.OLCRTC,
			Listen:   "0.0.0.0",
			Port:     47000,
			Settings: settings,
		}
		if _, err := InstanceFromInbound(ib); err == nil {
			t.Fatalf("%s: InstanceFromInbound accepted a path outside the panel data folder", name)
		}
	}

	ib := &model.Inbound{Id: 9, Protocol: model.OLCRTC, Listen: "0.0.0.0", Port: 47000, Settings: `{}`}
	if _, err := InstanceFromInbound(ib); err != nil {
		t.Fatalf("default olcRTC paths must stay valid: %v", err)
	}
}
