package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/warp"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// WarpController manages the Cloudflare WARP tunnel via usque.
type WarpController struct {
	settingService service.SettingService
	xrayService    service.XrayService
}

// NewWarpController wires the warp routes into /panel/api.
func NewWarpController(g *gin.RouterGroup) *WarpController {
	a := &WarpController{
		settingService: service.SettingService{},
		xrayService:    service.XrayService{},
	}
	a.initRouter(g)
	return a
}

func (a *WarpController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/warp")

	g.GET("/status", a.getStatus)
	g.GET("/settings", a.getSettings)
	g.POST("/settings", a.saveSettings)
	g.POST("/install", a.install)
	g.POST("/uninstall", a.uninstall)
	g.POST("/start", a.start)
	g.POST("/stop", a.stop)
	g.POST("/restart", a.restart)
	g.POST("/rotate", a.rotate)
	g.GET("/logs", a.getLogs)
	g.GET("/install-logs", a.getInstallLogs)
}

func (a *WarpController) getStatus(c *gin.Context) {
	st := warp.GetStatus()
	if st.Installed {
		_ = a.ensureXrayOutbound()
	}
	jsonObj(c, st, nil)
}

func (a *WarpController) getSettings(c *gin.Context) {
	jsonObj(c, warp.GetSettings(), nil)
}

