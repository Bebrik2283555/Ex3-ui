package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func TestWarpIntervalReportsClockPersistenceFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	db := database.GetDB()
	for _, setting := range []*model.Setting{
		{Key: "warpUpdateInterval", Value: "0"},
		{Key: "warpLastUpdate", Value: "0"},
	} {
		if err := db.Create(setting).Error; err != nil {
			t.Fatalf("seed %s: %v", setting.Key, err)
		}
	}

	const callback = "test:fail_warp_clock_update"
	errInjected := errors.New("injected WARP clock persistence failure")
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		setting, ok := tx.Statement.Model.(*model.Setting)
		if ok && setting.Key == "warpLastUpdate" {
			tx.AddError(errInjected)
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callback); err != nil {
			t.Errorf("remove update callback: %v", err)
		}
	})

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewXraySettingController(engine.Group("/panel/api"))
	form := url.Values{"interval": {"7"}}
	req := httptest.NewRequest(http.MethodPost, "/panel/api/xray/warp/interval", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatalf("interval update reported success after clock persistence failure: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), errInjected.Error()) {
		t.Fatalf("response omitted clock persistence error: %s", w.Body.String())
	}
}

// enabled and prepend decide which subscription outbounds the core runs, so an edit
// must schedule a reload like the sibling handlers do.
func TestUpdateOutboundSubSchedulesXrayReload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	sub := &model.OutboundSubscription{Url: "https://provider.example/sub", TagPrefix: "p-", Enabled: true}
	if err := database.GetDB().Create(sub).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	a := NewXraySettingController(engine.Group("/panel/api"))
	a.XrayService.IsNeedRestartAndSetFalse()
	form := url.Values{"url": {"https://1.1.1.1/sub"}, "enabled": {"false"}}
	req := httptest.NewRequest(http.MethodPost, "/panel/api/xray/outbound-subs/"+strconv.Itoa(sub.Id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
	}
	if !a.XrayService.IsNeedRestartAndSetFalse() {
		t.Fatalf("disabling an outbound subscription did not schedule an Xray reload")
	}
}

func TestWarpControllerEnsureAndRemoveXrayRouting(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	t.Setenv("XRAY_LOCATION_ASSET", os.Getenv("XRAY_LOCATION_ASSET"))
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))

	baseTpl := `{
  "outbounds": [{"tag": "direct", "protocol": "freedom"}],
  "routing": {"domainStrategy": "AsIs", "rules": [{"type": "field", "ip": ["geoip:private"], "outboundTag": "blocked"}]}
}`
	if err := database.GetDB().Create(&model.Setting{Key: "xrayTemplateConfig", Value: baseTpl}).Error; err != nil {
		t.Fatalf("seed xrayTemplateConfig: %v", err)
	}

	wc := &WarpController{
		settingService: service.SettingService{},
		xrayService:    service.XrayService{},
	}
	if err := wc.ensureXrayRouting(); err != nil {
		t.Fatalf("ensureXrayRouting: %v", err)
	}

	gotStr, err := wc.settingService.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("GetXrayConfigTemplate: %v", err)
	}
	var got struct {
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal([]byte(service.UnwrapXrayTemplateConfig(gotStr)), &got); err != nil {
		t.Fatalf("unmarshal after ensure: %v", err)
	}
	if len(got.Outbounds) < 2 || got.Outbounds[len(got.Outbounds)-1]["tag"] != "warp-usque" {
		t.Fatalf("expected warp-usque outbound appended, got %+v", got.Outbounds)
	}
	hasWarpRule := false
	for _, r := range got.Routing.Rules {
		if r["outboundTag"] == "warp-usque" {
			hasWarpRule = true
		}
	}
	if !hasWarpRule {
		t.Fatalf("expected warp-usque routing rule present, got %+v", got.Routing.Rules)
	}

	wc.removeXrayRouting()
	afterStr, err := wc.settingService.GetXrayConfigTemplate()
	if err != nil {
		t.Fatalf("GetXrayConfigTemplate after remove: %v", err)
	}
	var after struct {
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal([]byte(service.UnwrapXrayTemplateConfig(afterStr)), &after); err != nil {
		t.Fatalf("unmarshal after remove: %v", err)
	}
	for _, ob := range after.Outbounds {
		if ob["tag"] == "warp-usque" {
			t.Fatalf("expected warp-usque outbound removed, got %+v", after.Outbounds)
		}
	}
	for _, r := range after.Routing.Rules {
		if r["outboundTag"] == "warp-usque" {
			t.Fatalf("expected warp-usque routing rule removed, got %+v", after.Routing.Rules)
		}
	}
}
