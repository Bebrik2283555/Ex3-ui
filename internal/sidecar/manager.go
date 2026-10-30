package sidecar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// Reconciliation and retry cadences.
const (
	statusLines           = 200
	routingRefreshPeriod  = 5 * time.Minute
	retryBaseDelay        = 10 * time.Second
	retryMaxDelay         = 5 * time.Minute
	csqttPushAttempts     = 8
	csqttPushStepDelay    = 250 * time.Millisecond
	csqttLedgerFileSuffix = "ex3ui-clients.json"
)

var errInboundIDRequired = errors.New("sidecar: inbound id is required")

// Status is the operator-visible state of one supervised inbound.
type Status struct {
	InboundID int      `json:"inboundId"`
	Protocol  string   `json:"protocol"`
	Running   bool     `json:"running"`
	LastError string   `json:"lastError,omitempty"`
	LastLog   string   `json:"lastLog,omitempty"`
	Logs      []string `json:"logs,omitempty"`
}

// managed is the per-inbound bookkeeping: the committed instance, backoff
// state, routing stamp, push hand-off state and the child itself.
type managed struct {
	inst        Instance
	fingerprint string
	lastError   string
	failures    int
	nextAttempt time.Time
	lastRouting time.Time
	pushError   string
	pushing     bool
	proc        *Proc
	startMu     sync.Mutex
}

// Manager is the process-wide registry of sidecar children keyed by inbound
// id, together with the per-inbound traffic baseline.
type Manager struct {
	mu    sync.Mutex
	procs map[int]*managed
	seen  map[int]map[string]trafficCounter
}

var defaultManager = &Manager{
	procs: make(map[int]*managed),
	seen:  make(map[int]map[string]trafficCounter),
}

// GetManager returns the singleton supervisor used by every panel subsystem.
func GetManager() *Manager { return defaultManager }

func (m *Manager) initLocked() {
	if m.procs == nil {
		m.procs = make(map[int]*managed)
	}
	if m.seen == nil {
		m.seen = make(map[int]map[string]trafficCounter)
	}
}

// Ensure converges one inbound: a live child with an unchanged fingerprint is
// left alone, a deferred retry is skipped, everything else goes through the
// launch sequence.
func (m *Manager) Ensure(inst Instance) error {
	if inst.InboundID <= 0 {
		return errInboundIDRequired
	}

	m.mu.Lock()
	m.initLocked()
	if e := m.procs[inst.InboundID]; e != nil {
		if handled, push, route := m.aliveLocked(e, inst); handled {
			m.mu.Unlock()
			m.followUp(inst, e, push, route)
			return nil
		}
		if e.fingerprint == inst.Fingerprint && !e.proc.IsRunning() && time.Now().Before(e.nextAttempt) {
			m.mu.Unlock()
			return nil
		}
	}
	m.mu.Unlock()

	return m.startSerialised(inst)
}

// aliveLocked matches the fast path for a live child: it refreshes the stored
// instance, resets the backoff state and reports the follow-up work.
func (m *Manager) aliveLocked(e *managed, inst Instance) (handled, push, route bool) {
	if e.fingerprint != inst.Fingerprint || !e.proc.IsRunning() {
		return false, false, false
	}
	e.inst = inst
	e.failures = 0
	e.nextAttempt = time.Time{}
	push = inst.Protocol == model.CSQTT && e.pushError != "" && !e.pushing
	route = time.Since(e.lastRouting) >= routingRefreshPeriod
	if route {
		e.lastRouting = time.Now()
	}
	return true, push, route
}

func (m *Manager) followUp(inst Instance, e *managed, push, route bool) {
	if push {
		m.schedulePush(inst.InboundID)
	}
	if route {
		m.scheduleRouting(inst, e)
	}
}

