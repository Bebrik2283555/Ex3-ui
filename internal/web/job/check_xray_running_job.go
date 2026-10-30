// Package job provides background job implementations for the 3x-ui web panel,
// including traffic monitoring, system checks, and periodic maintenance tasks.
package job

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// EventBus is set from web layer to publish events.
var EventBus *eventbus.Bus

// A restart that kept the core alive this long is treated as healthy; a core
// that dies sooner is a crash loop (bad config, missing binary, OOM) and is
// retried with a growing delay instead of every two seconds forever.
const (
	xrayRestartStableWindow = 30 * time.Second
	xrayRestartMaxDelay     = time.Minute
)

// CheckXrayRunningJob monitors Xray process health and restarts it if it crashes.
type CheckXrayRunningJob struct {
	xrayService service.XrayService
	checkTime   int
	failures    int
	restartedAt time.Time
	retryAt     time.Time
}

// NewCheckXrayRunningJob creates a new Xray health check job instance.
func NewCheckXrayRunningJob() *CheckXrayRunningJob {
	return new(CheckXrayRunningJob)
}

// Run checks if Xray has crashed and restarts it after confirming it's down for 2 consecutive checks.
func (j *CheckXrayRunningJob) Run() {
	if !j.xrayService.DidXrayCrash() {
		j.checkTime = 0
		if time.Since(j.restartedAt) >= xrayRestartStableWindow {
			j.failures = 0
			j.retryAt = time.Time{}
		}
		return
	}
	j.checkTime++
	// only restart if it's down 2 times in a row
	if j.checkTime <= 1 {
		return
	}
	if !j.retryAt.IsZero() && time.Now().Before(j.retryAt) {
		return
	}
	if !j.restartedAt.IsZero() && time.Since(j.restartedAt) < xrayRestartStableWindow {
		j.failures++
	}
	err := j.xrayService.RestartXray(false)
	j.checkTime = 0
	j.restartedAt = time.Now()
	if err != nil {
		logger.Error("Restart xray failed:", err)
	}
	if j.failures > 0 {
		delay := xrayRestartMaxDelay
		if shift := j.failures - 1; shift < 4 {
			if d := (10 * time.Second) << uint(shift); d < delay {
				delay = d
			}
		}
		j.retryAt = time.Now().Add(delay)
		logger.Warningf("xray keeps exiting after start; next restart attempt in %s", delay)
	}
}
