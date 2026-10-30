package sidecar

// Loopback admin-API client for the CSQTT core: login, client reconciliation
// and counter reads. The child caps concurrent web sessions, so one client
// instance (and its cookie jar) is shared per admin port.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const (
	csqttAPITimeout     = 8 * time.Second
	csqttAPIMaxBody     = 1 << 20
	csqttAPILockout     = 3 * time.Minute
	csqttAPILoginPath   = "/api/login"
	csqttAPIClientsPath = "/api/clients"
	csqttAPIDeletePath  = "/api/clients/batch-delete"
	csqttAPITogglePath  = "/api/clients/batch-toggle"
	csqttAPIDaysMax     = 3650
	csqttAPIDayMillis   = int64(86_400_000)
	csqttAPICredPrefix  = "c1:"
	csqttAPIByteShift   = 47
)

// ErrCSQTTNotReady is kept for callers that used to probe readiness; the API
// reports a plain transport error before the child listens.
var ErrCSQTTNotReady = errors.New("csqtt admin api is not ready")

// csqttAuthState tracks which dialect the child accepted.
type csqttAuthState int

const (
	csqttAuthUnknown csqttAuthState = iota
	csqttAuthAPIKey
	csqttAuthCookie
)

// csqttClientInfo is the subset of one core-side client the panel consumes.
type csqttClientInfo struct {
	Password       string `json:"password"`
	Name           string `json:"name"`
	Active         bool   `json:"active"`
	Up             int64  `json:"up"`
	Down           int64  `json:"down"`
	ActiveSessions int    `json:"active_sessions"`
}

// Request bodies, in the child's documented key order.
type csqttLoginPayload struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

type csqttCreatePayload struct {
	Name     string `json:"name"`
	Days     int    `json:"days"`
	Password string `json:"password"`
	Hash     string `json:"hash"`
}

type csqttPasswordsPayload struct {
	Passwords []string `json:"passwords"`
}

type csqttTogglePayload struct {
	Passwords []string `json:"passwords"`
	Active    bool     `json:"active"`
}

// CSQTTClient talks to one core's admin API.
type CSQTTClient struct {
	baseURL string
	user    string
	pass    string
	http    *http.Client

	mu            sync.Mutex
	state         csqttAuthState
	loginError    string
	loginErrorTil time.Time
}

var (
	csqttClientsMu sync.Mutex
	csqttClients   = make(map[int]*CSQTTClient)
)

// NewCSQTTClient returns the shared client for an admin port, replacing the
// cached one when the credentials changed.
func NewCSQTTClient(port int, user, pass string) *CSQTTClient {
	csqttClientsMu.Lock()
	defer csqttClientsMu.Unlock()
	if cached := csqttClients[port]; cached != nil {
		if cached.user == user && cached.pass == pass {
			return cached
		}
		cached.http.CloseIdleConnections()
	}
	base := "https://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	client := newCSQTTClient(base, user, pass)
	csqttClients[port] = client
	return client
}

