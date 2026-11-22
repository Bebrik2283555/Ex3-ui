package sub

import (
	"fmt"
	"testing"
	"time"
)

// Full sweeps of the ceiling-sized map are throttled: a flood must not walk
// every entry on each request.
func TestSubRateLimiterSweepsAtMostEveryFiveSeconds(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newSubRateLimiter()
	l.now = func() time.Time { return now }
	fillExpired := func() {
		for i := 0; i < subRateLimitMaxIPs; i++ {
			l.counts[fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)] = &subRateRecord{windowStart: now.Add(-2 * time.Minute)}
		}
	}

	fillExpired()
	l.allow("192.0.2.1")
	if !l.lastSweep.Equal(now) {
		t.Fatalf("first ceiling hit did not sweep: lastSweep=%v, want %v", l.lastSweep, now)
	}

	now = now.Add(4 * time.Second)
	fillExpired()
	l.allow("192.0.2.2")
	if !l.lastSweep.Equal(time.Unix(1_700_000_000, 0)) {
		t.Fatalf("swept again after 4s: lastSweep=%v", l.lastSweep)
	}

	now = now.Add(time.Second)
	fillExpired()
	l.allow("192.0.2.3")
	if !l.lastSweep.Equal(now) {
		t.Fatalf("no sweep after 5s: lastSweep=%v, want %v", l.lastSweep, now)
	}
}

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
