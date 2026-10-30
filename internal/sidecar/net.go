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

// Public IPv4 detection: the value is cached and served from cache for ten
// minutes, including a failed probe.
const (
	publicIPCacheTTL = 10 * time.Minute
	publicIPTimeout  = 5 * time.Second
	publicIPBodyMax  = 64
)

var publicIPEchoServices = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://icanhazip.com",
	"https://checkip.amazonaws.com",
}

var (
	publicIPMu      sync.Mutex
	publicIPValue   string
	publicIPFetched time.Time

	// publicIPProbe is held while a probe runs. Concurrent callers do not start
	// their own probe: they get the last known value instead.
	publicIPProbe sync.Mutex
)

// PublicIP returns the panel's detected public IPv4 address, or an empty
// string when no echo service answered. The result is cached for ten minutes.
func PublicIP() string {
	publicIPMu.Lock()
	if fresh := !publicIPFetched.IsZero() && time.Since(publicIPFetched) < publicIPCacheTTL; fresh {
		value := publicIPValue
		publicIPMu.Unlock()
		return value
	}
	publicIPMu.Unlock()

	if !publicIPProbe.TryLock() {
		publicIPMu.Lock()
		value := publicIPValue
		publicIPMu.Unlock()
		return value
	}
	defer publicIPProbe.Unlock()

	// Another caller may have refreshed the cache while we were waiting.
	publicIPMu.Lock()
	if fresh := !publicIPFetched.IsZero() && time.Since(publicIPFetched) < publicIPCacheTTL; fresh {
		value := publicIPValue
		publicIPMu.Unlock()
		return value
	}
	publicIPMu.Unlock()

	value := probePublicIPv4()

	publicIPMu.Lock()
	publicIPValue = value
	publicIPFetched = time.Now()
	publicIPMu.Unlock()
	return value
}

func probePublicIPv4() string {
	client := &http.Client{Timeout: publicIPTimeout}
	for _, service := range publicIPEchoServices {
		if ip := fetchPublicIPv4(client, service); ip != "" {
			return ip
		}
	}
	return ""
}

func fetchPublicIPv4(client *http.Client, service string) string {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, service, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, publicIPBodyMax))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(body))
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return value
}