// newCSQTTClient builds a client for an admin base URL.
func newCSQTTClient(baseURL, user, pass string) *CSQTTClient {
	jar, _ := cookiejar.New(nil)
	return &CSQTTClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		pass:    pass,
		http: &http.Client{
			Timeout: csqttAPITimeout,
			Jar:     jar,
			// The core serves its admin panel from a self-signed loopback
			// certificate, so verification cannot succeed by design.
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// ClientTraffic reads every client's cumulative counters.
func (c *CSQTTClient) ClientTraffic() ([]csqttClientInfo, error) {
	return c.listClients("decode client traffic")
}

// SyncClients converges the core's client set onto the panel's.
func (c *CSQTTClient) SyncClients(clients []model.Client, ledgerPath, mainPass, defaultHash string, hashesByEmail map[string]string) error {
	coreClients, err := c.listClients("decode clients")
	if err != nil {
		return err
	}
	// The cookie dialect generates passwords itself and has no batch API.
	if c.authState() == csqttAuthCookie {
		return nil
	}

	existing := make(map[string]csqttClientInfo, len(coreClients))
	for _, info := range coreClients {
		credential := strings.TrimSpace(info.Password)
		if credential == "" {
			continue
		}
		existing[credential] = info
	}

	now := time.Now().UnixMilli()
	mainCredential := strings.TrimSpace(mainPass)
	type wantedClient struct {
		credential string
		client     model.Client
		active     bool
	}
	wanted := make([]wantedClient, 0, len(clients))
	wantedSet := make(map[string]struct{}, len(clients))
	for _, client := range clients {
		credential := csqttCredential(client)
		if credential == "" || credential == mainCredential {
			continue
		}
		wanted = append(wanted, wantedClient{
			credential: credential,
			client:     client,
			active:     client.Enable && (client.ExpiryTime <= 0 || client.ExpiryTime > now),
		})
		wantedSet[credential] = struct{}{}
	}

	ledger := readLedger(ledgerPath)
	ledgerSet := make(map[string]struct{}, len(ledger))
	removals := make([]string, 0, len(ledger))
	for _, entry := range ledger {
		credential := strings.TrimSpace(entry)
		if credential == "" || credential == mainCredential {
			continue
		}
		if _, keep := wantedSet[credential]; keep {
			ledgerSet[credential] = struct{}{}
			continue
		}
		removals = append(removals, credential)
	}
	// Removals go first: a renamed client still holds its old display name.
	if len(removals) > 0 {
		if err := c.post(csqttAPIDeletePath, csqttPasswordsPayload{Passwords: removals}); err != nil {
			return err
		}
	}

	for _, item := range wanted {
		info, found := existing[item.credential]
		if !found {
			payload := csqttCreatePayload{
				Name:     item.client.Email,
				Days:     csqttDays(item.client.ExpiryTime, now),
				Password: item.credential,
				Hash:     hashFor(hashesByEmail, item.client.Email, defaultHash),
			}
			if err := c.post(csqttAPIClientsPath, payload); err != nil {
				return err
			}
			ledgerSet[item.credential] = struct{}{}
			existing[item.credential] = csqttClientInfo{Password: item.credential, Active: true}
			if !item.active {
				if err := c.toggle(item.credential, false); err != nil {
					return err
				}
				existing[item.credential] = csqttClientInfo{Password: item.credential, Active: false}
			}
			continue
		}
		if info.Active == item.active {
			continue
		}
		if err := c.toggle(item.credential, item.active); err != nil {
			return err
		}
		info.Active = item.active
		existing[item.credential] = info
	}

	return writeLedger(ledgerPath, setKeys(ledgerSet))
}

func (c *CSQTTClient) toggle(credential string, active bool) error {
	return c.post(csqttAPITogglePath, csqttTogglePayload{
		Passwords: []string{credential},
		Active:    active,
	})
}

func (c *CSQTTClient) post(path string, payload any) error {
	_, err := c.request(http.MethodPost, path, payload)
	return err
}

func (c *CSQTTClient) listClients(decodeLabel string) ([]csqttClientInfo, error) {
	body, err := c.request(http.MethodGet, csqttAPIClientsPath, nil)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", decodeLabel, err)
	}
	out := make([]csqttClientInfo, 0, len(entries))
	for _, entry := range entries {
		var info csqttClientInfo
		if err := json.Unmarshal(entry, &info); err != nil {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// request performs one call and, on an authentication failure, logs in once
// and retries the same call exactly once.
func (c *CSQTTClient) request(method, path string, payload any) ([]byte, error) {
	var body []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = encoded
	}
	for attempt := 0; ; attempt++ {
		status, responseBody, err := c.send(method, path, body)
		if err != nil {
			return nil, err
		}
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			c.markAccepted()
			return responseBody, nil
		}
		if (status == http.StatusUnauthorized || status == http.StatusForbidden) && attempt == 0 {
			if err := c.login(); err != nil {
				return nil, err
			}
			continue
		}
		return nil, fmt.Errorf("csqtt api %s %s: %s", method, path, strings.TrimSpace(string(responseBody)))
	}
}

func (c *CSQTTClient) send(method, path string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	state, pass := c.authSnapshot()
	if state != csqttAuthCookie && pass != "" {
		req.Header.Set("X-API-Key", pass)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// An oversized body is truncated here and fails to decode upstream.
	data, _ := io.ReadAll(io.LimitReader(resp.Body, csqttAPIMaxBody))
	return resp.StatusCode, data, nil
}

// login authenticates and caches the child's lockout answer so a wrong
// password does not hammer the endpoint.
func (c *CSQTTClient) login() error {
	c.mu.Lock()
	if c.loginError != "" && time.Now().Before(c.loginErrorTil) {
		cached := c.loginError
		c.mu.Unlock()
		return errors.New(cached)
	}
	c.mu.Unlock()

	payload, err := json.Marshal(csqttLoginPayload{
		User: csqttCaesar(c.user),
		Pass: csqttCaesar(c.pass),
	})
	if err != nil {
		return err
	}
	status, body, err := c.send(http.MethodPost, csqttAPILoginPath, payload)
	if err != nil {
		return err
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		c.mu.Lock()
		c.state = csqttAuthCookie
		c.loginError = ""
		c.loginErrorTil = time.Time{}
		c.mu.Unlock()
		return nil
	}

	failure := errors.New("csqtt api login: " + strings.TrimSpace(string(body)))
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		c.mu.Lock()
		c.loginError = failure.Error()
		c.loginErrorTil = time.Now().Add(csqttAPILockout)
		c.mu.Unlock()
	}
	return failure
}

func (c *CSQTTClient) authSnapshot() (csqttAuthState, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.pass
}

func (c *CSQTTClient) authState() csqttAuthState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// markAccepted records that the plain X-API-Key header was enough.
func (c *CSQTTClient) markAccepted() {
	c.mu.Lock()
	if c.state == csqttAuthUnknown {
		c.state = csqttAuthAPIKey
	}
	c.mu.Unlock()
}

// csqttCaesar reproduces the login form's byte-offset + base64 obfuscation.
func csqttCaesar(value string) string {
	shifted := []byte(value)
	for i := range shifted {
		shifted[i] += csqttAPIByteShift
	}
	return csqttAPICredPrefix + base64.StdEncoding.EncodeToString(shifted)
}

// csqttDays converts an expiry timestamp into the request's day count.
func csqttDays(expiry, now int64) int {
	if expiry <= 0 {
		return 0
	}
	days := (expiry - now) / csqttAPIDayMillis
	if days < 1 {
		return 1
	}
	if days > csqttAPIDaysMax {
		return csqttAPIDaysMax
	}
	return int(days)
}
