package service

import (
	"errors"
	"fmt"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/sidecar"
)

// CoreInfo is the Settings -> Cores row of one external core.
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

// CoreInboundInfo is one inbound row of a core, with its live state.
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

// CoreDetails is one core plus every inbound it serves.
type CoreDetails struct {
	CoreInfo
	Inbounds []CoreInboundInfo `json:"inboundList"`
}

// SidecarClientLinks is the share links one client of an inbound produced.
type SidecarClientLinks struct {
	Email string   `json:"email"`
	Links []string `json:"links"`
}

// SidecarLinkSet is the share payload of one sidecar inbound.
type SidecarLinkSet struct {
	InboundID int                  `json:"inboundId"`
	Remark    string               `json:"remark"`
	Protocol  string               `json:"protocol"`
	Links     []string             `json:"links"`
	LegacyURI string               `json:"legacyUri,omitempty"`
	SubJSON   string               `json:"subJson,omitempty"`
	Clients   []SidecarClientLinks `json:"clients"`
}

// CoreService reads the three external cores and drives their processes.
type CoreService struct{}

// sidecarCoreOrder is the fixed order the panel lists the cores in.
var sidecarCoreOrder = []model.Protocol{model.QWDTT, model.OLCRTC, model.CSQTT}

// ParseCoreName maps a core name from the URL onto its protocol. Names are the
// protocol strings, exact and case-sensitive.
func ParseCoreName(name string) (model.Protocol, error) {
	for _, protocol := range sidecarCoreOrder {
		if string(protocol) == name {
			return protocol, nil
		}
	}
	return "", fmt.Errorf("unknown core %q", name)
}

// GetSidecarLinkSet returns the share payload of one sidecar inbound: the union
// of its links plus the per-client breakdown.
func (s *InboundService) GetSidecarLinkSet(id int) (*SidecarLinkSet, error) {
	inbound, err := s.GetInbound(id)
	if err != nil {
		return nil, err
	}
	if inbound == nil || !sidecar.Supported(inbound.Protocol) {
		return nil, fmt.Errorf("inbound %d is not served by a sidecar core", id)
	}
	instance, err := sidecar.InstanceFromInbound(inbound)
	if err != nil {
		return nil, err
	}

	clients, err := s.GetClients(inbound)
	if err != nil {
		return nil, err
	}

	set := &SidecarLinkSet{
		InboundID: inbound.Id,
		Remark:    inbound.Remark,
		Protocol:  string(inbound.Protocol),
		Clients:   make([]SidecarClientLinks, 0, len(clients)),
	}
	// A client-less inbound still shares: an enabled synthetic client falls
	// back to the remark (or "client") for its display name.
	var union []string
	if len(clients) == 0 {
		union = instance.Links(model.Client{Enable: true})
	}
	for _, client := range clients {
		// A disabled client is still part of the payload: share links describe
		// the inbound, not the client's current switch.
		client.Enable = true
		links := instance.Links(client)
		if len(links) == 0 {
			continue
		}
		set.Clients = append(set.Clients, SidecarClientLinks{Email: client.Email, Links: links})
		union = append(union, links...)
	}
	set.Links = dedupeStrings(union)
	if inbound.Protocol == model.QWDTT {
		set.LegacyURI = instance.LegacyURI()
	}
	set.SubJSON = instance.SubJSON(documentClient(clients))
	return set, nil
}

// applyLocalSidecar converges one local sidecar inbound right after a client
// edit commits, so the change lands without waiting for the next reconcile
// pass. Failures are debug-logged: the passes are the backstop.
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
	instance, err := sidecar.InstanceFromInbound(inbound)
	if err != nil {
		// Leave a running child process alone: the reconcile pass is the
		// backstop, and a resolve failure must not take the core down.
		logger.Debug("sidecar: resolve inbound", inboundID, ":", err)
		return
	}
	if err := manager.Ensure(instance); err != nil {
		logger.Debug("sidecar: immediate client apply failed for inbound", inboundID, ":", err)
	}
}

