package sidecar

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// publicIPURLs are public IPv4 echo services used when an inbound listens on a
// wildcard address and no explicit public host is configured.
var publicIPURLs = []string{
	"https://api4.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://v4.api.ipinfo.io/ip",
	"https://ipv4.myexternalip.com/raw",
	"https://4.ident.me",
}

var (
	publicIPMu       sync.Mutex
	publicIPCached   string
	publicIPFetched  time.Time
	publicIPCacheTTL = 10 * time.Minute
)

// PublicIP returns the server's public IPv4 address, cached for 10 minutes.
// Returns "" when detection fails; callers then omit the link.
func PublicIP() string {
	publicIPMu.Lock()
	defer publicIPMu.Unlock()
	if publicIPCached != "" && time.Since(publicIPFetched) < publicIPCacheTTL {
		return publicIPCached
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, u := range publicIPURLs {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			publicIPCached = ip
			publicIPFetched = time.Now()
			return ip
		}
	}
	publicIPCached = ""
	publicIPFetched = time.Now()
	return ""
}
