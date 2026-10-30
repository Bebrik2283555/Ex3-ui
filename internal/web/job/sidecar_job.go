package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/sidecar"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// SidecarJob folds the per-client counters of the external cores (qWDTT,
// olcRTC, CSQTT) into the panel's traffic accounting and refreshes the local
// online/active sets. The process side of the cores is reconciled elsewhere;
// this job only moves counters.
type SidecarJob struct {
	inboundService service.InboundService
}

// NewSidecarJob creates the sidecar traffic collector.
func NewSidecarJob() *SidecarJob { return new(SidecarJob) }

// Run collects the delta of every running core, adds it to the inbound and
// client counters in one write, and then refreshes the online sets.
func (j *SidecarJob) Run() {
	deltas := sidecar.GetManager().CollectTraffic()
	if len(deltas) == 0 {
		// No traffic and no online signal: nothing to write, no refresh. Known
		// online clients are left to age out on their own grace window.
		return
	}

	clientTraffics := make([]*xray.ClientTraffic, 0, len(deltas))
	inboundUp := make(map[string]int64, len(deltas))
	inboundDown := make(map[string]int64, len(deltas))
	onlineEmails := make([]string, 0, len(deltas))
	onlineTags := make([]string, 0, len(deltas))
	seenEmails := make(map[string]struct{}, len(deltas))
	seenTags := make(map[string]struct{}, len(deltas))

	for _, delta := range deltas {
		if delta.Email != "" {
			clientTraffics = append(clientTraffics, &xray.ClientTraffic{
				Email: delta.Email,
				Up:    delta.Up,
				Down:  delta.Down,
			})
			if delta.Online {
				if _, dup := seenEmails[delta.Email]; !dup {
					seenEmails[delta.Email] = struct{}{}
					onlineEmails = append(onlineEmails, delta.Email)
				}
			}
		}
		// A routed core's bytes are metered by the Xray bridge, and an untagged
		// delta belongs to no inbound row, but a routed core still reports
		// connectivity for its tag.
		if delta.Tag == "" {
			continue
		}
		if delta.Online {
			if _, dup := seenTags[delta.Tag]; !dup {
				seenTags[delta.Tag] = struct{}{}
				onlineTags = append(onlineTags, delta.Tag)
			}
		}
		if delta.Routed {
			continue
		}
		inboundUp[delta.Tag] += delta.Up
		inboundDown[delta.Tag] += delta.Down
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

	// The online refresh runs even when the write failed: the cores did report
	// these clients as connected.
	j.inboundService.RefreshLocalOnlineClients(onlineEmails, onlineTags)
}
