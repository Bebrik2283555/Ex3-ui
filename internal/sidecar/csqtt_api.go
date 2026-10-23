package sidecar

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type csqttAuthMode int

const (
	csqttAuthUnknown csqttAuthMode = iota
	csqttAuthAPIKey
	csqttAuthCookie
)

// CSQTTClient talks to the csqtt admin HTTP API on loopback.
type CSQTTClient struct {
	baseURL string
	user    string
	pass    string
	http    *http.Client

	mu      sync.Mutex
	mode    csqttAuthMode
	authErr error
	retryAt time.Time
}

// Clients are cached per endpoint and credential pair: csqtt limits active web
// sessions, so stats polls must reuse one cookie instead of logging in each time.
var (
	csqttClientsMu sync.Mutex
	csqttClients   = map[string]*CSQTTClient{}
)

// NewCSQTTClient builds a client for a csqtt instance listening on port.
func NewCSQTTClient(port int, user, pass string) *CSQTTClient {
	key := fmt.Sprintf("%d\x00%s\x00%s", port, user, pass)
	csqttClientsMu.Lock()
	defer csqttClientsMu.Unlock()
	if c, ok := csqttClients[key]; ok {
		return c
	}
	c := newCSQTTClient(fmt.Sprintf("https://127.0.0.1:%d", port), user, pass)
	csqttClients[key] = c
	return c
}

func newCSQTTClient(baseURL, user, pass string) *CSQTTClient {
	// The admin panel uses a self-signed certificate, so verification is
	// skipped for this loopback-only connection.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // loopback self-signed admin API
	}
	jar, _ := cookiejar.New(nil)
	return &CSQTTClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		pass:    pass,
		http: &http.Client{
			Timeout:   8 * time.Second,
			Transport: transport,
			Jar:       jar,
		},
	}
}

func (c *CSQTTClient) do(method, path string, body any) ([]byte, error) {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, status, err := c.request(method, path, raw)
	if err != nil {
		return nil, err
	}
	if status >= 200 && status < 300 {
		if c.mode == csqttAuthUnknown {
			c.mode = csqttAuthAPIKey
		}
		return data, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return data, fmt.Errorf("csqtt api %s %s: %s", method, path, strings.TrimSpace(string(data)))
	}

	// 2.1.9 replaced X-API-Key with a Caesar-encoded login cookie.
	if err := c.loginLocked(); err != nil {
		return data, err
	}
	data, status, err = c.request(method, path, raw)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return data, fmt.Errorf("csqtt api %s %s: %s", method, path, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *CSQTTClient) request(method, path string, raw []byte) ([]byte, int, error) {
	var reader io.Reader
	if raw != nil {
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	// csqtt 2.5.0 accepts the admin password through X-API-Key (or a Bearer
	// token); 2.1.9 ignores it and needs the cookie session instead.
	if c.mode != csqttAuthCookie && c.pass != "" {
		req.Header.Set("X-API-Key", c.pass)
	}
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return data, resp.StatusCode, nil
}

func (c *CSQTTClient) loginLocked() error {
	now := time.Now()
	if c.authErr != nil && now.Before(c.retryAt) {
		return c.authErr
	}
	body, _ := json.Marshal(map[string]string{
		"user": csqttCaesar(c.user),
		"pass": csqttCaesar(c.pass),
	})
	data, status, err := c.request(http.MethodPost, "/api/login", body)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		c.mode = csqttAuthCookie
		c.authErr = nil
		return nil
	}
	err = fmt.Errorf("csqtt api login: %s", strings.TrimSpace(string(data)))
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests {
		// A wrong password will not fix itself; a 429 clears once the
		// server's three-minute lockout window expires.
		c.authErr = err
		c.retryAt = now.Add(3 * time.Minute)
	}
	return err
}

// csqttCaesar mirrors the obfuscation the 2.1.9 login form applies before the
// credentials leave the browser.
func csqttCaesar(s string) string {
	raw := []byte(s)
	for i := range raw {
		raw[i] += 47
	}
	return "c1:" + base64.StdEncoding.EncodeToString(raw)
}

type csqttClientInfo struct {
	Password       string `json:"password"`
	Name           string `json:"name"`
	Active         bool   `json:"active"`
	Up             int64  `json:"up"`
	Down           int64  `json:"down"`
	ActiveSessions int    `json:"active_sessions"`
}

// ClientTraffic reads the cumulative per-client counters and active session
// counts from the csqtt admin API.
func (c *CSQTTClient) ClientTraffic() ([]csqttClientInfo, error) {
	raw, err := c.do(http.MethodGet, "/api/clients", nil)
	if err != nil {
		return nil, err
	}
	var clients []csqttClientInfo
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &clients); err != nil {
			return nil, fmt.Errorf("decode client traffic: %w", err)
		}
	}
	return clients, nil
}

