package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginLimiter throttles POST /v1/auth/login per client, so a brute-force
// script can't hammer the endpoint with unlimited guesses. It's a sliding-
// window counter: allow prunes hits older than window before checking
// whether the key is already at maxAttempts.
//
// This limiter is in-process/per-instance. That's fine for a single Knobs
// instance, but a multi-instance deployment would need a shared store (e.g.
// Redis) — otherwise each instance enforces its own independent budget per
// key, and a client could get maxAttempts per instance rather than overall.
type loginLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	hits        map[string][]time.Time

	// now is the limiter's clock. It defaults to time.Now and is
	// overridden in tests so the window-expiry path is deterministic
	// without sleeping through a real window.
	now func() time.Time
}

// newLoginLimiter builds a limiter that allows at most maxAttempts recorded
// hits per key within window.
func newLoginLimiter(maxAttempts int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		hits:        make(map[string][]time.Time),
		now:         time.Now,
	}
}

// allow reports whether key is still within budget. Call it once per FAILED
// login attempt: if key already has maxAttempts or more hits within the
// current window, it returns false and records nothing further; otherwise
// it records this attempt and returns true.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	kept := l.pruneLocked(key, now)

	if len(kept) >= l.maxAttempts {
		l.hits[key] = kept
		return false
	}

	l.hits[key] = append(kept, now)
	return true
}

// underLimit reports whether key is still under budget, WITHOUT recording a
// new attempt. handleLogin calls this before checking credentials, so a
// client already over budget is rejected up front; allow (called only on a
// failed credential check) is what actually records an attempt, so a
// successful login — and the pre-check itself — never consumes budget.
func (l *loginLimiter) underLimit(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.pruneLocked(key, l.now())
	l.hits[key] = kept
	return len(kept) < l.maxAttempts
}

// pruneLocked drops key's hits older than the window as of now, returning
// what's left. Caller must hold l.mu.
func (l *loginLimiter) pruneLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, h := range l.hits[key] {
		if h.After(cutoff) {
			kept = append(kept, h)
		}
	}
	return kept
}

// clientIP extracts the caller's address for rate-limit keying: the host
// part of RemoteAddr, or — only when trustProxy is true — the first hop of
// X-Forwarded-For when present.
//
// X-Forwarded-For is entirely client-settable on a direct connection (no
// proxy involved), so trusting it unconditionally would defeat the login
// limiter outright: an attacker sends a fresh, made-up value on every
// request and gets a brand-new rate-limit bucket each time. It's only safe
// to read once the operator has confirmed (via TRUST_PROXY=true /
// config.Config.TrustProxy) that a trusted reverse proxy sits in front of
// Knobs and is the one actually setting/overwriting this header — at which
// point RemoteAddr would otherwise just be the proxy's own address for
// every client.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first := strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
			if first != "" {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