func (a *WarpController) bindSettings(c *gin.Context) (warp.Settings, error) {
	var st warp.Settings
	data, err := c.GetRawData()
	if err != nil || len(data) == 0 {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err == nil {
		return st, nil
	}
	vals, err := url.ParseQuery(string(data))
	if err == nil && len(vals) > 0 {
		if timeoutStr := vals.Get("timeout"); timeoutStr != "" {
			st.Timeout, _ = strconv.Atoi(timeoutStr)
		}
		if intStr := vals.Get("checkInterval"); intStr != "" {
			st.CheckInterval, _ = strconv.Atoi(intStr)
		}
		if u := vals.Get("checkUrl"); u != "" {
			st.CheckUrl = u
		}
		if ar := vals.Get("autoRotate"); ar != "" {
			st.AutoRotate = ar == "true" || ar == "1"
		}
		for _, d := range vals["domains"] {
			d = strings.TrimSpace(d)
			if d != "" {
				st.Domains = append(st.Domains, d)
			}
		}
		return st, nil
	}
	return st, errors.New("invalid request body")
}

func (a *WarpController) saveSettings(c *gin.Context) {
	st, err := a.bindSettings(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.settingsFailed"), err)
		return
	}
	if err := warp.SaveSettings(st); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.settingsFailed"), err)
		return
	}
	if err := a.ensureXrayOutbound(); err != nil {
		logger.Warning("ensure warp outbound in xray failed:", err)
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.settingsSuccess"), nil)
}

func (a *WarpController) install(c *gin.Context) {
	var pSt *warp.Settings
	if st, err := a.bindSettings(c); err == nil {
		pSt = &st
	}
	if err := a.ensureXrayRouting(); err != nil {
		logger.Warning("ensure warp routing in xray failed:", err)
	}
	if err := warp.Install(pSt); err != nil {
		if errors.Is(err, warp.ErrAlreadyRunning) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": I18nWeb(c, "pages.warp.installRunning")})
			return
		}
		jsonMsg(c, I18nWeb(c, "pages.warp.installFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.installStarted"), nil)
}

func (a *WarpController) uninstall(c *gin.Context) {
	a.removeXrayRouting()
	if err := warp.Uninstall(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.uninstallFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.uninstallSuccess"), nil)
}

func (a *WarpController) start(c *gin.Context) {
	if err := warp.Start(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.startFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.startSuccess"), nil)
}

func (a *WarpController) stop(c *gin.Context) {
	if err := warp.Stop(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.stopFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.stopSuccess"), nil)
}

func (a *WarpController) restart(c *gin.Context) {
	if err := warp.Restart(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.warp.restartFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.restartSuccess"), nil)
}

func (a *WarpController) rotate(c *gin.Context) {
	if err := warp.RotateIP(); err != nil {
		if errors.Is(err, warp.ErrRotateRunning) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": I18nWeb(c, "pages.warp.rotateRunning")})
			return
		}
		jsonMsg(c, I18nWeb(c, "pages.warp.rotateFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.warp.rotateStarted"), nil)
}

func (a *WarpController) getLogs(c *gin.Context) {
	n := 200
	if s := c.Query("lines"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 {
			n = parsed
		}
	}
	jsonObj(c, warp.Logs(n), nil)
}

func (a *WarpController) getInstallLogs(c *gin.Context) {
	n := 100
	if s := c.Query("lines"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 {
			n = parsed
		}
	}
	jsonObj(c, warp.InstallLogs(n), nil)
}

func (a *WarpController) ensureXrayOutbound() error {
	templateStr, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	templateStr = service.UnwrapXrayTemplateConfig(templateStr)
	var template map[string]any
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		return err
	}

	outbounds, _ := template["outbounds"].([]any)
	modified := false
	hasOutbound := false

	var newOutbounds []any
	for _, o := range outbounds {
		if om, ok := o.(map[string]any); ok {
			tag, _ := om["tag"].(string)
			if tag == "warp-double" {
				modified = true
				continue
			}
			if tag == "warp-usque" {
				hasOutbound = true
				ss, _ := om["streamSettings"].(map[string]any)
				if ss == nil {
					ss = make(map[string]any)
					om["streamSettings"] = ss
					modified = true
				}
				sockopt, _ := ss["sockopt"].(map[string]any)
				if sockopt == nil {
					sockopt = make(map[string]any)
					ss["sockopt"] = sockopt
					modified = true
				}
				if sockopt["domainStrategy"] != "UseIPv4" {
					sockopt["domainStrategy"] = "UseIPv4"
					modified = true
				}
			}
		}
		newOutbounds = append(newOutbounds, o)
	}

	if !hasOutbound {
		warpOutbound := map[string]any{
			"tag":      "warp-usque",
			"protocol": "freedom",
			"streamSettings": map[string]any{
				"sockopt": map[string]any{
					"mark":           10000,
					"domainStrategy": "UseIPv4",
				},
			},
		}
		newOutbounds = append(newOutbounds, warpOutbound)
		modified = true
	}
	template["outbounds"] = newOutbounds

	if modified {
		updatedBytes, err := json.MarshalIndent(template, "", "  ")
		if err != nil {
			return err
		}

		xraySettingSvc := service.XraySettingService{}
		if err := xraySettingSvc.SaveXraySetting(string(updatedBytes)); err != nil {
			return err
		}
		_ = a.xrayService.RestartXray(false)
	}
	return nil
}

func (a *WarpController) ensureXrayRouting() error {
	templateStr, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	templateStr = service.UnwrapXrayTemplateConfig(templateStr)
	var template map[string]any
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		return err
	}

	modified := false

	// 1. Ensure warp-usque outbound exists with UseIPv4
	outbounds, _ := template["outbounds"].([]any)
	hasOutbound := false
	var newOutbounds []any
	for _, o := range outbounds {
		if om, ok := o.(map[string]any); ok {
			tag, _ := om["tag"].(string)
			if tag == "warp-double" {
				modified = true
				continue
			}
			if tag == "warp-usque" {
				hasOutbound = true
				ss, _ := om["streamSettings"].(map[string]any)
				if ss == nil {
					ss = make(map[string]any)
					om["streamSettings"] = ss
					modified = true
				}
				sockopt, _ := ss["sockopt"].(map[string]any)
				if sockopt == nil {
					sockopt = make(map[string]any)
					ss["sockopt"] = sockopt
					modified = true
				}
				if sockopt["domainStrategy"] != "UseIPv4" {
					sockopt["domainStrategy"] = "UseIPv4"
					modified = true
				}
			}
		}
		newOutbounds = append(newOutbounds, o)
	}
	if !hasOutbound {
		warpOutbound := map[string]any{
			"tag":      "warp-usque",
			"protocol": "freedom",
			"streamSettings": map[string]any{
				"sockopt": map[string]any{
					"mark":           10000,
					"domainStrategy": "UseIPv4",
				},
			},
		}
		newOutbounds = append(newOutbounds, warpOutbound)
		modified = true
	}
	template["outbounds"] = newOutbounds

	// 2. Ensure warp routing rule exists for warp-usque
	routing, _ := template["routing"].(map[string]any)
	if routing == nil {
		routing = make(map[string]any)
		template["routing"] = routing
	}
	rules, _ := routing["rules"].([]any)
	hasRule := false
	for _, r := range rules {
		if rm, ok := r.(map[string]any); ok {
			curTag, _ := rm["outboundTag"].(string)
			if curTag == "warp-usque" || curTag == "warp-double" {
				hasRule = true
				if curTag != "warp-usque" {
					rm["outboundTag"] = "warp-usque"
					modified = true
				}
				break
			}
		}
	}

	if !hasRule {
		cleanDomains := make([]string, 0, len(warp.DefaultTelegramDomains))
		for _, d := range warp.DefaultTelegramDomains {
			cleanDomains = append(cleanDomains, "domain:"+d)
		}
		warpRule := map[string]any{
			"type":        "field",
			"outboundTag": "warp-usque",
			"domain":      cleanDomains,
		}
		routing["rules"] = append([]any{warpRule}, rules...)
		modified = true
	}

	if modified {
		updatedBytes, err := json.MarshalIndent(template, "", "  ")
		if err != nil {
			return err
		}
		xraySettingSvc := service.XraySettingService{}
		if err := xraySettingSvc.SaveXraySetting(string(updatedBytes)); err != nil {
			return err
		}
		_ = a.xrayService.RestartXray(false)
	}
	return nil
}

func (a *WarpController) removeXrayRouting() {
	templateStr, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		logger.Warning("removeXrayRouting: get template failed:", err)
		return
	}
	templateStr = service.UnwrapXrayTemplateConfig(templateStr)
	var template map[string]any
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		logger.Warning("removeXrayRouting: unmarshal template failed:", err)
		return
	}
	changed := false
	if outbounds, ok := template["outbounds"].([]any); ok {
		var newOutbounds []any
		for _, o := range outbounds {
			if om, ok := o.(map[string]any); !ok || (om["tag"] != "warp-usque" && om["tag"] != "warp-double") {
				newOutbounds = append(newOutbounds, o)
			} else {
				changed = true
			}
		}
		template["outbounds"] = newOutbounds
	}
	if routing, ok := template["routing"].(map[string]any); ok {
		if rules, ok := routing["rules"].([]any); ok {
			var newRules []any
			for _, r := range rules {
				if rm, ok := r.(map[string]any); !ok || (rm["outboundTag"] != "warp-usque" && rm["outboundTag"] != "warp-double") {
					newRules = append(newRules, r)
				} else {
					changed = true
				}
			}
			routing["rules"] = newRules
		}
	}
	if changed {
		updatedBytes, err := json.MarshalIndent(template, "", "  ")
		if err != nil {
			logger.Warning("removeXrayRouting: marshal template failed:", err)
			return
		}
		xraySettingSvc := service.XraySettingService{}
		if err := xraySettingSvc.SaveXraySetting(string(updatedBytes)); err != nil {
			logger.Warning("removeXrayRouting: save template failed:", err)
			return
		}
		_ = a.xrayService.RestartXray(false)
	}
}
