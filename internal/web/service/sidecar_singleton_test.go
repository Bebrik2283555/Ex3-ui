package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// qWDTT and CSQTT allow exactly one inbound per panel; duplicates stay editable
// only while they are disabled so an operator can clean them up.
func TestValidateSingletonSidecar(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	existing := &model.Inbound{Tag: "qwdtt-one", Protocol: model.QWDTT, Enable: true, Settings: `{}`}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create qwdtt inbound: %v", err)
	}

	if err := validateSingletonSidecar(db, &model.Inbound{Protocol: model.QWDTT}, 0, true); err == nil {
		t.Fatal("second enabled qWDTT inbound was allowed")
	}
	if err := validateSingletonSidecar(db, &model.Inbound{Protocol: model.QWDTT}, existing.Id, false); err != nil {
		t.Fatalf("disabled duplicate should stay editable: %v", err)
	}
	if err := validateSingletonSidecar(db, &model.Inbound{Protocol: model.CSQTT}, 0, true); err != nil {
		t.Fatalf("different singleton protocol should be allowed: %v", err)
	}

	existing.Protocol = model.CSQTT
	if err := db.Save(existing).Error; err != nil {
		t.Fatalf("convert inbound: %v", err)
	}
	if err := validateSingletonSidecar(db, &model.Inbound{Protocol: model.CSQTT}, 0, true); err == nil {
		t.Fatal("second enabled CSQTT inbound was allowed")
	}
}
