package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// CoreController exposes the sidecar cores shown in Settings → Cores.
type CoreController struct {
	coreService service.CoreService
}

// NewCoreController wires the core management routes into /panel/api.
func NewCoreController(g *gin.RouterGroup) *CoreController {
	a := &CoreController{}
	a.initRouter(g)
	return a
}

func (a *CoreController) initRouter(g *gin.RouterGroup) {
	g.GET("/cores", a.list)
	g.GET("/cores/:name/details", a.details)
	g.GET("/cores/:name/logs", a.details)
	g.POST("/cores/:name/start", a.start)
	g.POST("/cores/:name/stop", a.stop)
	g.POST("/cores/:name/restart", a.restart)
}

func (a *CoreController) list(c *gin.Context) {
	rows, err := a.coreService.List()
	jsonObj(c, rows, err)
}

func (a *CoreController) details(c *gin.Context) {
	details, err := a.coreService.Details(c.Param("name"))
	jsonObj(c, details, err)
}

func (a *CoreController) start(c *gin.Context) {
	err := a.coreService.Start(c.Param("name"))
	jsonMsg(c, "core started", err)
}

func (a *CoreController) stop(c *gin.Context) {
	err := a.coreService.Stop(c.Param("name"))
	jsonMsg(c, "core stopped", err)
}

func (a *CoreController) restart(c *gin.Context) {
	err := a.coreService.Restart(c.Param("name"))
	jsonMsg(c, "core restarted", err)
}
