package sub

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A subscription whose only inbound is a sidecar used to 404 because the
// getInboundsBySubId query only listed Xray protocols. Sidecar-only clients
// must resolve their inbound so sidecar.LinkFor can append qwdtt/olcrtc/csqtt
// links to the URI subscription.
func TestGetInboundsBySubIdIncludesSidecars(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()

	for _, protocol := range []model.Protocol{model.QWDTT, model.OLCRTC, model.CSQTT} {
		inbound := &model.Inbound{Tag: "tag-" + string(protocol), Protocol: protocol, Enable: true, Port: 56000, Settings: `{}`}
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("create %s inbound: %v", protocol, err)
		}
		rec := &model.ClientRecord{Email: string(protocol) + "@example.com", SubID: "sub-" + string(protocol), Enable: true}
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("create %s client: %v", protocol, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("link %s client: %v", protocol, err)
		}
	}

	svc := &SubService{}
	for _, protocol := range []model.Protocol{model.QWDTT, model.OLCRTC, model.CSQTT} {
		got, err := svc.getInboundsBySubId("sub-" + string(protocol))
		if err != nil {
			t.Fatalf("getInboundsBySubId(%s): %v", protocol, err)
		}
		if len(got) != 1 || got[0].Protocol != protocol {
			t.Fatalf("getInboundsBySubId(%s) = %+v, want one %s inbound", protocol, got, protocol)
		}
	}
}

// qWDTT/olcRTC/CSQTT keep their credentials on the inbound, so the direct
// Links tab must work even when no client rows are attached.
func TestInboundLinksSidecarWithoutClients(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))

	inbound := &model.Inbound{
		Tag:      "qwdtt-clientless",
		Protocol: model.QWDTT,
		Enable:   true,
		Port:     56000,
		Settings: `{"sidecar":{"listenAddr":"0.0.0.0:56000","mainPassword":"secret","subHost":"vpn.example.com:56000"}}`,
	}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	links := (&SubService{}).inboundLinks(inbound)
	if len(links) != 1 {
		t.Fatalf("inboundLinks() = %q, want one qwdtt link", links)
	}
	if !strings.HasPrefix(links[0], "qwdtt://config?") ||
		!strings.Contains(links[0], "peer=vpn.example.com%3A56000") ||
		!strings.Contains(links[0], "pass=secret") {
		t.Fatalf("inboundLinks() = %q, want peer/pass from inbound settings", links[0])
	}
}

func TestThroneSubscriptionIncludesOLCRTCName(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()

	key := strings.Repeat("ab", 32)
	inbound := &model.Inbound{
		Tag:      "in-0-any",
		Remark:   "new",
		Protocol: model.OLCRTC,
		Enable:   true,
		Port:     0,
		Settings: `{"clients":[{"email":"Me","password":"pw","enable":true}],"sidecar":{"provider":"vkcalls","roomId":"https://vk.ru/call/join/room1","cryptoKey":"` + key + `","transport":"vp8channel"}}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create olcrtc inbound: %v", err)
	}
	rec := &model.ClientRecord{Email: "Me", SubID: "sub-throne", Enable: true}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("link client: %v", err)
	}

	req := (&SubService{}).ForRequest("sub.example.com")
	req.forceSidecars = true
	links, _, _, _, err := req.getSubs("sub-throne")
	if err != nil {
		t.Fatalf("getSubs: %v", err)
	}
	if len(links) != 1 || !strings.HasSuffix(links[0], "#"+key+"$new") {
		t.Fatalf("throne olcrtc links = %q, want suffix #%s$new", links, key)
	}
}
