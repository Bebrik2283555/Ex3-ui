package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/sidecar"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// SidecarJob folds the cumulative qWDTT/CSQTT client counters into the panel's
// traffic and online state. Routed inbounds are only metered per client here;
// their inbound totals come from the Xray TUN bridge.
type SidecarJob struct {
	inboundService service.InboundService
}

// NewSidecarJob creates the sidecar traffic collector.
func NewSidecarJob() *SidecarJob { return new(SidecarJob) }

// Run records client deltas and refreshes online status.
func (j *SidecarJob) Run() {
	deltas := sidecar.GetManager().CollectTraffic()
	if len(deltas) == 0 {
		return
	}
	clientTraffics := make([]*xray.ClientTraffic, 0, len(deltas))
	inboundUp := make(map[string]int64)
	inboundDown := make(map[string]int64)
	onlineEmails := make([]string, 0, len(deltas))
	activeTags := make([]string, 0, len(deltas))
	seenEmail := make(map[string]bool, len(deltas))
	seenTag := make(map[string]bool, len(deltas))
	for _, d := range deltas {
		if d.Email != "" {
			clientTraffics = append(clientTraffics, &xray.ClientTraffic{
				Email: d.Email,
				Up:    d.Up,
				Down:  d.Down,
			})
			if d.Online && !seenEmail[d.Email] {
				seenEmail[d.Email] = true
				onlineEmails = append(onlineEmails, d.Email)
			}
		}
		if !d.Routed && d.Tag != "" {
			inboundUp[d.Tag] += d.Up
			inboundDown[d.Tag] += d.Down
		}
		if d.Online && d.Tag != "" && !seenTag[d.Tag] {
			seenTag[d.Tag] = true
			activeTags = append(activeTags, d.Tag)
		}
	}
	traffics := make([]*xray.Traffic, 0, len(inboundUp))
	for tag, up := range inboundUp {
		traffics = append(traffics, &xray.Traffic{
			IsInbound: true,
			Tag:       tag,
			Up:        up,
			Down:      inboundDown[tag],
		})
	}
	if len(traffics) > 0 || len(clientTraffics) > 0 {
		if _, _, err := j.inboundService.AddTraffic(traffics, clientTraffics); err != nil {
			logger.Warning("sidecar job: add traffic failed:", err)
		}
	}
	j.inboundService.RefreshLocalOnlineClients(onlineEmails, activeTags)
}
