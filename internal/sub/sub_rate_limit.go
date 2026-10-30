package sub

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// subRateLimitPerMinute caps public subscription requests per IP so a
	// client storm (or a bot) cannot burn CPU on link generation. Legitimate
	// VPN clients update subscriptions at most every few minutes.
	subRateLimitPerMinute = 120
	// subRateLimitMaxIPs bounds the tracking map under a spoofed-source flood.
	subRateLimitMaxIPs = 10000
)

type subRateLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	counts map[string]*subRateRecord
}

type subRateRecord struct {
	windowStart time.Time
	count       int
}

func newSubRateLimiter() *subRateLimiter {
	return &subRateLimiter{now: time.Now, counts: make(map[string]*subRateRecord)}
}

func (l *subRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if _, known := l.counts[ip]; !known && len(l.counts) >= subRateLimitMaxIPs {
		// At the ceiling, reclaim expired windows before admitting a new IP.
		for key, rec := range l.counts {
			if now.Sub(rec.windowStart) >= time.Minute {
				delete(l.counts, key)
			}
		}
		// Still full: every tracked IP is live, so drop one arbitrary entry to
		// keep the map bounded instead of growing past the cap.
		if len(l.counts) >= subRateLimitMaxIPs {
			for key := range l.counts {
				delete(l.counts, key)
				break
			}
		}
	}
	rec := l.counts[ip]
	if rec == nil || now.Sub(rec.windowStart) >= time.Minute {
		rec = &subRateRecord{windowStart: now}
		l.counts[ip] = rec
	}
	rec.count++
	return rec.count <= subRateLimitPerMinute
}

// middleware throttles the public subscription endpoints per client IP. The
// key comes from keyFor, which treats the socket peer as authoritative and
// only honors forwarded headers from a trusted proxy: gin's ClientIP trusts
// every X-Forwarded-For by default, so a spoofed header would otherwise give
// an attacker a fresh bucket per request.
func (l *subRateLimiter) middleware(keyFor func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := keyFor(c)
		if net.ParseIP(ip) == nil {
			ip = c.Request.RemoteAddr
		}
		if !l.allow(ip) {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}
