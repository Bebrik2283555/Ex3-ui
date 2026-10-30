package sidecar

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// shareEndpoint is the externally dialable address of an inbound.
type shareEndpoint struct {
	host string
	port int
	peer string
}

// Links returns the share links (at most one) for one client.
func (i Instance) Links(client model.Client) []string {
	if !client.Enable {
		return nil
	}
	switch i.Protocol {
	case model.QWDTT:
		if link := i.qwdttLink(client); link != "" {
			return []string{link}
		}
	case model.OLCRTC:
		if link := i.olcrtcLink(); link != "" {
			return []string{link}
		}
	case model.CSQTT:
		if link := i.csqttLink(client); link != "" {
			return []string{link}
		}
	}
	return nil
}

// LegacyURI renders the legacy wdtt:// URI of a qWDTT inbound.
func (i Instance) LegacyURI() string {
	cfg := i.QWDTT
	if cfg == nil {
		return ""
	}
	endpoint := i.shareEndpoint()
	if endpoint.host == "" || endpoint.port <= 0 {
		return ""
	}
	password := ""
	for _, client := range i.Clients {
		if !client.Enable {
			continue
		}
		candidate := strings.TrimSpace(client.QwdttPassword)
		if candidate == "" {
			candidate = strings.TrimSpace(client.Password)
		}
		if candidate != "" {
			password = candidate
			break
		}
	}
	if password == "" {
		password = strings.TrimSpace(cfg.MainPass)
	}
	hashes := firstHashToken(cfg.Hashes)
	if password == "" || hashes == "" {
		return ""
	}
	port := qwdttClientPort(cfg)
	return fmt.Sprintf("wdtt://%s:%d:%d:%d:%s:%s", endpoint.host, endpoint.port, cfg.WGPort, port, password, hashes)
}

// SubJSON renders the qWDTT subscription document for one client.
func (i Instance) SubJSON(client model.Client) string {
	cfg := i.QWDTT
	if cfg == nil {
		return ""
	}
	endpoint := i.shareEndpoint()
	if endpoint.host == "" || endpoint.port <= 0 {
		return ""
	}
	password := qwdttPassword(cfg, client)
	if password == "" {
		return ""
	}
	name := i.shareName(client)
	document := QwdttSubscription{
		SubscriptionName: name,
		Description:      "qWDTT tunnel via " + endpoint.peer,
		Version:          1,
		UpdatedAt:        time.Now().Format("2006-01-02"),
		Profiles: []QwdttSubProfile{{
			Name:     name,
			Peer:     endpoint.peer,
			Hashes:   qwdttHashes(cfg, client.Email),
			Workers:  qwdttWorkers(cfg),
			Port:     qwdttClientPort(cfg),
			Password: password,
		}},
	}
	out, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return ""
	}
	return string(out)
}

func (i Instance) qwdttLink(client model.Client) string {
	cfg := i.QWDTT
	if cfg == nil {
		return ""
	}
	endpoint := i.shareEndpoint()
	if endpoint.host == "" || endpoint.port <= 0 {
		return ""
	}
	password := qwdttPassword(cfg, client)
	if password == "" {
		return ""
	}
	query := url.Values{}
	query.Set("name", i.shareName(client))
	query.Set("peer", endpoint.peer)
	if hashes := qwdttHashes(cfg, client.Email); hashes != "" {
		query.Set("hashes", hashes)
	}
	query.Set("pass", password)
	query.Set("port", strconv.Itoa(qwdttClientPort(cfg)))
	query.Set("workers", strconv.Itoa(qwdttWorkers(cfg)))
	return "qwdtt://config?" + query.Encode()
}

func (i Instance) olcrtcLink() string {
	cfg := i.OLCRTC
	if cfg == nil {
		return ""
	}
	room := strings.TrimSpace(cfg.RoomID)
	key := strings.TrimSpace(cfg.CryptoKey)
	if room == "" || key == "" {
		return ""
	}
	return fmt.Sprintf("olcrtc://%s?%s@%s#%s", cfg.Provider, olcrtcTransport(cfg), cfg.RoomID, cfg.CryptoKey)
}

func (i Instance) csqttLink(client model.Client) string {
	cfg := i.CSQTT
	if cfg == nil {
		return ""
	}
	endpoint := i.shareEndpoint()
	if endpoint.host == "" || endpoint.port <= 0 {
		return ""
	}
	password := strings.TrimSpace(client.CsqttPassword)
	if password == "" {
		return ""
	}
	var link strings.Builder
	link.WriteString("csqtt://connect?v=2")
	link.WriteString("&host=" + url.QueryEscape(endpoint.host))
	link.WriteString("&peer=" + strconv.Itoa(endpoint.port))
	link.WriteString("&password=" + url.QueryEscape(password))
	if hashes := csqttHashes(cfg, client.Email); hashes != "" {
		link.WriteString("&hashes=" + hashes)
	}
	link.WriteString("&name=" + url.QueryEscape(i.shareName(client)))
	return link.String()
}

// shareName picks the first non-blank of remark, client email and "client".
// Component accessors kept as methods so link tests can verify each part.

func urlEncode(value string) string { return url.QueryEscape(value) }

func (i Instance) qwdttHashes(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	return qwdttHashes(i.QWDTT, client.Email)
}

func (i Instance) qwdttPeer() string { return i.shareEndpoint().peer }