// startSerialised runs the whole stop/prepare/spawn/commit sequence under the
// inbound's own start lock, never under the registry lock.
func (m *Manager) startSerialised(inst Instance) error {
	e := m.entry(inst)
	e.startMu.Lock()
	defer e.startMu.Unlock()

	m.mu.Lock()
	if m.procs[inst.InboundID] != e {
		// A concurrent stop dropped the entry while we waited.
		m.mu.Unlock()
		return nil
	}
	if handled, push, route := m.aliveLocked(e, inst); handled {
		m.mu.Unlock()
		m.followUp(inst, e, push, route)
		return nil
	}
	if e.fingerprint == inst.Fingerprint && !e.proc.IsRunning() && time.Now().Before(e.nextAttempt) {
		// A concurrent attempt failed while we waited for the start lock.
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	err := m.launch(e, inst)

	m.mu.Lock()
	if m.procs[inst.InboundID] != e {
		m.mu.Unlock()
		// The entry vanished during the launch: the caller no longer wants it.
		_ = e.proc.Stop()
		return nil
	}
	if err != nil {
		e.fingerprint = inst.Fingerprint
		e.lastError = err.Error()
		e.failures++
		e.nextAttempt = time.Now().Add(retryDelay(e.failures))
		m.mu.Unlock()
		return err
	}
	e.inst = inst
	e.fingerprint = inst.Fingerprint
	e.lastError = ""
	e.pushError = ""
	e.failures = 0
	e.nextAttempt = time.Time{}
	e.lastRouting = time.Now()
	push := inst.Protocol == model.CSQTT
	m.mu.Unlock()

	m.followUp(inst, e, push, true)
	return nil
}

func (m *Manager) entry(inst Instance) *managed {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLocked()
	if e := m.procs[inst.InboundID]; e != nil {
		return e
	}
	e := &managed{
		proc:        NewProc(fmt.Sprintf("%s-%d", inst.Protocol, inst.InboundID)),
		fingerprint: inst.Fingerprint,
	}
	m.procs[inst.InboundID] = e
	return e
}

// launch resolves the binary, fixes the exec bit, rebuilds argv, stops the old
// child before preparing files and spawns the new one.
func (m *Manager) launch(e *managed, inst Instance) error {
	name := DisplayName(inst.Protocol)
	bin := BinaryPath(inst.Protocol)
	if strings.TrimSpace(bin) == "" {
		return fmt.Errorf("%s: binary not found", name)
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("%s: binary not found at %s", name, bin)
	}
	if err := ensureExecutable(bin); err != nil {
		return err
	}
	args := inst.BuildArgs()
	if len(args) == 0 {
		return fmt.Errorf("%s: no launch arguments", name)
	}

	if e.proc.IsRunning() {
		_ = e.proc.Stop()
	}
	if err := inst.Prepare(); err != nil {
		return err
	}
	if err := e.proc.Start(bin, args, inst.SecretEnv()...); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	logger.Infof("sidecar: %s started (pid %d)", e.proc.name, e.proc.pid())
	return nil
}

// Remove drops the entry and its traffic baseline first, then waits for an
// in-flight start, clears routing and stops the child.
func (m *Manager) Remove(inboundID int) {
	m.mu.Lock()
	e := m.procs[inboundID]
	if e == nil {
		m.mu.Unlock()
		return
	}
	delete(m.procs, inboundID)
	delete(m.seen, inboundID)
	m.mu.Unlock()

	e.startMu.Lock()
	defer e.startMu.Unlock()

	ClearRouting(e.inst)
	if err := e.proc.Stop(); err != nil {
		logger.Debug("sidecar: stop inbound", inboundID, ":", err)
	}
}

// StopAll stops every supervised inbound, sequentially.
func (m *Manager) StopAll() {
	m.mu.Lock()
	m.initLocked()
	ids := make([]int, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Remove(id)
	}
}

// StatusOf returns a snapshot of one inbound, never blocking on the child.
func (m *Manager) StatusOf(inboundID int) Status {
	m.mu.Lock()
	e := m.procs[inboundID]
	if e == nil {
		m.mu.Unlock()
		return Status{InboundID: inboundID}
	}
	status := Status{InboundID: inboundID}
	if e.inst.Protocol != "" {
		status.Protocol = string(e.inst.Protocol)
	}
	status.LastError = e.lastError
	proc := e.proc
	m.mu.Unlock()

	status.Running = proc.IsRunning()
	status.LastLog = proc.LastLine()
	status.Logs = proc.Lines(statusLines)
	return status
}

// Logs returns the last lines captured for one inbound, oldest first.
func (m *Manager) Logs(inboundID, lines int) []string {
	m.mu.Lock()
	e := m.procs[inboundID]
	m.mu.Unlock()
	if e == nil {
		return nil
	}
	return e.proc.Lines(lines)
}

// SyncAll reconciles the running set against the given rows: removal happens
// before any start, and a failing row never fails the pass.
func (m *Manager) SyncAll(inbounds []*model.Inbound) {
	desired := make(map[int]Instance, len(inbounds))
	for _, ib := range inbounds {
		if ib == nil || !Supported(ib.Protocol) || !ib.Enable || ib.NodeID != nil {
			continue
		}
		inst, err := InstanceFromInbound(ib)
		if err != nil {
			logger.Warning("sidecar: inbound", ib.Id, ":", err)
			continue
		}
		if err := Validate(ib); err != nil {
			logger.Warning("sidecar: inbound", ib.Id, ":", err)
			continue
		}
		desired[ib.Id] = inst
	}

	m.mu.Lock()
	m.initLocked()
	stale := make([]int, 0, len(m.procs))
	for id := range m.procs {
		if _, ok := desired[id]; !ok {
			stale = append(stale, id)
		}
	}
	m.mu.Unlock()

	for _, id := range stale {
		m.Remove(id)
	}
	for _, inst := range desired {
		if err := m.Ensure(inst); err != nil {
			logger.Warning("sidecar: start", DisplayName(inst.Protocol), inst.InboundID, ":", err)
		}
	}
}

// scheduleRouting applies routing in the background for a live entry only.
func (m *Manager) scheduleRouting(inst Instance, e *managed) {
	common.GoRecover("sidecar-routing", func() {
		m.mu.Lock()
		live := m.procs[inst.InboundID] == e
		m.mu.Unlock()
		if !live {
			return
		}
		EnsureRouting(inst)
	})
}

// schedulePush starts the CSQTT client hand-off unless one is already running
// for this inbound; a schedule during a push is dropped, not queued.
func (m *Manager) schedulePush(inboundID int) {
	m.mu.Lock()
	m.initLocked()
	e := m.procs[inboundID]
	if e == nil || e.pushing {
		m.mu.Unlock()
		return
	}
	e.pushing = true
	m.mu.Unlock()

	common.GoRecover("sidecar-csqtt-push", func() { m.pushClients(inboundID) })
}

// pushClients retries the admin-API hand-off with a linear backoff and gives
// up when the child dies or the attempts are exhausted.
func (m *Manager) pushClients(inboundID int) {
	e := m.lookup(inboundID)
	if e == nil {
		return
	}
	defer func() {
		m.mu.Lock()
		if m.procs[inboundID] == e {
			e.pushing = false
		}
		m.mu.Unlock()
	}()

	var lastErr error
	for attempt := 1; attempt <= csqttPushAttempts; attempt++ {
		time.Sleep(time.Duration(attempt) * csqttPushStepDelay)

		m.mu.Lock()
		cur := m.procs[inboundID]
		if cur != e || !e.proc.IsRunning() {
			m.mu.Unlock()
			return
		}
		inst := e.inst
		m.mu.Unlock()

		cfg := inst.CSQTT
		if cfg == nil {
			return
		}
		err := NewCSQTTClient(cfg.WebPort, cfg.WebUser, cfg.WebPass).SyncClients(
			inst.Clients, csqttLedgerPath(cfg), cfg.MainPass, cfg.Hashes, cfg.HashesByEmail)
		if err == nil {
			m.mu.Lock()
			if m.procs[inboundID] == e {
				e.pushError = ""
			}
			m.mu.Unlock()
			return
		}
		lastErr = err
	}

	m.mu.Lock()
	if m.procs[inboundID] == e {
		e.pushError = lastErr.Error()
	}
	m.mu.Unlock()
	logger.Warning("sidecar: CSQTT client sync failed:", lastErr)
}

func (m *Manager) lookup(inboundID int) *managed {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[inboundID]
}

func csqttLedgerPath(cfg *CSQTTConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return ""
	}
	return filepath.Join(cfg.ConfigDir, csqttLedgerFileSuffix)
}

// retryDelay doubles from ten seconds and is clamped to five minutes.
func retryDelay(failures int) time.Duration {
	delay := retryBaseDelay
	for n := 1; n < failures && delay < retryMaxDelay; n++ {
		delay *= 2
	}
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	return delay
}
