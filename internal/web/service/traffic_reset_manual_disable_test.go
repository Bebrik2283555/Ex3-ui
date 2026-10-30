package service

import "testing"

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
