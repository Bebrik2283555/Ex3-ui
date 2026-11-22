package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A bulk reset is about counters, not lifecycle: a client the operator switched
// off below its quota must stay off, while one the quota itself switched off is
// restored. The two are indistinguishable from the enable flags alone, so the
// quota/usage pair decides before the counters are zeroed.
func TestBulkResetTrafficKeepsOperatorDisabledClientOff(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	ib := seedLocalDisabledClient(t, svc, 52013, "", "banned-bulk@x", 100000, 0, 600, 500)

	if _, err := svc.BulkResetTraffic(inboundSvc, []string{"banned-bulk@x"}); err != nil {
		t.Fatalf("BulkResetTraffic: %v", err)
	}

	assertEnableEverywhere(t, svc, inboundSvc, ib.Id, "banned-bulk@x", false)
	if tr := trafficOf(t, "banned-bulk@x"); tr.Up != 0 || tr.Down != 0 {
		t.Fatalf("counters = up %d / down %d, want 0/0", tr.Up, tr.Down)
	}
}

// The same reset must still revive the client when the quota is what switched
// it off, otherwise the guard above would break re-enabling.
func TestBulkResetTrafficReenablesQuotaDisabledClient(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	ib := seedLocalDisabledClient(t, svc, 52014, "", "spent-bulk@x", 1000, 0, 600, 500)

	if _, err := svc.BulkResetTraffic(inboundSvc, []string{"spent-bulk@x"}); err != nil {
		t.Fatalf("BulkResetTraffic: %v", err)
	}

	assertEnableEverywhere(t, svc, inboundSvc, ib.Id, "spent-bulk@x", true)
}

// resetClientTrafficLocked used to force client_traffics.enable=true even when
// the settings entry stayed disabled, leaving the panel showing an off client
// that accounting treated as on.
func TestResetClientTrafficLeavesOperatorDisabledRowOff(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}

	ib := seedLocalDisabledClient(t, svc, 52015, "", "banned-single@x", 100000, 0, 600, 500)

	if _, err := (&InboundService{}).ResetClientTraffic(ib.Id, "banned-single@x"); err != nil {
		t.Fatalf("ResetClientTraffic: %v", err)
	}

	if tr := trafficOf(t, "banned-single@x"); tr.Enable {
		t.Fatal("client_traffics.enable = true after resetting an operator-disabled client")
	} else if tr.Up != 0 || tr.Down != 0 {
		t.Fatalf("counters = up %d / down %d, want 0/0", tr.Up, tr.Down)
	}
}

func TestResetAllClientTraffics_PreservesManualDisable(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	manualOff := "manual-off@x"
	quotaOff := "quota-off@x"
	normalOn := "normal-on@x"

	clients := []model.Client{
		{Email: manualOff, ID: "11111111-1111-1111-1111-111111111111", SubID: manualOff, Enable: false, TotalGB: 1000},
		{Email: quotaOff, ID: "22222222-2222-2222-2222-222222222222", SubID: quotaOff, Enable: false, TotalGB: 100},
		{Email: normalOn, ID: "33333333-3333-3333-3333-333333333333", SubID: normalOn, Enable: true, TotalGB: 1000},
	}
	ib := mkInbound(t, 53010, model.VLESS, clientsSettings(t, clients))
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}

	// manualOff: Total=1000, Up=10, Down=10 (not depleted, manually disabled)
	mkTraffic(t, ib.Id, manualOff, 10, 10, 1000, 0, false)
	// quotaOff: Total=100, Up=50, Down=50 (depleted: 100 >= 100)
	mkTraffic(t, ib.Id, quotaOff, 50, 50, 100, 0, false)
	// normalOn: Total=1000, Up=50, Down=50, Enable=true
	mkTraffic(t, ib.Id, normalOn, 50, 50, 1000, 0, true)

	if _, err := svc.ResetAllClientTraffics(inboundSvc, ib.Id); err != nil {
		t.Fatalf("ResetAllClientTraffics: %v", err)
	}

	// manualOff must remain disabled
	if got := trafficOf(t, manualOff).Enable; got {
		t.Fatalf("manualOff: traffic.Enable = true, want false")
	}
	// quotaOff must be re-enabled
	if got := trafficOf(t, quotaOff).Enable; !got {
		t.Fatalf("quotaOff: traffic.Enable = false, want true")
	}
	// normalOn must remain enabled
	if got := trafficOf(t, normalOn).Enable; !got {
		t.Fatalf("normalOn: traffic.Enable = false, want true")
	}

	// Counters must be reset to 0
	for _, email := range []string{manualOff, quotaOff, normalOn} {
		tr := trafficOf(t, email)
		if tr.Up != 0 || tr.Down != 0 {
			t.Fatalf("%s: expected up=0 down=0, got up=%d down=%d", email, tr.Up, tr.Down)
		}
	}
}

func TestResetAllTraffics_PreservesManualDisable(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}

	manualOff := "all-manual-off@x"
	quotaOff := "all-quota-off@x"
	normalOn := "all-normal-on@x"

	clients := []model.Client{
		{Email: manualOff, ID: "44444444-4444-4444-4444-444444444444", SubID: manualOff, Enable: false, TotalGB: 1000},
		{Email: quotaOff, ID: "55555555-5555-5555-5555-555555555555", SubID: quotaOff, Enable: false, TotalGB: 100},
		{Email: normalOn, ID: "66666666-6666-6666-6666-666666666666", SubID: normalOn, Enable: true, TotalGB: 1000},
	}
	ib := mkInbound(t, 53011, model.VLESS, clientsSettings(t, clients))
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}

	mkTraffic(t, ib.Id, manualOff, 10, 10, 1000, 0, false)
	mkTraffic(t, ib.Id, quotaOff, 50, 50, 100, 0, false)
	mkTraffic(t, ib.Id, normalOn, 50, 50, 1000, 0, true)

	if _, err := svc.ResetAllTraffics(); err != nil {
		t.Fatalf("ResetAllTraffics: %v", err)
	}

	// manualOff must remain disabled
	if got := trafficOf(t, manualOff).Enable; got {
		t.Fatalf("manualOff: traffic.Enable = true, want false")
	}
	// quotaOff must be re-enabled
	if got := trafficOf(t, quotaOff).Enable; !got {
		t.Fatalf("quotaOff: traffic.Enable = false, want true")
	}
	// normalOn must remain enabled
	if got := trafficOf(t, normalOn).Enable; !got {
		t.Fatalf("normalOn: traffic.Enable = false, want true")
	}
}
