package sidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type trafficCounter struct {
	Up   int64
	Down int64
}

// TrafficDelta is one client's traffic increase since the previous scrape.
type TrafficDelta struct {
	InboundID int
	Tag       string
	Email     string
	Up        int64
	Down      int64
	Online    bool
	Routed    bool
}

// CollectTraffic reads the cumulative counters each core persists and returns
// only the increases. qWDTT keeps them in passwords.json, CSQTT in its SQLite
// store exposed by the admin API. olcRTC has no per-client accounting.
func (m *Manager) CollectTraffic() []TrafficDelta {
	m.mu.Lock()
	type snapshot struct {
		id   int
		inst Instance
	}
	list := make([]snapshot, 0, len(m.procs))
	for id, mg := range m.procs {
		if mg == nil || !mg.proc.IsRunning() {
			continue
		}
		list = append(list, snapshot{id: id, inst: mg.inst})
	}
	m.mu.Unlock()

	out := make([]TrafficDelta, 0, 8)
	for _, s := range list {
		var raw map[string]trafficCounter
		online := map[string]bool{}
		switch s.inst.Protocol {
		case model.QWDTT:
			raw = qwdttCounters(s.inst)
		case model.CSQTT:
			raw, online = csqttCounters(s.inst)
		default:
			continue
		}
		if len(raw) == 0 {
			continue
		}

		matched := map[string]model.Client{}
		for _, cl := range s.inst.Clients {
			pass := sidecarClientPassword(s.inst.Protocol, cl)
			if pass == "" {
				continue
			}
			if _, dup := matched[pass]; !dup {
				matched[pass] = cl
			}
		}

		m.mu.Lock()
		prev := m.seen[s.id]
		if prev == nil {
			prev = map[string]trafficCounter{}
			m.seen[s.id] = prev
		}
		for pass, cur := range raw {
			cl, ok := matched[pass]
			if !ok {
				continue
			}
			old, had := prev[pass]
			prev[pass] = cur
			if !had {
				continue
			}
			up := cur.Up - old.Up
			down := cur.Down - old.Down
			// A core store reset (password change, data dir purge) restarts
			// the counters; count the new value instead of dropping it.
			if up < 0 {
				up = cur.Up
			}
			if down < 0 {
				down = cur.Down
			}
			if up == 0 && down == 0 && !online[pass] {
				continue
			}
			out = append(out, TrafficDelta{
				InboundID: s.id,
				Tag:       s.inst.Tag,
				Email:     cl.Email,
				Up:        up,
				Down:      down,
				Online:    online[pass] || up > 0 || down > 0,
				Routed:    s.inst.RouteThroughXray,
			})
		}
		m.mu.Unlock()
	}
	return out
}

// sidecarClientPassword returns the credential the core stores per client.
// CSQTT has no generic fallback; qWDTT keeps it for older records.
func sidecarClientPassword(protocol model.Protocol, cl model.Client) string {
	switch protocol {
	case model.QWDTT:
		if pass := strings.TrimSpace(cl.QwdttPassword); pass != "" {
			return pass
		}
		return strings.TrimSpace(cl.Password)
	case model.CSQTT:
		return strings.TrimSpace(cl.CsqttPassword)
	default:
		return ""
	}
}

func qwdttCounters(inst Instance) map[string]trafficCounter {
	cfg := inst.QWDTT
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "passwords.json"))
	if err != nil {
		return nil
	}
	var root struct {
		Passwords map[string]struct {
			Up   int64 `json:"up_bytes"`
			Down int64 `json:"down_bytes"`
		} `json:"passwords"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	out := make(map[string]trafficCounter, len(root.Passwords))
	for pass, entry := range root.Passwords {
		out[pass] = trafficCounter{Up: entry.Up, Down: entry.Down}
	}
	return out
}

func csqttCounters(inst Instance) (map[string]trafficCounter, map[string]bool) {
	cfg := inst.CSQTT
	if cfg == nil || cfg.WebPort <= 0 || strings.TrimSpace(cfg.WebPass) == "" {
		return nil, nil
	}
	clients, err := NewCSQTTClient(cfg.WebPort, cfg.WebUser, cfg.WebPass).ClientTraffic()
	if err != nil {
		return nil, nil
	}
	raw := make(map[string]trafficCounter, len(clients))
	online := make(map[string]bool, len(clients))
	for _, cl := range clients {
		if strings.TrimSpace(cl.Password) == "" {
			continue
		}
		raw[cl.Password] = trafficCounter{Up: cl.Up, Down: cl.Down}
		online[cl.Password] = cl.ActiveSessions > 0
	}
	return raw, online
}
