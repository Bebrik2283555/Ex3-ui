package sidecar

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type managed struct {
	inst        Instance
	proc        *Proc
	fingerprint string
	lastErr     string
	// syncErr remembers a failed CSQTT client push so the reconcile tick can
	// retry it even when the inbound fingerprint did not change.
	syncErr string
	syncing bool
}

// Manager owns one process per sidecar inbound.
type Manager struct {
	mu    sync.Mutex
	procs map[int]*managed
	// seen keeps the last counter read per inbound and client key. Core
	// counters are cumulative, so only the delta is committed.
	seen map[int]map[string]trafficCounter
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// GetManager returns the shared per-inbound sidecar manager.
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = &Manager{procs: map[int]*managed{}, seen: map[int]map[string]trafficCounter{}}
	})
	return manager
}

// Ensure starts or reconfigures the process for one inbound. A no-op when the
// fingerprint is unchanged and the process is still alive.
func (m *Manager) Ensure(inst Instance) error {
	if inst.InboundID <= 0 {
		return fmt.Errorf("sidecar: inbound id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mg, ok := m.procs[inst.InboundID]
	if !ok {
		mg = &managed{proc: NewProc(fmt.Sprintf("%s-%d", inst.Protocol, inst.InboundID))}
		m.procs[inst.InboundID] = mg
	}
	if mg.fingerprint == inst.Fingerprint && mg.proc.IsRunning() {
		mg.inst = inst
		if inst.Protocol == model.CSQTT && mg.syncErr != "" && !mg.syncing {
			mg.syncing = true
			go m.syncCSQTT(inst)
		}
		go EnsureRouting(inst)
		return nil
	}
	bin := BinaryPath(inst.Protocol)
	if bin == "" {
		err := fmt.Errorf("%s: binary not found", DisplayName(inst.Protocol))
		mg.lastErr = err.Error()
		return err
	}
	if _, err := os.Stat(bin); err != nil {
		err = fmt.Errorf("%s: binary not found at %s", DisplayName(inst.Protocol), bin)
		mg.lastErr = err.Error()
		return err
	}
	if err := ensureExecutable(bin); err != nil {
		mg.lastErr = err.Error()
		return err
	}
	args := inst.BuildArgs()
	if len(args) == 0 {
		err := fmt.Errorf("%s: no launch arguments", DisplayName(inst.Protocol))
		mg.lastErr = err.Error()
		return err
	}
	// Stop before Prepare: CSQTT checks its bundled store version and qWDTT
	// rewrites passwords.json. Touching either while the old process is live
	// races its own writer.
	if mg.proc.IsRunning() {
		_ = mg.proc.Stop()
	}
	if err := inst.Prepare(); err != nil {
		mg.lastErr = err.Error()
		return err
	}
	if err := mg.proc.Start(bin, args); err != nil {
		mg.lastErr = err.Error()
		return fmt.Errorf("%s: %w", DisplayName(inst.Protocol), err)
	}
	mg.inst = inst
	mg.fingerprint = inst.Fingerprint
	mg.lastErr = ""
	mg.syncErr = ""
	if inst.Protocol == model.CSQTT {
		mg.syncing = true
		go m.syncCSQTT(inst)
	} else {
		mg.syncing = false
	}
	go EnsureRouting(inst)
	return nil
}

// Remove stops and forgets the process for an inbound.
func (m *Manager) Remove(inboundID int) {
	m.mu.Lock()
	mg, ok := m.procs[inboundID]
	if ok {
		delete(m.procs, inboundID)
		delete(m.seen, inboundID)
	}
	m.mu.Unlock()
	if ok {
		ClearRouting(mg.inst)
		if mg.proc.IsRunning() {
			if err := mg.proc.Stop(); err != nil {
				logger.Debug("sidecar: stop inbound", inboundID, ":", err)
			}
		}
	}
}

// SyncAll reconciles the manager with the database: enabled local sidecar
// inbounds are ensured, everything else is stopped.
func (m *Manager) SyncAll(inbounds []*model.Inbound) {
	desired := make(map[int]Instance)
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
	var stale []int
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

// StopAll terminates every managed process (panel shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]int, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Remove(id)
	}
}

// Status describes one supervised inbound.
type Status struct {
	InboundID int      `json:"inboundId"`
	Protocol  string   `json:"protocol"`
	Running   bool     `json:"running"`
	LastError string   `json:"lastError,omitempty"`
	LastLog   string   `json:"lastLog,omitempty"`
	Logs      []string `json:"logs,omitempty"`
}

// StatusOf returns a status snapshot for one inbound.
func (m *Manager) StatusOf(inboundID int) Status {
	m.mu.Lock()
	mg, ok := m.procs[inboundID]
	if !ok {
		m.mu.Unlock()
		return Status{InboundID: inboundID}
	}
	protocol := mg.inst.Protocol
	lastErr := mg.lastErr
	proc := mg.proc
	m.mu.Unlock()
	return Status{
		InboundID: inboundID,
		Protocol:  string(protocol),
		Running:   proc.IsRunning(),
		LastError: lastErr,
		LastLog:   proc.LastLine(),
		Logs:      proc.Lines(200),
	}
}

// Logs returns recent process output for one inbound.
func (m *Manager) Logs(inboundID, lines int) []string {
	m.mu.Lock()
	mg, ok := m.procs[inboundID]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return mg.proc.Lines(lines)
}

// LinkFor returns the share links for one client of the given inbound.
func LinkFor(ib *model.Inbound, email string) []string {
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		return nil
	}
	for _, cl := range inst.Clients {
		if strings.EqualFold(cl.Email, email) {
			return inst.Links(cl)
		}
	}
	return nil
}

// syncCSQTT pushes the panel client list into a freshly started csqtt server.
// The admin API only listens on loopback; failures are logged and retried by
// the reconcile tick while syncErr remains set.
func (m *Manager) syncCSQTT(inst Instance) {
	var lastErr error
	defer func() { m.finishCSQTTSync(inst.InboundID, lastErr) }()
	cfg := inst.CSQTT
	if cfg == nil {
		return
	}
	api := NewCSQTTClient(cfg.WebPort, cfg.WebUser, cfg.WebPass)
	ledger := filepath.Join(cfg.ConfigDir, "ex3ui-clients.json")
	for attempt := 0; attempt < 8; attempt++ {
		time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
		if !m.isRunning(inst.InboundID) {
			return
		}
		lastErr = api.SyncClients(inst.Clients, ledger, cfg.MainPass, cfg.Hashes, cfg.HashesByEmail)
		if lastErr == nil {
			return
		}
	}
	if lastErr != nil {
		logger.Warning("sidecar: CSQTT client sync failed:", lastErr)
	}
}

// finishCSQTTSync records the result so a failed push is retried by Ensure.
func (m *Manager) finishCSQTTSync(inboundID int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mg, ok := m.procs[inboundID]
	if !ok {
		return
	}
	mg.syncing = false
	if err == nil {
		mg.syncErr = ""
		return
	}
	mg.syncErr = err.Error()
}

func (m *Manager) isRunning(inboundID int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	mg, ok := m.procs[inboundID]
	return ok && mg.proc.IsRunning()
}
