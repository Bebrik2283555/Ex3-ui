package sidecar

// Traffic hand-off: reads the counters each core persists and publishes the
// increase since the previous poll.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// trafficCounter is the cumulative byte count of one core-side credential.
type trafficCounter struct {
	Up   int64
	Down int64
}

// TrafficDelta is one client's traffic increase since the previous poll.
type TrafficDelta struct {
	InboundID int
	Tag       string
	Email     string
	Up        int64
	Down      int64
	Online    bool
	Routed    bool
}

// CollectTraffic returns this poll's per-client increases. It never fails: a
// broken counter source simply contributes nothing.
func (m *Manager) CollectTraffic() []TrafficDelta {
	type inboundPoll struct {
		id   int
		ent  *managed
		inst Instance
	}

	m.mu.Lock()
	m.initLocked()
	polls := make([]inboundPoll, 0, len(m.procs))
	for id, ent := range m.procs {
		if ent == nil || ent.proc == nil || !ent.proc.IsRunning() {
			continue
		}
		polls = append(polls, inboundPoll{id: id, ent: ent, inst: ent.inst})
	}
	m.mu.Unlock()

	out := make([]TrafficDelta, 0, len(polls))
	for _, poll := range polls {
		out = append(out, m.trafficFor(poll.id, poll.ent, poll.inst)...)
	}
	return out
}

// trafficFor does the I/O outside the registry lock and only then commits the
// new baselines, so a slow read never blocks another inbound.
func (m *Manager) trafficFor(id int, ent *managed, inst Instance) []TrafficDelta {
	current, online := readCounters(inst)
	if len(current) == 0 {
		return nil
	}
	owners := credentialOwners(inst)
	if len(owners) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.procs[id] != ent {
		return nil
	}
	m.initLocked()
	seen := m.seen[id]
	if seen == nil {
		seen = make(map[string]trafficCounter)
		m.seen[id] = seen
	}

	var out []TrafficDelta
	for credential, now := range current {
		email, matched := owners[credential]
		if !matched {
			continue
		}
		previous, baseline := seen[credential]
		seen[credential] = now
		if !baseline {
			continue
		}
		up, down := now.Up-previous.Up, now.Down-previous.Down
		if up < 0 {
			up = now.Up
		}
		if down < 0 {
			down = now.Down
		}
		isOnline := online[credential] || up > 0 || down > 0
		if up == 0 && down == 0 && !isOnline {
			continue
		}
		out = append(out, TrafficDelta{
			InboundID: inst.InboundID,
			Tag:       inst.Tag,
			Email:     email,
			Up:        up,
			Down:      down,
			Online:    isOnline,
			Routed:    inst.RouteThroughXray,
		})
	}
	return out
}

// readCounters returns the cumulative counters of every credential the core
// reports, plus the credentials it reports as connected.
func readCounters(inst Instance) (map[string]trafficCounter, map[string]bool) {
	switch inst.Protocol {
	case model.QWDTT:
		return qwdttCounters(inst), nil
	case model.CSQTT:
		return csqttCounters(inst)
	}
	return nil, nil
}

// credentialOwners maps a core credential to the first panel client that owns
// it; a later client with the same credential is ignored.
func credentialOwners(inst Instance) map[string]string {
	owners := make(map[string]string, len(inst.Clients))
	for _, client := range inst.Clients {
		credential := sidecarClientPassword(inst.Protocol, client)
		if credential == "" {
			continue
		}
		if _, taken := owners[credential]; taken {
			continue
		}
		owners[credential] = client.Email
	}
	return owners
}

// qwdttCounters reads the child's own store, which preparation also merges.
func qwdttCounters(inst Instance) map[string]trafficCounter {
	cfg := inst.QWDTT
	if cfg == nil || strings.TrimSpace(cfg.ConfigDir) == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(cfg.ConfigDir, qwdttStoreName))
	if err != nil {
		return nil
	}
	var document struct {
		Passwords map[string]struct {
			Up   int64 `json:"up_bytes"`
			Down int64 `json:"down_bytes"`
		} `json:"passwords"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil
	}
	out := make(map[string]trafficCounter, len(document.Passwords))
	for credential, entry := range document.Passwords {
		credential = strings.TrimSpace(credential)
		if credential == "" {
			continue
		}
		out[credential] = trafficCounter{Up: entry.Up, Down: entry.Down}
	}
	return out
}

// csqttCounters asks the running core's admin API. A blank password or a
// disabled admin port means the inbound contributes nothing.
func csqttCounters(inst Instance) (map[string]trafficCounter, map[string]bool) {
	cfg := inst.CSQTT
	if cfg == nil || cfg.WebPort <= 0 || strings.TrimSpace(cfg.WebPass) == "" {
		return nil, nil
	}
	clients, err := NewCSQTTClient(cfg.WebPort, cfg.WebUser, cfg.WebPass).ClientTraffic()
	if err != nil {
		return nil, nil
	}
	counters := make(map[string]trafficCounter, len(clients))
	online := make(map[string]bool, len(clients))
	for _, client := range clients {
		credential := strings.TrimSpace(client.Password)
		if credential == "" {
			continue
		}
		counters[credential] = trafficCounter{Up: client.Up, Down: client.Down}
		if client.ActiveSessions > 0 {
			online[credential] = true
		}
	}
	return counters, online
}
