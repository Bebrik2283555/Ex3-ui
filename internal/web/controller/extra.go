package controller

import (
	"errors"
	"os"
	"runtime"
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/extra"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// ExtraController manages the extra cores (qwdtt, olcRTC) as managed sidecars.
type ExtraController struct {
	extraManager *extra.Manager
}

// NewExtraController wires the extra-core routes into the /panel/api group.
func NewExtraController(g *gin.RouterGroup, settingService *service.SettingService) *ExtraController {
	a := &ExtraController{
		extraManager: extra.Instance(service.NewSettingStoreAdapter(*settingService)),
	}
	a.initRouter(g)
	return a
}

func (a *ExtraController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/extra")

	g.GET("/services", a.listServices)
	g.GET("/services/:name/status", a.serviceStatus)
	g.GET("/services/:name/config", a.serviceConfig)
	g.PUT("/services/:name/config", a.saveServiceConfig)
	g.POST("/services/:name/start", a.startService)
	g.POST("/services/:name/stop", a.stopService)
	g.POST("/services/:name/restart", a.restartService)
	g.GET("/services/:name/logs", a.serviceLogs)
	g.POST("/services/:name/upload", a.uploadBinary)
	g.POST("/services/:name/download", a.downloadBinary)
	g.DELETE("/services/:name/binary", a.deleteBinary)
}

func (a *ExtraController) parseName(c *gin.Context) (extra.Name, bool) {
	name := extra.Name(c.Param("name"))
	if !name.Valid() {
		jsonMsg(c, I18nWeb(c, "pages.extras.invalidCoreName"), errors.New("unknown core name"))
		return "", false
	}
	return name, true
}

// listServices returns status for every extra core.
func (a *ExtraController) listServices(c *gin.Context) {
	jsonObj(c, a.extraManager.AllStatuses(), nil)
}

func (a *ExtraController) serviceStatus(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	jsonObj(c, a.extraManager.StatusOf(name), nil)
}

func (a *ExtraController) serviceConfig(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	cfg, err := a.extraManager.LoadConfig(name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.extras.configLoadFailed"), err)
		return
	}
	jsonObj(c, cfg, nil)
}

func (a *ExtraController) saveServiceConfig(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	form := &extra.Config{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	if err := a.extraManager.SaveConfig(name, *form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.extras.configSaveFailed"), err)
		return
	}
	jsonObj(c, a.extraManager.StatusOf(name), nil)
}

func (a *ExtraController) startService(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	err := a.extraManager.Start(name)
	jsonMsg(c, I18nWeb(c, "pages.extras.toasts.started"), err)
}

func (a *ExtraController) stopService(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	err := a.extraManager.Stop(name)
	jsonMsg(c, I18nWeb(c, "pages.extras.toasts.stopped"), err)
}

func (a *ExtraController) restartService(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	err := a.extraManager.Restart(name)
	jsonMsg(c, I18nWeb(c, "pages.extras.toasts.restarted"), err)
}

func (a *ExtraController) serviceLogs(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	lines := 200
	if n := c.Query("lines"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			lines = parsed
		}
	}
	jsonObj(c, a.extraManager.Logs(name, lines), nil)
}

// uploadBinary replaces the core binary on disk (bin/extra-<name>).
func (a *ExtraController) uploadBinary(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.extras.uploadFailed"), err)
		return
	}
	dst := name.DefaultBinaryPath()
	// A running core executes its binary in place; opening dst for write
	// then fails with ETXTBSY ("text file busy"). Write to a temp file and
	// rename it over dst: rename replaces the path atomically while the old
	// inode keeps executing, and the new binary applies on the next restart.
	tmp := dst + ".upload"
	if err := c.SaveUploadedFile(file, tmp); err != nil {
		logger.Warning("save uploaded binary failed:", err)
		jsonMsg(c, I18nWeb(c, "pages.extras.uploadFailed"), err)
		return
	}
	// Uploaded cores must be executable on disk or exec fails with
	// "permission denied". SaveUploadedFile writes with default (0644) perms.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			logger.Warning("chmod uploaded binary failed:", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		logger.Warning("replace uploaded binary failed:", err)
		jsonMsg(c, I18nWeb(c, "pages.extras.uploadFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.extras.uploadSuccess"), nil)
}

// downloadBinary fetches the core binary from a public URL (file host, Google
// Drive, CDN) and saves it to bin/extra-<name>, chmod 755.
func (a *ExtraController) downloadBinary(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	form := &urlForm{}
	if !middleware.BindAndValidateInto(c, form) {
		return
	}
	dst := name.DefaultBinaryPath()
	if err := util.DownloadTo(form.URL, dst, 0o755); err != nil {
		logger.Warning("download binary failed:", err)
		jsonMsg(c, I18nWeb(c, "pages.extras.downloadFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.extras.downloadSuccess"), nil)
}

// deleteBinary stops the core and removes its binary from disk.
func (a *ExtraController) deleteBinary(c *gin.Context) {
	name, ok := a.parseName(c)
	if !ok {
		return
	}
	_ = a.extraManager.Stop(name)
	dst := name.DefaultBinaryPath()
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		jsonMsg(c, I18nWeb(c, "pages.extras.deleteFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.extras.deleteSuccess"), nil)
}
