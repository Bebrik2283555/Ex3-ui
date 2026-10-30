package dnsresolver

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	dnsQueryTimeout  = 3500 * time.Millisecond
	probeDialTimeout = 3 * time.Second
	probeIOTimeout   = 4 * time.Second
	maxCandidates    = 4
	traceBodyLimit   = 4096
)

// blockedServiceRegions are the countries the proxied services refuse to serve.
// An unblock address whose edge reports one of them is as good as dead: the
// user only sees the "not available in your country" page again.
var blockedServiceRegions = map[string]bool{
	"AF": true, "BY": true, "CN": true, "CU": true, "IR": true,
	"KP": true, "RU": true, "SY": true, "VE": true,
}

// fallbackDnsServers are queried only when the configured server yields no
// usable address, so a stale geo-unblock answer can still be replaced instead
// of leaving a dead IP in /etc/hosts.
var fallbackDnsServers = []string{
	"https://1.1.1.1/dns-query",
	"https://8.8.8.8/dns-query",
}

// regionGatedSuffixes are the services whose edge country decides whether the
// account works at all. Static asset hosts (oaistatic, oaiusercontent, google
// CDNs) are deliberately absent: their trace reports the *client* country, not
// the proxy's, so gating them would only drop working CDN addresses.
var regionGatedSuffixes = []string{
	"chatgpt.com",
	"openai.com",
	"claude.ai",
	"anthropic.com",
}

// needsRegionGate reports whether a blocked region must reject this domain.
func needsRegionGate(domain string) bool {
	d := strings.ToLower(strings.TrimSuffix(domain, "."))
	for _, suffix := range regionGatedSuffixes {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return true
		}
	}
	return false
}

// probe is a candidate address that answered a TLS handshake for its domain.
type probe struct {
	IP      string
	Region  string
	Latency time.Duration
}

// resolveBest resolves domain, then keeps only addresses that really serve it
// (valid certificate for the name) and whose edge country can serve accounts.
func resolveBest(ctx context.Context, server, domain string) (probe, error) {
	sources := append([]string{server}, fallbackDnsServers...)
	var lastErr error

	for i, src := range sources {
		qCtx, cancel := context.WithTimeout(ctx, dnsQueryTimeout)
		candidates, err := resolveAll(qCtx, src, domain)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		best, err := probeBest(ctx, domain, candidates)
		if err == nil {
			if i > 0 {
				logger.Infof("SmartDNS: %s resolved via fallback %s", domain, src)
			}
			return best, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no A record in response")
	}
	return probe{}, lastErr
}

// probeBest verifies every candidate concurrently and returns the fastest one
// that passed. Only the first few answers are tried to bound the work.
func probeBest(ctx context.Context, domain string, candidates []string) (probe, error) {
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	type outcome struct {
		p   probe
		err error
	}
	outcomes := make([]outcome, len(candidates))
	var wg sync.WaitGroup
	for i, ip := range candidates {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			p, err := probeCandidate(ctx, domain, ip)
			outcomes[i] = outcome{p: p, err: err}
		}(i, ip)
	}
	wg.Wait()

	var best probe
	var errs []string
	found := false
	for _, o := range outcomes {
		if o.err != nil {
			errs = append(errs, o.err.Error())
			continue
		}
		if !found || o.p.Latency < best.Latency {
			best = o.p
			found = true
		}
	}
	if found {
		return best, nil
	}
	return probe{}, fmt.Errorf("%s", strings.Join(errs, "; "))
}

// probeCandidate opens TLS to ip with the domain as SNI. Go validates the
// certificate and hostname, so a handshake proves the address actually serves
// this name rather than some unrelated host. Cloudflare edges are then asked
// which country they belong to.
func probeCandidate(ctx context.Context, domain, ip string) (probe, error) {
	p := probe{IP: ip}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: probeDialTimeout},
		Config: &tls.Config{
			ServerName: domain,
			MinVersion: tls.VersionTLS12,
		},
	}
	dialCtx, cancel := context.WithTimeout(ctx, probeDialTimeout)
	defer cancel()

	start := time.Now()
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return p, fmt.Errorf("%s: %w", ip, err)
	}
	tlsConn := conn.(*tls.Conn)
	p.Latency = time.Since(start)
	defer tlsConn.Close()

	if !needsRegionGate(domain) {
		// Only the certificate matters here; gating static CDNs by country
		// would drop addresses that work fine.
		return p, nil
	}

	region, err := traceRegion(tlsConn, domain)
	if err != nil {
		// No trace endpoint: the certificate check is the only signal available.
		return p, nil
	}
	p.Region = region
	if blockedServiceRegions[region] {
		return p, fmt.Errorf("%s: edge region %s is blocked", ip, region)
	}
	return p, nil
}

// traceRegion reads Cloudflare's /cdn-cgi/trace over the already verified TLS
// socket and returns the reporting edge country.
func traceRegion(conn *tls.Conn, domain string) (string, error) {
	_ = conn.SetDeadline(time.Now().Add(probeIOTimeout))
	req := "GET /cdn-cgi/trace HTTP/1.1\r\nHost: " + domain +
		"\r\nUser-Agent: Ex3-UI-SmartDNS\r\nAccept: */*\r\nConnection: close\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return "", err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("trace status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, traceBodyLimit))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if v, ok := strings.CutPrefix(line, "loc="); ok {
			return strings.ToUpper(strings.TrimSpace(v)), nil
		}
	}
	return "", fmt.Errorf("no loc in trace")
}
