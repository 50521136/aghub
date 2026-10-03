package portal

import (
	"net/netip"
	"sync"
	"time"
)

// loginLimiter counts the failed sign-in attempts per client address and locks
// an address out for a while once it makes too many.
//
// The portal signs in users that are not administrators, so it cannot rely on
// the administrator rate limiter, which is shared with the administrator
// interface and has its own lockout policy.
type loginLimiter struct {
	now      func() time.Time
	attempts map[netip.Addr]*attemptRecord
	window   time.Duration
	max      int
	mu       sync.Mutex
}

// attemptRecord is the state of a single address.
type attemptRecord struct {
	// first is when the current counting window started.
	first time.Time

	// lockedUntil is when the lockout ends.  It is zero when the address is
	// not locked out.
	lockedUntil time.Time

	// count is the number of failed attempts within the window.
	count int
}

// maxTrackedAddresses bounds the memory the limiter uses.  The map is pruned
// when it grows past this, which is only reachable by an attacker.
const maxTrackedAddresses = 4096

// newLoginLimiter returns a limiter that locks an address out for window after
// max failed attempts.
func newLoginLimiter(max int, window time.Duration, now func() time.Time) (l *loginLimiter) {
	return &loginLimiter{
		now:      now,
		attempts: map[netip.Addr]*attemptRecord{},
		window:   window,
		max:      max,
	}
}

// allow returns true if the address may attempt a sign-in.
func (l *loginLimiter) allow(ip netip.Addr) (ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	r, found := l.attempts[ip]
	if !found {
		return true
	}

	return !l.now().Before(r.lockedUntil)
}

// fail records a failed attempt.
func (l *loginLimiter) fail(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.attempts) >= maxTrackedAddresses {
		l.pruneLocked()
	}

	now := l.now()
	r, found := l.attempts[ip]
	if !found || now.Sub(r.first) > l.window {
		r = &attemptRecord{first: now}
		l.attempts[ip] = r
	}

	r.count++
	if r.count >= l.max {
		r.lockedUntil = now.Add(l.window)
		r.count = 0
		r.first = now
	}
}

// succeed forgets the attempts of an address.
func (l *loginLimiter) succeed(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, ip)
}

// pruneLocked drops the records that are neither locked out nor counting.
func (l *loginLimiter) pruneLocked() {
	now := l.now()

	for ip, r := range l.attempts {
		if now.Before(r.lockedUntil) {
			continue
		}

		if now.Sub(r.first) > l.window {
			delete(l.attempts, ip)
		}
	}
}
