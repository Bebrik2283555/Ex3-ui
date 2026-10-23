package service

import (
	"fmt"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/sidecar"
)

// CoreInfo is the status snapshot shown in Settings → Cores.
type CoreInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	Inbounds    int    `json:"inbounds"`
	RunningNum  int    `json:"runningInbounds"`
	Binary      string `json:"binary"`
	Size        int64  `json:"size"`
	Modified    int64  `json:"modified"`
	LastError   string `json:"lastError,omitempty"`
}

// CoreInboundInfo describes one inbound supervised by a sidecar core.
type CoreInboundInfo struct {
	ID        int      `json:"id"`
	Remark    string   `json:"remark"`
	Protocol  string   `json:"protocol"`
	Listen    string   `json:"listen,omitempty"`
	Port      int      `json:"port"`
	ConfigDir string   `json:"configDir,omitempty"`
	Running   bool     `json:"running"`
	LastError string   `json:"lastError,omitempty"`
	Logs      []string `json:"logs,omitempty"`
	Clients   int      `json:"clients"`
	Links     []string `json:"links,omitempty"`
	LegacyURI string   `json:"legacyUri,omitempty"`
	SubJSON   string   `json:"subJson,omitempty"`
}

// CoreDetails combines the core snapshot with its supervised inbounds.
type CoreDetails struct {
	CoreInfo
	Inbounds []CoreInboundInfo `json:"inboundList"`
}

// SidecarClientLinks groups the share links generated for one client.
type SidecarClientLinks struct {
	Email string   `json:"email"`
	Links []string `json:"links"`
}

// SidecarLinkSet is the complete link payload for one sidecar inbound.
type SidecarLinkSet struct {
	InboundID int                  `json:"inboundId"`
	Remark    string               `json:"remark"`
	Protocol  string               `json:"protocol"`
	Links     []string             `json:"links"`
	LegacyURI string               `json:"legacyUri,omitempty"`
	SubJSON   string               `json:"subJson,omitempty"`
	Clients   []SidecarClientLinks `json:"clients"`
}

// GetSidecarLinkSet returns modern links, the wdtt legacy URI and subJson.
func (s *InboundService) GetSidecarLinkSet(id int) (*SidecarLinkSet, error) {
	inbound, err := s.GetInbound(id)
	if err != nil {
		return nil, err
	}
	if inbound == nil || !sidecar.Supported(inbound.Protocol) {
		return nil, fmt.Errorf("inbound %d is not served by a sidecar core", id)
	}
	inst, err := sidecar.InstanceFromInbound(inbound)
	if err != nil {
		return nil, err
	}
	clients, err := s.GetClients(inbound)
	if err != nil {
		return nil, err
	}
	out := &SidecarLinkSet{
		InboundID: inbound.Id,
		Remark:    inbound.Remark,
		Protocol:  string(inbound.Protocol),
		Clients:   make([]SidecarClientLinks, 0, len(clients)),
	}
	for _, client := range clients {
		show := client
		show.Enable = true
		links := inst.Links(show)
		if len(links) == 0 {
			continue
		}
		out.Clients = append(out.Clients, SidecarClientLinks{Email: client.Email, Links: links})
		out.Links = append(out.Links, links...)
	}
	if len(clients) == 0 {
		out.Links = inst.Links(model.Client{Enable: true})
	}
	out.Links = dedupeStrings(out.Links)
	out.LegacyURI = inst.LegacyURI()
	if len(clients) > 0 {
		out.SubJSON = inst.SubJSON(clients[0])
	} else {
		out.SubJSON = inst.SubJSON(model.Client{Enable: true})
	}
	return out, nil
}

// CoreService manages the sidecar cores exposed in Settings → Cores.
type CoreService struct{}

var sidecarCoreOrder = []model.Protocol{model.QWDTT, model.OLCRTC, model.CSQTT}

// applyLocalSidecar re-applies one sidecar inbound after its client set
// changed. The manager only restarts when the resolved fingerprint differs,
// so unrelated client edits stay cheap.
func (s *InboundService) applyLocalSidecar(inboundID int) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil || inbound == nil || inbound.NodeID != nil || !sidecar.Supported(inbound.Protocol) {
		return
	}
	manager := sidecar.GetManager()
	if !inbound.Enable {
		manager.Remove(inbound.Id)
		return
	}
	inst, err := sidecar.InstanceFromInbound(inbound)
	if err != nil {
		logger.Debug("sidecar: resolve inbound", inboundID, ":", err)
		return
	}
	if err := manager.Ensure(inst); err != nil {
		logger.Debug("sidecar: immediate client apply failed for inbound", inboundID, ":", err)
	}
}