// SyncClients makes the csqtt store match the panel client list. Only entries
// the panel previously created are removed, so clients added through csqtt's
// own admin panel survive. 2.1.9 generates passwords server-side and has no
// batch API; links there all share the main password, so nothing to sync.
func (c *CSQTTClient) SyncClients(clients []model.Client, ledgerPath, mainPass, defaultHash string, hashesByEmail map[string]string) error {
	raw, err := c.do(http.MethodGet, "/api/clients", nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	legacy := c.mode == csqttAuthCookie
	c.mu.Unlock()
	if legacy {
		return nil
	}
	var existing []csqttClientInfo
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("decode clients: %w", err)
		}
	}
	have := make(map[string]csqttClientInfo, len(existing))
	for _, cl := range existing {
		have[cl.Password] = cl
	}
	ledger := readLedger(ledgerPath)
	mainPass = strings.TrimSpace(mainPass)
	keep := map[string]bool{}
	for _, cl := range clients {
		if pass := csqttPasswordForClient(cl); pass != "" && pass != mainPass {
			keep[pass] = true
		}
	}
	// Remove stale panel clients first: editing a client's password creates a
	// new entry, and csqtt rejects duplicate names, so the old one has to go.
	var remove []string
	for pass := range ledger {
		if pass == mainPass {
			// The main password is not a client entry; never batch-delete it.
			delete(ledger, pass)
			continue
		}
		if !keep[pass] {
			remove = append(remove, pass)
			delete(ledger, pass)
		}
	}
	if len(remove) > 0 {
		if _, err := c.do(http.MethodPost, "/api/clients/batch-delete",
			map[string]any{"passwords": remove}); err != nil {
			return err
		}
	}
	for _, cl := range clients {
		pass := csqttPasswordForClient(cl)
		if pass == "" || pass == mainPass {
			// A main credential is not a panel client entry.
			continue
		}
		want := cl.Enable && (cl.ExpiryTime <= 0 || cl.ExpiryTime > time.Now().UnixMilli())
		if _, ok := have[pass]; !ok {
			hash := strings.TrimSpace(hashesByEmail[cl.Email])
			if hash == "" {
				hash = strings.TrimSpace(defaultHash)
			}
			cl.Password = pass
			if err := c.createClient(cl, hash); err != nil {
				return err
			}
			ledger[pass] = true
			if !want {
				if _, err := c.do(http.MethodPost, "/api/clients/batch-toggle",
					map[string]any{"passwords": []string{pass}, "active": false}); err != nil {
					return err
				}
			}
			continue
		}
		if have[pass].Active != want {
			if _, err := c.do(http.MethodPost, "/api/clients/batch-toggle",
				map[string]any{"passwords": []string{pass}, "active": want}); err != nil {
				return err
			}
		}
	}
	return writeLedger(ledgerPath, ledger)
}

// csqttPasswordForClient returns the client's own CSQTT credential. The main
// password and the generic client password are not client fallbacks.
func csqttPasswordForClient(cl model.Client) string {
	return strings.TrimSpace(cl.CsqttPassword)
}

func (c *CSQTTClient) createClient(cl model.Client, hash string) error {
	// csqtt accepts 0 (never expires) or 1..3650 days.
	days := int64(0)
	if cl.ExpiryTime > 0 {
		days = (cl.ExpiryTime - time.Now().UnixMilli()) / (24 * 60 * 60 * 1000)
		if days < 1 {
			days = 1
		} else if days > 3650 {
			days = 3650
		}
	}
	body := map[string]any{
		"name":     cl.Email,
		"days":     days,
		"password": cl.Password,
		"hash":     hash,
	}
	if _, err := c.do(http.MethodPost, "/api/clients", body); err != nil {
		return err
	}
	return nil
}

func readLedger(path string) map[string]bool {
	out := map[string]bool{}
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var list []string
	if json.Unmarshal(data, &list) == nil {
		for _, p := range list {
			out[p] = true
		}
	}
	return out
}

func writeLedger(path string, ledger map[string]bool) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	list := make([]string, 0, len(ledger))
	for p := range ledger {
		list = append(list, p)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ErrCSQTTNotReady is returned before the admin API starts listening.
var ErrCSQTTNotReady = errors.New("csqtt admin api is not ready")
