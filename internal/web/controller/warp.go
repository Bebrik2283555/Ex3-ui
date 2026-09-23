package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/warp"

	"github.com/gin-gonic/gin"
)

// WarpController manages the Cloudflare WARP tunnel via usque.
type WarpController struct{}

// NewWarpController wires the warp routes into /panel/api.
func NewWarpController(g *gin.RouterGroup) *WarpController {
	a := &WarpController{}
	a.initRouter(g)
	return a
}

func (a *WarpController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/warp")

	g.GET("/status", a.getStatus)
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
	jsonObj(c, warp.GetStatus(), nil)
}

func (a *WarpController) install(c *gin.Context) {
	if err := warp.Install(); err != nil {
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
