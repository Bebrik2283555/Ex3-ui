package tgbot

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"github.com/mymmrac/telego"
)

func TestAnswerCallbackDeniesPrivilegedActionToNonAdmin(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a non-admin callback reached a privileged handler: %v", r)
		}
	}()

	tg := &Tgbot{}
	for _, data := range []string{"get_backup", "reset_all_traffics_c", "add_client", "onlines", "inbounds"} {
		q := &telego.CallbackQuery{
			Data:    data,
			From:    telego.User{ID: 999999},
			Message: &telego.Message{Chat: telego.Chat{ID: 1}},
		}
		tg.answerCallback(q, false)
	}
}

func TestIsClientSelfCallback(t *testing.T) {
	allowed := []string{"client_traffic", "client_sub_links", "client_qr_links", "client_sub_links alice@x"}
	for _, d := range allowed {
		if !isClientSelfCallback(d) {
			t.Errorf("%q should be a per-user client callback", d)
		}
	}
	denied := []string{"get_backup", "reset_all_traffics_c", "add_client", "onlines", "get_banlogs", "get_usage"}
	for _, d := range denied {
		if isClientSelfCallback(d) {
			t.Errorf("%q is an admin-only callback and must not be treated as per-user", d)
		}
	}
}

// TestClientOwnedBy guards the per-user email callbacks (client_sub_links
// <email> and friends) against IDOR: a non-admin must not reach another
// client's subscription/QR/individual links by naming a foreign email.
func TestClientOwnedBy(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	inbound := &model.Inbound{
		UserId:   1,
		Tag:      "tg-owner",
		Enable:   true,
		Port:     41011,
		Protocol: model.VLESS,
		Settings: `{"clients":[{"id":"u1","email":"alice@x","tgId":777},{"id":"u2","email":"bob@y","tgId":999}]}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	for _, email := range []string{"alice@x", "bob@y"} {
		if err := db.Create(&xray.ClientTraffic{InboundId: inbound.Id, Email: email, Enable: true, Up: 1, Down: 2}).Error; err != nil {
			t.Fatalf("create client_traffics for %s: %v", email, err)
		}
	}

	svc := service.InboundService{}
	tg := &Tgbot{inboundService: svc}
	if !tg.clientOwnedBy(777, "alice@x") {
		t.Error("owner must see their own client")
	}
	if tg.clientOwnedBy(777, "bob@y") {
		t.Error("owner must NOT see another client's links")
	}
	if tg.clientOwnedBy(777, "nobody@z") {
		t.Error("unknown email must be rejected")
	}
}
