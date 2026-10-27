// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimit configures request limits. Buckets live in this process only:
// with several replicas, each enforces its own limits.
type RateLimit struct {
	PerMinute    int // requests per minute per authenticated actor; 0 disables
	Burst        int // bucket size of the per-actor limit (default PerMinute)
	AuthFailures int // failed authentications per minute per client IP; 0 disables
}

// now is the limiter clock, replaced in tests.
var now = time.Now

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a set of token buckets refilled at perMinute, holding at most burst.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	calls   int
}

func newLimiter(perMinute, burst int) *limiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	return &limiter{rate: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}}
}

// refill returns key's bucket brought up to date; l.mu must be held.
func (l *limiter) refill(key string, t time.Time) *bucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: t}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+t.Sub(b.last).Seconds()*l.rate)
	b.last = t
	l.calls++
	if l.calls%1024 == 0 {
		l.prune(t)
	}
	return b
}

// prune drops full buckets idle for more than ten minutes; l.mu must be held.
func (l *limiter) prune(t time.Time) {
	for k, b := range l.buckets {
		if t.Sub(b.last) > 10*time.Minute && b.tokens+t.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

func (l *limiter) wait(b *bucket) time.Duration {
	return time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// take consumes one token; when none is left it reports how long to wait.
func (l *limiter) take(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, now())
	if b.tokens < 1 {
		return false, l.wait(b)
	}
	b.tokens--
	return true, 0
}

// blocked reports, without consuming, whether key's bucket is empty.
func (l *limiter) blocked(key string) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, now())
	if b.tokens < 1 {
		return l.wait(b), true
	}
	return 0, false
}

// clientIP is the connection's address; forwarding headers are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func retryAfterSeconds(d time.Duration) string {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}
