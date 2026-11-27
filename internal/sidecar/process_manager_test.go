package sidecar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// TestMain lets a copy of this test binary named extra-olcrtc act as a real
// managed child process during Manager/Proc tests on both Windows and Linux.
func TestMain(m *testing.M) {
	base := filepath.Base(os.Args[0])
	if strings.HasPrefix(base, "extra-olcrtc") {
		if len(os.Args) < 2 {
			_, _ = fmt.Fprintln(os.Stderr, "missing config path")
			os.Exit(2)
		}
		fmt.Println("[info] TRACE: filtered-trace-line")
		fmt.Println("ready config=" + os.Args[1])
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		select {
		case <-sigCh:
		case <-time.After(15 * time.Second):
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestProcLifecycleAndLineBuffer(t *testing.T) {
	buf := newLineBuffer("unit")
	_, _ = buf.Write([]byte("alpha\n[2026] TRACE: skip\n[2026] DEBUG: skip\nbet"))
	_, _ = buf.Write([]byte("a\n"))
	_, _ = buf.Write([]byte("tail-no-nl"))
	buf.flush()
	lines := buf.snapshot(3)
	if !slices.Equal(lines, []string{"[2026] DEBUG: skip", "beta", "tail-no-nl"}) {
		t.Fatalf("snapshot(3) = %v", lines)
	}
	if buf.last() != "tail-no-nl" {
		t.Fatalf("last = %q", buf.last())
	}
	if forwardLogLine("") || forwardLogLine("[x] TRACE: a") || forwardLogLine("[x] DEBUG: b") || !forwardLogLine("INFO: ok") {
		t.Fatal("forwardLogLine filter mismatch")
	}

	// Copy the test binary as extra-olcrtc so Proc starts a real cross-platform child.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	exeName := "extra-olcrtc"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	binPath := filepath.Join(binDir, exeName)
	if err := os.WriteFile(binPath, data, 0o755); err != nil {
		t.Fatal(err)
	}

	proc := NewProc("test-olcrtc")
	if err := proc.Stop(); !errors.Is(err, errProcNotRunning) {
		t.Fatalf("Stop while idle = %v, want errProcNotRunning", err)
	}
	if err := proc.Signal(syscall.SIGTERM); !errors.Is(err, errProcNotRunning) {
		t.Fatalf("Signal while idle = %v, want errProcNotRunning", err)
	}
	if err := proc.Start(binPath, []string{filepath.Join(binDir, "server.yaml")}, "TEST_ENV=1"); err != nil {
		t.Fatalf("Proc.Start: %v", err)
	}
	t.Cleanup(func() { _ = proc.Stop() })

	if !proc.IsRunning() || proc.pid() <= 0 {
		t.Fatalf("expected running proc with pid > 0")
	}
	if err := proc.Start(binPath, []string{"x"}); !errors.Is(err, errProcRunning) {
		t.Fatalf("double Start = %v, want errProcRunning", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(proc.LastLine(), "ready config=") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(proc.LastLine(), "ready config=") {
		t.Fatalf("child output never captured; lines = %v", proc.Lines(0))
	}
	if err := proc.Stop(); err != nil {
		t.Fatalf("Proc.Stop: %v", err)
	}
	if proc.IsRunning() {
		t.Fatal("proc still running after Stop")
	}
}

func TestManagerEnsureSyncAllRemoveAndCollectTraffic(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	dbDir := filepath.Join(root, "db")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exeName := "extra-olcrtc"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, exeName), data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XUI_BIN_FOLDER", binDir)
	t.Setenv("XUI_DB_FOLDER", dbDir)

	mgr := &Manager{
		procs: make(map[int]*managed),
		seen:  make(map[int]map[string]trafficCounter),
	}
	t.Cleanup(mgr.StopAll)

	if err := mgr.Ensure(Instance{Protocol: model.OLCRTC}); !errors.Is(err, errInboundIDRequired) {
		t.Fatalf("Ensure(id=0) = %v, want errInboundIDRequired", err)
	}

	ib := &model.Inbound{
		Id:       77,
		Tag:      "olcrtc-vk",
		Protocol: model.OLCRTC,
		Enable:   true,
		Settings: `{
			"clients": [{"email": "u1@x", "enable": true}],
			"sidecar": {
				"provider": "vkcalls",
				"roomId": "https://vk.ru/call/join/123",
				"cryptoKey": "` + testHexKey64 + `",
				"transport": "vp8channel",
				"dns": "8.8.8.8:53"
			}
		}`,
	}
	mgr.SyncAll([]*model.Inbound{
		nil,
		{Id: 1, Enable: false, Protocol: model.OLCRTC},
		{Id: 2, Enable: true, Protocol: model.VLESS},
		{Id: 3, Enable: true, Protocol: model.OLCRTC, Settings: `{"sidecar":{"provider":"zoom"}}`},
		ib,
	})

	st := mgr.StatusOf(77)
	if !st.Running || st.Protocol != "olcrtc" || st.LastError != "" {
		t.Fatalf("StatusOf(77) = %+v, want running olcrtc", st)
	}
	// Calling Ensure with identical fingerprint leaves process untouched (idempotent).
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	// Changing fingerprint restarts the live child cleanly via Stop + Start.
	updatedIb := *ib
	updatedIb.Settings = strings.ReplaceAll(ib.Settings, `"vp8channel"`, `"vp8channel","debug":true`)
	updatedInst, err := InstanceFromInbound(&updatedIb)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Ensure(updatedInst); err != nil {
		t.Fatalf("fingerprint change Ensure: %v", err)
	}
	if got := mgr.StatusOf(999); got.Running || got.Protocol != "" {
		t.Fatalf("unknown status = %+v", got)
	}

	// SyncAll with empty list removes stale inbound 77 cleanly.
	mgr.SyncAll(nil)
	if st := mgr.StatusOf(77); st.Running {
		t.Fatalf("inbound 77 still running after removal: %+v", st)
	}

	// Inject a synthetic running qWDTT process entry to exercise CollectTraffic delta + rollover.
	qwdttDir := filepath.Join(root, "qwdtt")
	if err := os.MkdirAll(qwdttDir, 0o755); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(qwdttDir, qwdttStoreName)
	writeStore := func(up, down int64) {
		payload := fmt.Sprintf(`{"passwords":{"secret1":{"up_bytes":%d,"down_bytes":%d}}}`, up, down)
		if err := os.WriteFile(storePath, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeStore(100, 200)
	fakeProc := NewProc("qwdtt-88")
	fakeProc.running = true
	mgr.procs[88] = &managed{
		proc: fakeProc,
		inst: Instance{
			InboundID:        88,
			Tag:              "qwdtt-tag",
			Protocol:         model.QWDTT,
			RouteThroughXray: true,
			QWDTT:            &QWDTTConfig{ConfigDir: qwdttDir},
			Clients: []model.Client{
				{Email: "first@x", QwdttPassword: "secret1", Enable: true},
				{Email: "dup@x", QwdttPassword: "secret1", Enable: true},
			},
		},
	}
	if deltas := mgr.CollectTraffic(); len(deltas) != 0 {
		t.Fatalf("baseline poll returned %+v, want empty", deltas)
	}
	writeStore(350, 700)
	deltas := mgr.CollectTraffic()
	if len(deltas) != 1 || deltas[0].Email != "first@x" || deltas[0].Up != 250 || deltas[0].Down != 500 || !deltas[0].Online || !deltas[0].Routed {
		t.Fatalf("delta poll = %+v", deltas)
	}
	// Counter restart below previous baseline reports the new counter value instead of negative delta.
	writeStore(50, 90)
	deltas = mgr.CollectTraffic()
	if len(deltas) != 1 || deltas[0].Up != 50 || deltas[0].Down != 90 {
		t.Fatalf("rollover delta = %+v, want Up=50 Down=90", deltas)
	}
}

func TestPublicIPAndLegacyLinks(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			_, _ = io.WriteString(w, "not-an-ip")
			return
		}
		_, _ = io.WriteString(w, "203.0.113.55\n")
	}))
	defer srv.Close()

	origServices := publicIPEchoServices
	publicIPEchoServices = []string{srv.URL + "/bad", srv.URL + "/good"}
	publicIPMu.Lock()
	origVal, origFetched := publicIPValue, publicIPFetched
	publicIPValue, publicIPFetched = "", time.Time{}
	publicIPMu.Unlock()
	t.Cleanup(func() {
		publicIPEchoServices = origServices
		publicIPMu.Lock()
		publicIPValue, publicIPFetched = origVal, origFetched
		publicIPMu.Unlock()
	})

	if got := PublicIP(); got != "203.0.113.55" {
		t.Fatalf("PublicIP() = %q, want 203.0.113.55", got)
	}
	// Cached value stays warm without additional HTTP calls.
	before := atomic.LoadInt32(&hits)
	if got := PublicIP(); got != "203.0.113.55" || atomic.LoadInt32(&hits) != before {
		t.Fatalf("cached PublicIP mismatch")
	}

	// Exercise LegacyURI + prepareCSQTT + CSQTT counter endpoint against TLS test server.
	qwdttInst := Instance{
		Protocol: model.QWDTT,
		Listen:   "0.0.0.0",
		Port:     56000,
		QWDTT:    &QWDTTConfig{WGPort: 56001, ClientPort: 9000, MainPass: "mainpw", Hashes: "h1,h2", PublicHost: "vpn.example.com"},
		Clients:  []model.Client{{Email: "c@x", Enable: true, QwdttPassword: "clipw"}},
	}
	if got := qwdttInst.LegacyURI(); got != "wdtt://vpn.example.com:56000:56001:9000:clipw:h1" {
		t.Fatalf("LegacyURI = %q", got)
	}
	csqttDir := t.TempDir()
	if err := (Instance{Protocol: model.CSQTT, CSQTT: &CSQTTConfig{ConfigDir: csqttDir}}).Prepare(); err != nil {
		t.Fatalf("prepareCSQTT: %v", err)
	}
	if _, err := os.Stat(filepath.Join(csqttDir, csqttStampFileName)); err != nil {
		t.Fatalf("csqtt store stamp missing: %v", err)
	}
}

func TestCSQTTBatchSyncCountersAndManagerEdges(t *testing.T) {
	var mu sync.Mutex
	deleted := []string{}
	created := []csqttCreatePayload{}
	toggled := []csqttTogglePayload{}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case csqttAPILoginPath:
			http.Error(w, "use api key", http.StatusNotFound)
		case csqttAPIClientsPath:
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode([]csqttClientInfo{
					{Password: "keep-pw", Name: "keep@x", Active: true, Up: 100, Down: 250, ActiveSessions: 1},
					{Password: "old-pw", Name: "old@x", Active: true},
				})
				return
			}
			var req csqttCreatePayload
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				created = append(created, req)
				mu.Unlock()
			}
			_, _ = w.Write([]byte("ok"))
		case csqttAPIDeletePath:
			var req csqttPasswordsPayload
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				deleted = append(deleted, req.Passwords...)
				mu.Unlock()
			}
			_, _ = w.Write([]byte("ok"))
		case csqttAPITogglePath:
			var req csqttTogglePayload
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				toggled = append(toggled, req)
				mu.Unlock()
			}
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ledgerPath := filepath.Join(t.TempDir(), csqttLedgerFileSuffix)
	if err := writeLedger(ledgerPath, []string{"keep-pw", "old-pw"}); err != nil {
		t.Fatal(err)
	}

	c := newCSQTTClient(srv.URL, "ex3ui", "secret-pw")
	now := time.Now().UnixMilli()
	clients := []model.Client{
		{Email: "keep@x", CsqttPassword: "keep-pw", Enable: false},
		{Email: "new@x", CsqttPassword: "new-pw", Enable: true, ExpiryTime: now + 3*csqttAPIDayMillis},
	}
	if err := c.SyncClients(clients, ledgerPath, "main-pw", "defhash", map[string]string{"new@x": "custhash"}); err != nil {
		t.Fatalf("SyncClients(batch): %v", err)
	}
	mu.Lock()
	if !slices.Equal(deleted, []string{"old-pw"}) {
		t.Fatalf("deleted = %v, want [old-pw]", deleted)
	}
	if len(created) != 1 || created[0].Password != "new-pw" || created[0].Hash != "custhash" || created[0].Days <= 0 {
		t.Fatalf("created = %+v", created)
	}
	if len(toggled) != 1 || !slices.Equal(toggled[0].Passwords, []string{"keep-pw"}) || toggled[0].Active {
		t.Fatalf("toggled = %+v", toggled)
	}
	mu.Unlock()
	gotLedger := readLedger(ledgerPath)
	slices.Sort(gotLedger)
	if !slices.Equal(gotLedger, []string{"keep-pw", "new-pw"}) {
		t.Fatalf("ledger = %v", gotLedger)
	}

	// Verify csqttCounters reads traffic & active session state via cached client slot.
	const fakePort = 46999
	csqttClientsMu.Lock()
	csqttClients[fakePort] = c
	csqttClientsMu.Unlock()
	t.Cleanup(func() {
		csqttClientsMu.Lock()
		delete(csqttClients, fakePort)
		csqttClientsMu.Unlock()
	})
	counters, online := csqttCounters(Instance{
		Protocol: model.CSQTT,
		CSQTT:    &CSQTTConfig{WebPort: fakePort, WebUser: "ex3ui", WebPass: "secret-pw"},
	})
	if counters["keep-pw"] != (trafficCounter{Up: 100, Down: 250}) || !online["keep-pw"] {
		t.Fatalf("csqttCounters = (%+v, %+v)", counters, online)
	}

	// Verify retryDelay clamping, Settings(), DataDir(), Manager.Logs and missing-binary backoff.
	if retryDelay(1) != retryBaseDelay || retryDelay(20) != retryMaxDelay {
		t.Fatalf("retryDelay bounds invalid: %v, %v", retryDelay(1), retryDelay(20))
	}
	olInst := Instance{Protocol: model.OLCRTC, OLCRTC: &OLCRTCConfig{Provider: "jitsi"}}
	if olInst.Settings() != olInst.OLCRTC || (Instance{Protocol: model.VLESS}).Settings() != nil {
		t.Fatalf("Settings() dispatch mismatch")
	}
	if !strings.Contains(DataDir(9, model.OLCRTC), "olcrtc-9") {
		t.Fatalf("DataDir unexpected: %s", DataDir(9, model.OLCRTC))
	}

	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	mgr := &Manager{procs: make(map[int]*managed), seen: make(map[int]map[string]trafficCounter)}
	if err := mgr.Ensure(Instance{InboundID: 51, Protocol: model.OLCRTC, Fingerprint: "fp1", OLCRTC: &OLCRTCConfig{}}); err == nil {
		t.Fatal("expected binary not found error")
	}
	// Backoff skips immediate second Ensure with same fingerprint until retry window elapses.
	if err := mgr.Ensure(Instance{InboundID: 51, Protocol: model.OLCRTC, Fingerprint: "fp1", OLCRTC: &OLCRTCConfig{}}); err != nil {
		t.Fatalf("expected backoff skip, got: %v", err)
	}
	if got := mgr.Logs(51, 10); len(got) != 0 {
		t.Fatalf("Logs for failed launch = %v", got)
	}
	if got := mgr.Logs(999, 10); got != nil {
		t.Fatalf("Logs for unknown inbound = %v", got)
	}
}
