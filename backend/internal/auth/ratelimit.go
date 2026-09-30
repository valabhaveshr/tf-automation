package auth

import (
	"errors"
	"sync"
	"time"
)

// ErrTooManyAttempts is a sentinel describing why the rate limiter rejected
// an attempt; callers typically translate this into a 429 response.
var ErrTooManyAttempts = errors.New("too many login attempts, please try again later")

// RateLimiter is a simple in-memory fixed-window rate limiter, used to
// throttle POST /auth/login per the locked Phase 1 "Security" edge case
// (basic brute-force login rate limiting). It is keyed by an arbitrary
// string -- LoginHandler keys by username, falling back to remote IP when
// the username is blank/unknown, so an attacker can't dodge the limit by
// varying only the password for a fixed username, nor exhaust a shared
// bucket by varying the username from a single source.
//
// IMPORTANT (Phase 1 limitation, documented not accidental): this limiter
// is single-instance only. State lives in a process-local, mutex-guarded
// map, so it is reset on restart and is NOT shared across multiple backend
// replicas behind a load balancer -- an attacker distributing attempts
// across replicas would get max attempts per replica, not globally. If/when
// this service is horizontally scaled, this needs to move to a shared store
// (e.g. Redis, with atomic INCR+EXPIRE) so attempts against one instance are
// visible to the others. Acceptable as-is for Phase 1's single-instance
// deployment target.
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

// NewRateLimiter constructs a limiter allowing at most max attempts per key
// within the given sliding window.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

// DefaultLoginRateLimiter returns a limiter configured for 5 attempts per 5
// minutes per key -- a reasonable Phase 1 default for /auth/login.
func DefaultLoginRateLimiter() *RateLimiter {
	return NewRateLimiter(5, 5*time.Minute)
}

// Allow records an attempt for key and reports whether it is within the
// allowed threshold for the current window. Call it once per login attempt
// (regardless of whether credentials turn out to be valid) before
// evaluating credentials, so failed and successful attempts both count
// against the budget.
func (r *RateLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-r.window)

	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.attempts[key]
	kept := existing[:0]
	for _, t := range existing {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= r.max {
		r.attempts[key] = kept
		return false
	}

	kept = append(kept, now)
	r.attempts[key] = kept
	return true
}
