package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/hostsfile"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"

	"github.com/gin-gonic/gin"
)

// HostsFileController manages the system hosts file (/etc/hosts), used to pin
// domains for the qwdtt/olcRTC cores.
type HostsFileController struct{}

// NewHostsFileController wires the hosts-file routes into /panel/api.
func NewHostsFileController(g *gin.RouterGroup) *HostsFileController {
	a := &HostsFileController{}
	a.initRouter(g)
	return a
}

func (a *HostsFileController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/hostsfile")

	g.GET("", a.get)
	g.PUT("", a.put)
	g.POST("/download", a.download)
}

// get returns the parsed /etc/hosts.
func (a *HostsFileController) get(c *gin.Context) {
	file, err := hostsfile.Get()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.hostsfile.getFailed"), err)
		return
	}
	jsonObj(c, file, nil)
}

func (a *HostsFileController) put(c *gin.Context) {
	form := &hostsfile.HostsFile{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if err := hostsfile.Set(form.Raw); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.hostsfile.setFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.hostsfile.setSuccess"), nil)
}

func (a *HostsFileController) download(c *gin.Context) {
	form := &urlForm{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if err := hostsfile.SetFromURL(form.URL); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.hostsfile.setFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.hostsfile.setSuccess"), nil)
}