// inboundList returns every row of one protocol, enabled or not, in database
// order.
func (s *CoreService) inboundList(p model.Protocol) ([]*model.Inbound, error) {
	db := database.GetDB()
	if db == nil {
		return nil, errors.New("database is not ready")
	}
	var rows []*model.Inbound
	err := db.Model(model.Inbound{}).
		Where("protocol = ?", p).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// List renders one row per core in the fixed protocol order.
func (s *CoreService) List() ([]CoreInfo, error) {
	cores := make([]CoreInfo, 0, len(sidecarCoreOrder))
	for _, protocol := range sidecarCoreOrder {
		rows, err := s.inboundList(protocol)
		if err != nil {
			return nil, err
		}
		cores = append(cores, s.coreInfo(protocol, rows))
	}
	return cores, nil
}

// Details describes one core and every inbound of that protocol.
func (s *CoreService) Details(name string) (*CoreDetails, error) {
	protocol, err := ParseCoreName(name)
	if err != nil {
		return nil, err
	}
	rows, err := s.inboundList(protocol)
	if err != nil {
		return nil, err
	}
	details := &CoreDetails{
		CoreInfo: s.coreInfo(protocol, rows),
		Inbounds: make([]CoreInboundInfo, 0, len(rows)),
	}
	for _, inbound := range rows {
		if inbound == nil {
			continue
		}
		details.Inbounds = append(details.Inbounds, s.inboundInfo(inbound))
	}
	return details, nil
}

// coreInfo rolls one protocol's rows up into a Settings -> Cores row. It reads
// the binary on disk and the supervisor snapshots; it never starts anything.
func (s *CoreService) coreInfo(protocol model.Protocol, rows []*model.Inbound) CoreInfo {
	info := CoreInfo{
		Name:        string(protocol),
		DisplayName: sidecar.DisplayName(protocol),
		Binary:      sidecar.BinaryPath(protocol),
	}
	if file, err := os.Stat(info.Binary); err == nil && !file.IsDir() {
		info.Installed = true
		info.Size = file.Size()
		info.Modified = file.ModTime().Unix()
	}

	manager := sidecar.GetManager()
	for _, inbound := range rows {
		if inbound == nil {
			continue
		}
		if inbound.Enable {
			info.Enabled = true
			info.Inbounds++
		}
		status := manager.StatusOf(inbound.Id)
		if status.Running {
			info.Running = true
			info.RunningNum++
		}
		if info.LastError == "" {
			info.LastError = status.LastError
		}
	}
	return info
}

// inboundInfo describes one inbound row: its live snapshot, and — when it still
// resolves — its links and effective settings paths.
func (s *CoreService) inboundInfo(inbound *model.Inbound) CoreInboundInfo {
	info := CoreInboundInfo{
		ID:       inbound.Id,
		Remark:   inbound.Remark,
		Protocol: string(inbound.Protocol),
		Listen:   inbound.Listen,
		Port:     inbound.Port,
	}

	status := sidecar.GetManager().StatusOf(inbound.Id)
	info.Running = status.Running
	info.Logs = status.Logs
	info.LastError = status.LastError

	instance, err := sidecar.InstanceFromInbound(inbound)
	if err != nil {
		if info.LastError == "" {
			info.LastError = err.Error()
		}
		return info
	}

	clients, err := s.clientsOf(inbound)
	if err != nil {
		// An unreadable snapshot is not fatal: the row still shares through
		// the synthetic client and reports zero clients.
		clients = nil
	}
	info.Clients = len(clients)
	switch instance.Protocol {
	case model.QWDTT:
		if instance.QWDTT != nil {
			info.ConfigDir = instance.QWDTT.ConfigDir
		}
	case model.CSQTT:
		if instance.CSQTT != nil {
			info.ConfigDir = instance.CSQTT.ConfigDir
		}
	}
	info.Links = dedupeStrings(coreLinks(instance, clients))
	if instance.Protocol == model.QWDTT {
		info.LegacyURI = instance.LegacyURI()
	}
	info.SubJSON = instance.SubJSON(documentClient(clients))
	return info
}

// clientsOf reads the inbound's stored settings snapshot: the same list its own
// client form shows, in the order it appears there.
func (s *CoreService) clientsOf(inbound *model.Inbound) ([]model.Client, error) {
	var inboundService InboundService
	return inboundService.GetClients(inbound)
}

// coreLinks gathers every client's links in list order; a client-less inbound
// shares through one synthetic enabled client.
func coreLinks(instance sidecar.Instance, clients []model.Client) []string {
	if len(clients) == 0 {
		return instance.Links(model.Client{Enable: true})
	}
	links := make([]string, 0, len(clients))
	for _, client := range clients {
		links = append(links, instance.Links(client)...)
	}
	return links
}

// documentClient picks the client a subscription document is rendered for: the
// first one, or the synthetic client of a client-less inbound.
func documentClient(clients []model.Client) model.Client {
	if len(clients) > 0 {
		return clients[0]
	}
	return model.Client{Enable: true}
}

// dedupeStrings removes duplicates while preserving first-seen order.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// Start starts every enabled inbound of a core, skipping the rest and returning
// the first error while still attempting the later rows.
func (s *CoreService) Start(name string) error {
	protocol, err := ParseCoreName(name)
	if err != nil {
		return err
	}
	rows, err := s.inboundList(protocol)
	if err != nil {
		return err
	}
	manager := sidecar.GetManager()
	var firstErr error
	for _, inbound := range rows {
		if inbound == nil || !inbound.Enable {
			continue
		}
		instance, err := sidecar.InstanceFromInbound(inbound)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := sidecar.Validate(inbound); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := manager.Ensure(instance); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stop removes every inbound of a core from the supervisor, enabled or not.
func (s *CoreService) Stop(name string) error {
	protocol, err := ParseCoreName(name)
	if err != nil {
		return err
	}
	rows, err := s.inboundList(protocol)
	if err != nil {
		return err
	}
	manager := sidecar.GetManager()
	for _, inbound := range rows {
		if inbound == nil {
			continue
		}
		manager.Remove(inbound.Id)
	}
	return nil
}

// Restart stops the core's rows and starts the enabled ones again. It is not
// atomic: rows started before a failure stay started.
func (s *CoreService) Restart(name string) error {
	if err := s.Stop(name); err != nil {
		return err
	}
	return s.Start(name)
}
