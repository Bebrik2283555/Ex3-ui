package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
	"github.com/mhsanaei/3x-ui/v3/internal/zapret"

	"github.com/gin-gonic/gin"
)

// ZapretController manages the transparent DPI-bypass service and its domain lists.
type ZapretController struct{}

type zapretInstallForm struct {
	Firewall string `json:"firewall"`
	IfaceWan string `json:"ifaceWan"`
	IfaceLan string `json:"ifaceLan"`
}

type zapretDownloadForm struct {
	URL      string `json:"url" binding:"required"`
	Firewall string `json:"firewall"`
	IfaceWan string `json:"ifaceWan"`
	IfaceLan string `json:"ifaceLan"`
}

// NewZapretController wires the zapret routes into /panel/api.
func NewZapretController(g *gin.RouterGroup) *ZapretController {
	a := &ZapretController{}
	a.initRouter(g)
	return a
}

func (a *ZapretController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/zapret")

	g.GET("/status", a.getStatus)
	g.POST("/install", a.install)
	g.POST("/download", a.downloadInstall)
	g.POST("/uninstall", a.uninstall)
	g.POST("/start", a.start)
	g.POST("/stop", a.stop)
	g.POST("/restart", a.restart)
	g.GET("/hosts", a.getHosts)
	g.PUT("/hosts", a.setHosts)
	g.GET("/logs", a.getLogs)
}

func (a *ZapretController) getStatus(c *gin.Context) {
	jsonObj(c, zapret.GetStatus(), nil)
}

func (a *ZapretController) install(c *gin.Context) {
	form := &zapretInstallForm{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if form.Firewall == "" {
		form.Firewall = "nftables"
	}
	if err := zapret.Install(form.Firewall, form.IfaceWan, form.IfaceLan); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.installFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.installSuccess"), nil)
}

func (a *ZapretController) downloadInstall(c *gin.Context) {
	form := &zapretDownloadForm{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if form.Firewall == "" {
		form.Firewall = "nftables"
	}
	if err := zapret.InstallFromZip(form.URL, form.Firewall, form.IfaceWan, form.IfaceLan); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.installFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.installSuccess"), nil)
}

func (a *ZapretController) uninstall(c *gin.Context) {
	if err := zapret.Uninstall(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.uninstallFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.uninstallSuccess"), nil)
}

func (a *ZapretController) start(c *gin.Context) {
	if err := zapret.Start(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.startFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.startSuccess"), nil)
}

func (a *ZapretController) stop(c *gin.Context) {
	if err := zapret.Stop(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.stopFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.stopSuccess"), nil)
}

func (a *ZapretController) restart(c *gin.Context) {
	if err := zapret.Restart(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.restartFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.restartSuccess"), nil)
}

func (a *ZapretController) getHosts(c *gin.Context) {
	hosts, err := zapret.GetHosts()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.hostsFailed"), err)
		return
	}
	jsonObj(c, hosts, nil)
}

func (a *ZapretController) setHosts(c *gin.Context) {
	form := &zapret.Hosts{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if err := zapret.SetHosts(*form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.zapret.hostsFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.zapret.hostsSaved"), nil)
}

func (a *ZapretController) getLogs(c *gin.Context) {
	jsonObj(c, zapret.Logs(200), nil)
}
