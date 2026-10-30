package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// The purge and reset paths compare emails with LOWER(); a traffic read must
// not miss a row that the panel itself treats as the same client.
func TestGetClientTrafficByEmailMatchesOtherCase(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	const stored = "Mixed.Case@X"
	source := []model.Client{
		{Email: stored, ID: "11111111-1111-1111-1111-111111111111", SubID: "mixed", Enable: true},
	}
	ib := mkInbound(t, 52021, model.VLESS, clientsSettings(t, source))
	if err := svc.SyncInbound(nil, ib.Id, source); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}
	mkTraffic(t, ib.Id, stored, 7, 9, 0, 0, true)

	tr, err := inboundSvc.GetClientTrafficByEmail("mixed.case@x")
	if err != nil {
		t.Fatalf("GetClientTrafficByEmail: %v", err)
	}
	if tr == nil {
		t.Fatal("traffic row was not found for a differently cased email")
	}
	if tr.Email != stored {
		t.Fatalf("email = %q, want the stored %q", tr.Email, stored)
	}
	if tr.Up != 7 || tr.Down != 9 {
		t.Fatalf("traffic = up %d / down %d, want 7/9", tr.Up, tr.Down)
	}
	if tr.UUID == "" || tr.SubId == "" {
		t.Fatalf("client identity not enriched: %+v", tr)
	}
}