func (i Instance) qwdttPassword(client model.Client) string {
	if i.QWDTT == nil {
		return ""
	}
	return qwdttPassword(i.QWDTT, client)
}

func (i Instance) shareName(client model.Client) string {
	for _, candidate := range []string{i.Remark, client.Email, "client"} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return "client"
}

// shareEndpoint resolves the explicit share endpoint, the protocol's public
// field, the listen host or the detected public IP, in that order.
func (i Instance) shareEndpoint() shareEndpoint {
	if subHost := strings.TrimSpace(i.protocolSubHost()); subHost != "" {
		if host, port, err := net.SplitHostPort(subHost); err == nil {
			parsed, convErr := strconv.Atoi(port)
			if convErr != nil || !validPort(parsed) || strings.TrimSpace(host) == "" {
				return shareEndpoint{}
			}
			return shareEndpoint{host: trimBrackets(host), port: parsed, peer: subHost}
		}
		host := trimBrackets(subHost)
		if strings.TrimSpace(host) == "" || !validPort(i.Port) {
			return shareEndpoint{}
		}
		return shareEndpoint{host: host, port: i.Port, peer: net.JoinHostPort(host, strconv.Itoa(i.Port))}
	}

	if !validPort(i.Port) {
		return shareEndpoint{}
	}

	host := ""
	if public := strings.TrimSpace(i.protocolPublicHost()); public != "" {
		if parsed, _, err := net.SplitHostPort(public); err == nil {
			host = parsed
		} else {
			host = trimBrackets(public)
		}
	} else {
		host = trimBrackets(i.Listen)
		if unusableListenHost(host) {
			host = PublicIP()
		}
	}
	if strings.TrimSpace(host) == "" {
		return shareEndpoint{}
	}
	return shareEndpoint{host: host, port: i.Port, peer: net.JoinHostPort(host, strconv.Itoa(i.Port))}
}

func validPort(port int) bool { return port >= 1 && port <= 65535 }

func (i Instance) protocolSubHost() string {
	switch i.Protocol {
	case model.QWDTT:
		if i.QWDTT != nil {
			return i.QWDTT.SubHost
		}
	case model.CSQTT:
		if i.CSQTT != nil {
			return i.CSQTT.SubHost
		}
	case model.OLCRTC:
		if i.OLCRTC != nil {
			return i.OLCRTC.SubHost
		}
	}
	return ""
}

func (i Instance) protocolPublicHost() string {
	switch i.Protocol {
	case model.QWDTT:
		if i.QWDTT != nil {
			return i.QWDTT.PublicHost
		}
	case model.CSQTT:
		if i.CSQTT != nil {
			return i.CSQTT.PublicIP
		}
	}
	return ""
}

func unusableListenHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]", "127.0.0.1", "localhost":
		return true
	}
	return false
}

func qwdttPassword(cfg *QWDTTConfig, client model.Client) string {
	if password := strings.TrimSpace(client.QwdttPassword); password != "" {
		return password
	}
	if password := strings.TrimSpace(client.Password); password != "" {
		return password
	}
	return strings.TrimSpace(cfg.MainPass)
}

func qwdttHashes(cfg *QWDTTConfig, email string) string {
	if override := strings.TrimSpace(cfg.HashesByEmail[email]); override != "" {
		return override
	}
	return strings.TrimSpace(cfg.Hashes)
}

func qwdttWorkers(cfg *QWDTTConfig) int {
	if cfg.Workers > 0 {
		return cfg.Workers
	}
	return 16
}

func qwdttClientPort(cfg *QWDTTConfig) int {
	if cfg.ClientPort > 0 {
		return cfg.ClientPort
	}
	return 9000
}

func firstHashToken(hashes string) string {
	fields := strings.FieldsFunc(hashes, func(r rune) bool {
		switch r {
		case ',', ' ', ';', '\r', '\n':
			return true
		}
		return false
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

func olcrtcTransport(cfg *OLCRTCConfig) string {
	switch cfg.Transport {
	case "", "datachannel":
		return "datachannel"
	case "vp8channel":
		return fmt.Sprintf("vp8channel<vp8-fps=%d&vp8-batch=%d>", cfg.VP8Fps, cfg.VP8Batch)
	case "seichannel":
		return fmt.Sprintf("seichannel<fps=%d&batch=%d&frag=%d&ack-ms=%d>", cfg.SeiFps, cfg.SeiBatch, cfg.SeiFrag, cfg.SeiAck)
	case "videochannel":
		codec := cfg.VideoCodec
		if codec == "" {
			codec = "qrcode"
		}
		return fmt.Sprintf("videochannel<video-w=%d&video-h=%d&video-fps=%d&video-codec=%s>", cfg.VideoW, cfg.VideoH, cfg.VideoFps, codec)
	}
	return cfg.Transport
}

func csqttHashes(cfg *CSQTTConfig, email string) string {
	source := strings.TrimSpace(cfg.HashesByEmail[email])
	if source == "" {
		source = strings.TrimSpace(cfg.Hashes)
	}
	if source == "" {
		return ""
	}
	tokens := strings.FieldsFunc(source, func(r rune) bool {
		switch r {
		case ',', ' ', '\t', '\r', '\n', ';':
			return true
		}
		return false
	})
	if len(tokens) > 6 {
		tokens = tokens[:6]
	}
	encoded := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		encoded = append(encoded, url.QueryEscape(token))
	}
	return strings.Join(encoded, "+")
}
