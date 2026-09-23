package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/optimize"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"

	"github.com/gin-gonic/gin"
)

// OptimizeController exposes the one-shot system optimization actions.
type OptimizeController struct{}

// NewOptimizeController wires the optimization routes into /panel/api.
func NewOptimizeController(g *gin.RouterGroup) *OptimizeController {
	a := &OptimizeController{}
	a.initRouter(g)
	return a
}

func (a *OptimizeController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/optimize")

	g.GET("/status", a.getStatus)
	g.POST("/apply", a.apply)
	g.POST("/revert", a.revert)
}

// getStatus reports which optimizations are already applied.
func (a *OptimizeController) getStatus(c *gin.Context) {
	jsonObj(c, optimize.GetStatus(), nil)
}

func (a *OptimizeController) apply(c *gin.Context) {
	form := &optimize.Options{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	steps, err := optimize.Apply(*form)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.optimize.applyFailed"), err)
		return
	}
	jsonObj(c, steps, nil)
}

func (a *OptimizeController) revert(c *gin.Context) {
	if err := optimize.Revert(); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.optimize.revertFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.optimize.revertSuccess"), nil)
}
