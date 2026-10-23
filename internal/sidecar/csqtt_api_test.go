package sidecar

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestCSQTTCaesarMatchesLoginForm(t *testing.T) {
	encoded := csqttCaesar("ex3ui")
	if !strings.HasPrefix(encoded, "c1:") {
		t.Fatalf("encoded = %q, want the c1: prefix", encoded)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "c1:"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []byte{148, 167, 98, 164, 152}
	if string(raw) != string(want) {
		t.Fatalf("encoded bytes = %v, want %v", raw, want)
	}
}

// 2.1.9 protects its API with a cookie session; the client must log in once,
// reuse the cookie for later polls, and leave 2.5.0's batch sync alone.
func TestCSQTTLegacyCookieAuth(t *testing.T) {
	var logins, gets, posts int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/login":
			atomic.AddInt32(&logins, 1)
			var req struct {
				User string `json:"user"`
				Pass string `json:"pass"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if req.User != csqttCaesar("ex3ui") || req.Pass != csqttCaesar("web-pass") {
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: "csqtt_session", Value: "token", Path: "/", Secure: true, HttpOnly: true,
			})
			_, _ = w.Write([]byte("ok"))
		case "/api/clients":
			if r.Method == http.MethodPost {
				atomic.AddInt32(&posts, 1)
			}
			cookie, err := r.Cookie("csqtt_session")
			if err != nil || cookie.Value != "token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.Method == http.MethodGet {
				atomic.AddInt32(&gets, 1)
			}
			_ = json.NewEncoder(w).Encode([]csqttClientInfo{{
				Password: "client-pass", Up: 10, Down: 20, ActiveSessions: 1,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newCSQTTClient(srv.URL, "ex3ui", "web-pass")
	clients, err := c.ClientTraffic()
	if err != nil {
		t.Fatalf("ClientTraffic: %v", err)
	}
	if len(clients) != 1 || clients[0].Password != "client-pass" || clients[0].Up != 10 {
		t.Fatalf("clients = %+v", clients)
	}
	if _, err := c.ClientTraffic(); err != nil {
		t.Fatalf("second ClientTraffic: %v", err)
	}
	if got := atomic.LoadInt32(&logins); got != 1 {
		t.Fatalf("logins = %d, want one cached session", got)
	}

	if err := c.SyncClients([]model.Client{{
		Email: "a@x", Password: "client-pass", Enable: true,
	}}, "", "", "", nil); err != nil {
		t.Fatalf("SyncClients: %v", err)
	}
	if got := atomic.LoadInt32(&posts); got != 0 {
		t.Fatalf("posts = %d, want no 2.5.0 batch calls", got)
	}
	if got := atomic.LoadInt32(&gets); got < 3 {
		t.Fatalf("gets = %d, want traffic polls plus sync", got)
	}
}
