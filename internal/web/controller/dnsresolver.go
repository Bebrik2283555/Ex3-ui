package controller

import (
	"context"
	"errors"

	"github.com/mhsanaei/3x-ui/v3/internal/dnsresolver"

	"github.com/gin-gonic/gin"
)

// DnsResolverController manages the automatic DNS resolver for geo-unblocking.
type DnsResolverController struct{}

// NewDnsResolverController wires the dnsresolver routes into /panel/api.
func NewDnsResolverController(g *gin.RouterGroup) *DnsResolverController {
	a := &DnsResolverController{}
	a.initRouter(g)
	return a
}

func (a *DnsResolverController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/dnsresolver")

	g.GET("/status", a.getStatus)
	g.POST("/settings", a.saveSettings)
	g.POST("/domains", a.saveDomains)
	g.POST("/resolve", a.resolve)
	g.POST("/test", a.testDNS)
}

func (a *DnsResolverController) getStatus(c *gin.Context) {
	jsonObj(c, dnsresolver.GetStatus(), nil)
}

type dnsResolverDomainsForm struct {
	Domains []string `json:"domains"`
}

type dnsResolverTestForm struct {
	DnsServer string `json:"dnsServer"`
}

func (a *DnsResolverController) saveSettings(c *gin.Context) {
	var st dnsresolver.Settings
	if err := c.ShouldBindJSON(&st); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.invalidData"), err)
		return
	}
	if err := dnsresolver.SaveSettings(st); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.saveFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.saveSuccess"), nil)
}

func (a *DnsResolverController) saveDomains(c *gin.Context) {
	var form dnsResolverDomainsForm
	if err := c.ShouldBindJSON(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.invalidData"), err)
		return
	}
	if err := dnsresolver.SaveDomains(form.Domains); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.saveFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.domainsSaved"), nil)
}

func (a *DnsResolverController) resolve(c *gin.Context) {
	go func() {
		_ = dnsresolver.Resolve(context.Background())
	}()
	jsonMsg(c, I18nWeb(c, "pages.dnsresolver.toasts.resolveStarted"), nil)
}

func (a *DnsResolverController) testDNS(c *gin.Context) {
	var form dnsResolverTestForm
	_ = c.ShouldBindJSON(&form)
	res := dnsresolver.TestDNS(form.DnsServer)
	if !res.Success {
		jsonMsgObj(c, res.Error, res, errors.New(res.Error))
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.dnsresolver.testSuccess"), res, nil)
}
