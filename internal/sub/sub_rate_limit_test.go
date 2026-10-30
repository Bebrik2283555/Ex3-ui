package sub

import "testing"

// The limiter must bucket by the socket peer unless that peer is a configured
// trusted proxy: gin's ClientIP trusts X-Forwarded-For from anyone, so keying
// on it let a spoofed header mint a fresh bucket per request.
func TestSubClientIPIgnoresSpoofedForwardedFor(t *testing.T) {
	initSubDB(t)
	a := &SUBController{subService: &SubService{}}

	c := requestFrom(t, "203.0.113.9:51000", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got := a.subClientIP(c); got != "203.0.113.9" {
		t.Fatalf("untrusted peer keyed on %q, want 203.0.113.9", got)
	}

	setTrustedProxyCIDRs(t, "10.0.0.0/8")

	c = requestFrom(t, "10.1.2.3:44000", map[string]string{"X-Forwarded-For": "198.51.100.7, 198.51.100.8"})
	if got := a.subClientIP(c); got != "198.51.100.8" {
		t.Fatalf("trusted proxy keyed on %q, want the right-most forwarded hop", got)
	}

	c = requestFrom(t, "10.1.2.3:44000", map[string]string{"X-Real-IP": "198.51.100.9"})
	if got := a.subClientIP(c); got != "198.51.100.9" {
		t.Fatalf("trusted proxy with X-Real-IP keyed on %q, want 198.51.100.9", got)
	}

	c = requestFrom(t, "10.1.2.3:44000", nil)
	if got := a.subClientIP(c); got != "10.1.2.3" {
		t.Fatalf("trusted proxy without forwarded headers keyed on %q, want the peer", got)
	}

	// A header that is not an IP must not become a map key.
	c = requestFrom(t, "10.1.2.3:44000", map[string]string{"X-Forwarded-For": "not-an-ip"})
	if got := a.subClientIP(c); got != "10.1.2.3" {
		t.Fatalf("invalid forwarded value keyed on %q, want the peer", got)
	}
}
