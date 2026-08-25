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

var subLimiter = newSubRateLimiter()

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
	if len(l.counts) >= subRateLimitMaxIPs {
		// At the ceiling, reclaim expired windows before admitting a new IP.
		for key, rec := range l.counts {
			if now.Sub(rec.windowStart) >= time.Minute {
				delete(l.counts, key)
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

// subRateLimitMiddleware throttles the public subscription endpoints per
// client IP. The sub server runs with no trusted proxies, so gin's ClientIP
// is the socket peer, not a spoofable header.
func subRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if net.ParseIP(ip) == nil {
			ip = c.Request.RemoteAddr
		}
		if !subLimiter.allow(ip) {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}
