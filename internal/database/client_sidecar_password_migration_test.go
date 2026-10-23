package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Older builds stored qWDTT/CSQTT credentials only in inbound settings, so the
// shared client records kept an empty sidecar column and link generation fell
// back to the main password. The startup backfill must copy them over without
// overwriting an already configured value.
func TestBackfillClientSidecarPasswords(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	rec := &model.ClientRecord{Email: "sidecar-migrate@x", SubID: "sidecar-migrate", Password: "main-pass"}
	if err := GetDB().Create(rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	ib := &model.Inbound{
		Tag:      "sidecar-migrate-qwdtt",
		Enable:   true,
		Port:     56000,
		Protocol: model.QWDTT,
		Settings: `{"clients":[{"email":"sidecar-migrate@x","qwdttPassword":"q-from-settings","csqttPassword":"c-from-settings"}]}`,
	}
	if err := GetDB().Create(ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	if err := backfillClientSidecarPasswords(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var got model.ClientRecord
	if err := GetDB().First(&got, rec.Id).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	if got.QwdttPassword != "q-from-settings" || got.CsqttPassword != "c-from-settings" {
		t.Fatalf("sidecar passwords not backfilled: qwdtt=%q csqtt=%q", got.QwdttPassword, got.CsqttPassword)
	}

	// An operator-edited value wins over the settings snapshot on later starts.
	if err := GetDB().Model(&model.ClientRecord{}).Where("id = ?", rec.Id).
		Update("qwdtt_password", "operator-set").Error; err != nil {
		t.Fatalf("set operator password: %v", err)
	}
	if err := backfillClientSidecarPasswords(); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if err := GetDB().First(&got, rec.Id).Error; err != nil {
		t.Fatalf("reload client: %v", err)
	}
	if got.QwdttPassword != "operator-set" {
		t.Fatalf("backfill overwrote existing password: %q", got.QwdttPassword)
	}
}
