package api

import (
	"net/http"
	"testing"
	"time"
)

// TestLoginLimiterAllowsUpToMaxThenBlocks exercises the limiter directly, with
// no HTTP involved. It lives in package api (not api_test) because
// loginLimiter and its fields are unexported.
func TestLoginLimiterAllowsUpToMaxThenBlocks(t *testing.T) {
	clock := time.Now()
	l := newLoginLimiter(3, time.Minute)
	l.now = func() time.Time { return clock }

	for i := 1; i <= 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d: want allowed", i)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("4th attempt over budget: want blocked")
	}
}

func TestLoginLimiterKeysAreIndependent(t *testing.T) {
	clock := time.Now()
	l := newLoginLimiter(1, time.Minute)
	l.now = func() time.Time { return clock }

	if !l.allow("client-a") {
		t.Fatal("first key: want allowed")
	}
	if !l.allow("client-b") {
		t.Fatal("different key: want allowed independently of client-a's budget")
	}
	if l.allow("client-a") {
		t.Fatal("client-a's second attempt: want blocked")
	}
}

func TestLoginLimiterResetsAfterWindowElapses(t *testing.T) {
	clock := time.Now()
	l := newLoginLimiter(1, time.Minute)
	l.now = func() time.Time { return clock }

	if !l.allow("k") {
		t.Fatal("first attempt: want allowed")
	}
	if l.allow("k") {
		t.Fatal("second attempt inside window: want blocked")
	}

	clock = clock.Add(time.Minute + time.Second)
	if !l.allow("k") {
		t.Fatal("attempt after window elapses: want allowed again")
	}
}

func TestLoginLimiterUnderLimitDoesNotRecord(t *testing.T) {
	clock := time.Now()
	l := newLoginLimiter(1, time.Minute)
	l.now = func() time.Time { return clock }

	// Peeking repeatedly must not itself consume budget.
	for i := 0; i < 5; i++ {
		if !l.underLimit("k") {
			t.Fatalf("peek %d: want under limit (peeking shouldn't record)", i)
		}
	}
	if !l.allow("k") {
		t.Fatal("first real attempt after peeking: want allowed")
	}
	if l.underLimit("k") {
		t.Fatal("after one recorded attempt at maxAttempts=1: want over limit")
	}
}

func TestClientIPTrustProxyTrueUsesFirstForwardedForHop(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")

	if got := clientIP(r, true); got != "203.0.113.9" {
		t.Fatalf("clientIP(trustProxy=true) = %q, want 203.0.113.9", got)
	}
}

func TestClientIPTrustProxyFalseIgnoresForwardedFor(t *testing.T) {
	// This is the security-relevant case: without an explicit TRUST_PROXY
	// opt-in, X-Forwarded-For is attacker-controlled and must never be
	// used, or the login limiter becomes trivially bypassable (fresh XFF
	// value per request -> fresh bucket per request).
	r, _ := http.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")

	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Fatalf("clientIP(trustProxy=false) = %q, want 10.0.0.1 (RemoteAddr host, XFF ignored)", got)
	}
}

func TestClientIPFallsBackToRemoteAddrHost(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	r.RemoteAddr = "192.0.2.5:5555"

	if got := clientIP(r, false); got != "192.0.2.5" {
		t.Fatalf("clientIP = %q, want 192.0.2.5", got)
	}
	if got := clientIP(r, true); got != "192.0.2.5" {
		t.Fatalf("clientIP(trustProxy=true, no XFF header) = %q, want 192.0.2.5", got)
	}
}
