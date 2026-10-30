package tuic

import (
	"errors"
	"testing"
)

// A structural change must close the running server before the replacement
// binds its port. ensureLocked detaches it instead of starting early, and
// Ensure converges after the drain.
func TestEnsureDetachesRunningServerBeforeStructuralRestart(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t)
	inst := Instance{
		Id:          9131,
		Tag:         "tuic-restart",
		Listen:      "127.0.0.1",
		Port:        0,
		Certificate: string(certPEM),
		PrivateKey:  string(keyPEM),
		MaxIdleTime: 30,
		Clients:     []TuicClientSettings{{UUID: "a0000000-0000-0000-0000-000000000001", Password: "p", Email: "e1"}},
	}

	m := &Manager{servers: map[int]*managed{}, lastStartErr: map[int]string{}}
	t.Cleanup(m.StopAll)

	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	old := m.servers[inst.Id].server

	inst.MaxIdleTime = 45
	m.mu.Lock()
	detached, err := m.ensureLocked(inst)
	m.mu.Unlock()
	if !errors.Is(err, errRestartPending) {
		t.Fatalf("ensureLocked error = %v, want errRestartPending", err)
	}
	if detached == nil || detached.server != old {
		t.Fatal("ensureLocked must hand back the running server instead of starting a new one")
	}
	if _, tracked := m.servers[inst.Id]; tracked {
		t.Fatal("the detached entry must be dropped so the retry starts fresh")
	}

	m.stopAndDrain(detached)
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure after drain: %v", err)
	}
	if !m.HasRunning() {
		t.Fatal("no server running after the structural restart")
	}
	if m.servers[inst.Id] == nil || m.servers[inst.Id].server == old {
		t.Fatal("expected a replacement server")
	}
}
