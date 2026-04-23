package auth

import (
	"sync"
	"time"
)

// LoginRateLimiter is a per-IP token bucket used by the /login handler. It
// is intentionally in-process: a single Teal node serves a single host, so
// distributed rate limiting would be over-engineering. A restart wipes the
// state, which is acceptable — the protection is against scripted brute
// force, not against a careful attacker.
//
// Bucket semantics:
//   - capacity tokens fill the bucket; each Allow() call consumes one if
//     available.
//   - tokens regenerate at refillEvery. With capacity=5 and refillEvery=1m,
//     callers get 5 attempts immediately and a 6th attempt 1 minute later.
type LoginRateLimiter struct {
	capacity    int
	refillEvery time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens     int
	lastRefill time.Time
}

// NewLoginRateLimiter constructs a limiter with the given bucket capacity
// and per-token refill interval.
func NewLoginRateLimiter(capacity int, refillEvery time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		capacity:    capacity,
		refillEvery: refillEvery,
		buckets:     map[string]*bucket{},
		swept:       time.Now(),
	}
}

// Allow consumes one token for key (typically the client IP). Returns true
// if the request is permitted, false if the bucket is empty.
func (l *LoginRateLimiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.maybeSweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, lastRefill: now}
		l.buckets[key] = b
	}
	// Refill: add one token per refillEvery elapsed since lastRefill, capped
	// at capacity.
	elapsed := now.Sub(b.lastRefill)
	if elapsed >= l.refillEvery {
		add := int(elapsed / l.refillEvery)
		if add > 0 {
			b.tokens += add
			if b.tokens > l.capacity {
				b.tokens = l.capacity
			}
			b.lastRefill = b.lastRefill.Add(time.Duration(add) * l.refillEvery)
		}
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// maybeSweep removes buckets that have been full for a while, to prevent
// unbounded memory growth from one-off login attempts. Cheap enough to
// inline into Allow.
func (l *LoginRateLimiter) maybeSweep(now time.Time) {
	if now.Sub(l.swept) < 5*time.Minute {
		return
	}
	for k, b := range l.buckets {
		if b.tokens >= l.capacity && now.Sub(b.lastRefill) > 5*time.Minute {
			delete(l.buckets, k)
		}
	}
	l.swept = now
}
