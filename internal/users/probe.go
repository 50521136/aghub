package users

import (
	"crypto/rand"
	"encoding/hex"
	"net/netip"
	"strings"
	"time"
)

// probePrefix is the label prefix of a connectivity probe.
//
// A probe is how the user portal answers the only question that matters to
// somebody who has just configured encrypted DNS: is *this* device actually
// using us?  The portal hands the browser a name to resolve and then asks
// whether that exact name ever arrived.  The name carries a random token, so a
// match proves that the device that loaded the page resolved it through us --
// no address comparison and no guesswork about which of the account's devices
// is which.
//
// The prefix exists so that the query path can reject a name without taking a
// lock: ordinary traffic never matches it, and the map is touched only for the
// handful of probes in flight.
const probePrefix = "aghub-probe-"

// ProbeTTL is how long a probe stays matchable.  It only has to outlive the
// round trip from rendering the page to the browser resolving the name, so a
// few minutes is generous; keeping it short keeps the map small and stops a
// token from being useful long after it was issued.
const ProbeTTL = 3 * time.Minute

// probeTokenBytes is the size of the random part of a probe label.  Twelve
// bytes is far beyond guessable and keeps the label short.
const probeTokenBytes = 12

// ProbeHit describes the query that matched a probe.
type ProbeHit struct {
	// ClientID is the client identifier the query came in under.  For a
	// device using encrypted DNS it is the name it connected with, which is
	// the account identifier the operator handed out.
	ClientID string `json:"client_id"`

	// IP is the address the query came from.
	IP string `json:"ip"`

	// At is the Unix timestamp of the query.
	At int64 `json:"at"`
}

// probeEntry is one registered probe.
type probeEntry struct {
	// uid is the account that asked for the probe.  A status request for
	// somebody else's token finds nothing.
	uid string

	// expires is when the probe stops being matchable.
	expires time.Time

	// hit is set once a query for the probe name arrives.
	hit *ProbeHit
}

// RegisterProbe issues a probe for the given account and returns the label the
// browser must resolve.
//
// The caller appends its own domain to build the full name; the label is what
// carries the token.
func (m *Manager) RegisterProbe(uid string) (label string) {
	token := make([]byte, probeTokenBytes)
	if _, err := rand.Read(token); err != nil {
		// The random source failing is not something a caller can act on,
		// and the portal must still render.  An unguessable token is the
		// point of the probe, so fall back to a value that simply never
		// matches rather than one that is predictable.
		return probePrefix + "unavailable"
	}

	label = probePrefix + hex.EncodeToString(token)

	now := m.now()

	m.probesMu.Lock()
	defer m.probesMu.Unlock()

	m.pruneProbesLocked(now)
	m.probes[label] = &probeEntry{uid: uid, expires: now.Add(ProbeTTL)}

	return label
}

// ProbeStatus returns the hit of a probe registered by the given account, if
// the query has arrived yet.
func (m *Manager) ProbeStatus(uid, label string) (hit *ProbeHit, ok bool) {
	now := m.now()

	m.probesMu.Lock()
	defer m.probesMu.Unlock()

	m.pruneProbesLocked(now)

	e, found := m.probes[label]
	if !found || e.uid != uid {
		return nil, false
	}

	if e.hit == nil {
		return nil, false
	}

	h := *e.hit

	return &h, true
}

// matchProbe records a query that matches a registered probe, if any.  It is
// called on the query path and must stay cheap for ordinary traffic.
func (m *Manager) matchProbe(qname, clientID string, ip netip.Addr) {
	label, _, _ := strings.Cut(qname, ".")
	if !strings.HasPrefix(label, probePrefix) {
		return
	}

	now := m.now()

	m.probesMu.Lock()
	defer m.probesMu.Unlock()

	m.pruneProbesLocked(now)

	e, found := m.probes[label]
	if !found || e.hit != nil {
		// Keep the first hit: it is the one closest to the page load, and a
		// retry from a different address must not overwrite it.
		return
	}

	e.hit = &ProbeHit{
		ClientID: clientID,
		IP:       ip.String(),
		At:       now.Unix(),
	}
}

// pruneProbesLocked drops expired probes.  The caller must hold probesMu.
func (m *Manager) pruneProbesLocked(now time.Time) {
	for label, e := range m.probes {
		if !now.Before(e.expires) {
			delete(m.probes, label)
		}
	}
}
