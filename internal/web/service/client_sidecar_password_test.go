package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// New CSQTT clients always get their own credential; qWDTT keeps the generic
// password fallback for older records.
func TestFillProtocolDefaultsSidecarPasswords(t *testing.T) {
	cs := &ClientService{}

	qwdttNew := model.Client{Email: "q-new@x"}
	if err := cs.fillProtocolDefaults(&qwdttNew, &model.Inbound{Protocol: model.QWDTT}); err != nil {
		t.Fatalf("fillProtocolDefaults(qwdtt new): %v", err)
	}
	if qwdttNew.QwdttPassword == "" {
		t.Fatal("new qWDTT client did not get a password")
	}

	qwdttLegacy := model.Client{Email: "q-old@x", Password: "legacy-q"}
	if err := cs.fillProtocolDefaults(&qwdttLegacy, &model.Inbound{Protocol: model.QWDTT}); err != nil {
		t.Fatalf("fillProtocolDefaults(qwdtt legacy): %v", err)
	}
	if qwdttLegacy.QwdttPassword != "" {
		t.Fatalf("legacy qWDTT client got a new password %q; generic fallback must stay intact", qwdttLegacy.QwdttPassword)
	}

	csqttNew := model.Client{Email: "c-new@x"}
	if err := cs.fillProtocolDefaults(&csqttNew, &model.Inbound{Protocol: model.CSQTT}); err != nil {
		t.Fatalf("fillProtocolDefaults(csqtt new): %v", err)
	}
	if csqttNew.CsqttPassword == "" {
		t.Fatal("new CSQTT client did not get a password")
	}

	csqttGeneric := model.Client{Email: "c-old@x", Password: "legacy-c"}
	if err := cs.fillProtocolDefaults(&csqttGeneric, &model.Inbound{Protocol: model.CSQTT}); err != nil {
		t.Fatalf("fillProtocolDefaults(csqtt generic): %v", err)
	}
	if csqttGeneric.CsqttPassword == "" {
		t.Fatal("CSQTT client with only a generic password did not get its own password")
	}
	if csqttGeneric.CsqttPassword == csqttGeneric.Password {
		t.Fatal("CSQTT password must not reuse the generic password")
	}
}

// Inbound settings are the source the client editor writes, but link generation
// reads client_inbounds. Syncing an inbound must therefore persist both sidecar
// credentials on the shared client record.
func TestSyncInboundPersistsSidecarPasswords(t *testing.T) {
	setupBulkDB(t)
	client := model.Client{
		Email:         "sidecar-sync@x",
		Password:      "main-pass",
		QwdttPassword: "q-pass",
		CsqttPassword: "c-pass",
		Enable:        true,
	}
	ib := mkInbound(t, 56000, model.QWDTT, clientsSettings(t, []model.Client{client}))
	if err := (&ClientService{}).SyncInbound(nil, ib.Id, []model.Client{client}); err != nil {
		t.Fatalf("SyncInbound: %v", err)
	}

	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", client.Email).First(&rec).Error; err != nil {
		t.Fatalf("load client record: %v", err)
	}
	if rec.QwdttPassword != "q-pass" || rec.CsqttPassword != "c-pass" {
		t.Fatalf("sidecar passwords not persisted: qwdtt=%q csqtt=%q", rec.QwdttPassword, rec.CsqttPassword)
	}
}