// ParseCoreName validates a core name from the URL.
func ParseCoreName(name string) (model.Protocol, error) {
	for _, p := range sidecarCoreOrder {
		if string(p) == name {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown core %q", name)
}

func (s *CoreService) inboundList(p model.Protocol) ([]*model.Inbound, error) {
	var rows []*model.Inbound
	err := database.GetDB().Model(model.Inbound{}).Where("protocol = ?", string(p)).Find(&rows).Error
	return rows, err
}

// List returns one row per sidecar core.
func (s *CoreService) List() ([]CoreInfo, error) {
	out := make([]CoreInfo, 0, len(sidecarCoreOrder))
	for _, p := range sidecarCoreOrder {
		info := CoreInfo{
			Name:        string(p),
			DisplayName: sidecar.DisplayName(p),
			Binary:      sidecar.BinaryPath(p),
		}
		if st, err := os.Stat(info.Binary); err == nil && !st.IsDir() {
			info.Installed = true
			info.Size = st.Size()
			info.Modified = st.ModTime().Unix()
		}
		rows, err := s.inboundList(p)
		if err != nil {
			return nil, err
		}
		for _, ib := range rows {
			if ib.Enable {
				info.Inbounds++
				info.Enabled = true
			}
			st := sidecar.GetManager().StatusOf(ib.Id)
			if st.Running {
				info.RunningNum++
				info.Running = true
			}
			if info.LastError == "" && st.LastError != "" {
				info.LastError = st.LastError
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// Details returns the core snapshot plus its inbound processes, logs and links.
func (s *CoreService) Details(name string) (*CoreDetails, error) {
	p, err := ParseCoreName(name)
	if err != nil {
		return nil, err
	}
	cores, err := s.List()
	if err != nil {
		return nil, err
	}
	out := &CoreDetails{}
	for _, core := range cores {
		if core.Name == string(p) {
			out.CoreInfo = core
			break
		}
	}
	rows, err := s.inboundList(p)
	if err != nil {
		return nil, err
	}
	out.Inbounds = make([]CoreInboundInfo, 0, len(rows))
	for _, ib := range rows {
		info := CoreInboundInfo{
			ID:       ib.Id,
			Remark:   ib.Remark,
			Protocol: string(ib.Protocol),
			Listen:   ib.Listen,
			Port:     ib.Port,
			Clients:  len(ib.ClientStats),
		}
		st := sidecar.GetManager().StatusOf(ib.Id)
		info.Running = st.Running
		info.LastError = st.LastError
		info.Logs = st.Logs
		inst, instErr := sidecar.InstanceFromInbound(ib)
		if instErr != nil {
			if info.LastError == "" {
				info.LastError = instErr.Error()
			}
			out.Inbounds = append(out.Inbounds, info)
			continue
		}
		switch ib.Protocol {
		case model.QWDTT:
			info.ConfigDir = inst.QWDTT.ConfigDir
		case model.CSQTT:
			info.ConfigDir = inst.CSQTT.ConfigDir
		}
		clients, _ := s.clientsOf(ib)
		info.Clients = len(clients)
		if len(clients) == 0 {
			info.Links = inst.Links(model.Client{Enable: true})
			info.LegacyURI = inst.LegacyURI()
			info.SubJSON = inst.SubJSON(model.Client{Enable: true})
		} else {
			for _, client := range clients {
				info.Links = append(info.Links, inst.Links(client)...)
			}
			info.LegacyURI = inst.LegacyURI()
			info.SubJSON = inst.SubJSON(clients[0])
		}
		info.Links = dedupeStrings(info.Links)
		out.Inbounds = append(out.Inbounds, info)
	}
	return out, nil
}

func (s *CoreService) clientsOf(ib *model.Inbound) ([]model.Client, error) {
	return (&InboundService{}).GetClients(ib)
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Start brings every enabled inbound of the core up without touching others.
func (s *CoreService) Start(name string) error {
	p, err := ParseCoreName(name)
	if err != nil {
		return err
	}
	rows, err := s.inboundList(p)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ib := range rows {
		if !ib.Enable {
			continue
		}
		inst, err := sidecar.InstanceFromInbound(ib)
		if err == nil {
			err = sidecar.Validate(ib)
		}
		if err == nil {
			err = sidecar.GetManager().Ensure(inst)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stop terminates every process belonging to the core.
func (s *CoreService) Stop(name string) error {
	p, err := ParseCoreName(name)
	if err != nil {
		return err
	}
	rows, err := s.inboundList(p)
	if err != nil {
		return err
	}
	for _, ib := range rows {
		sidecar.GetManager().Remove(ib.Id)
	}
	return nil
}

// Restart stops the core and brings every enabled inbound back up.
func (s *CoreService) Restart(name string) error {
	if err := s.Stop(name); err != nil {
		return err
	}
	p, _ := ParseCoreName(name)
	rows, err := s.inboundList(p)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ib := range rows {
		if !ib.Enable {
			continue
		}
		inst, err := sidecar.InstanceFromInbound(ib)
		if err == nil {
			err = sidecar.Validate(ib)
		}
		if err == nil {
			err = sidecar.GetManager().Ensure(inst)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
