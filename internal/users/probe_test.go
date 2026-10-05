package users

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProbeMatch covers the connectivity probe: the portal issues a name, the
// device resolves it, and the resolver side records that it arrived.
func TestProbeMatch(t *testing.T) {
	m, now := newTestManager(t)

	label := m.RegisterProbe("uid-one")
	require.NotEmpty(t, label)

	// Nothing has arrived yet.
	hit, ok := m.ProbeStatus("uid-one", label)
	assert.False(t, ok)
	assert.Nil(t, hit)

	// Ordinary traffic must not match, even when it is the same length.
	m.matchProbe("aghub-probe-somethingelse.example.com.", "uid-one", netip.MustParseAddr("192.0.2.1"))

	hit, ok = m.ProbeStatus("uid-one", label)
	assert.False(t, ok)
	assert.Nil(t, hit)

	// The real query matches.
	qname := label + ".dns.example.com."
	m.matchProbe(qname, "lz0423", netip.MustParseAddr("192.0.2.10"))

	hit, ok = m.ProbeStatus("uid-one", label)
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "lz0423", hit.ClientID)
	assert.Equal(t, "192.0.2.10", hit.IP)
	assert.Equal(t, now.Unix(), hit.At)

	// The first hit wins, so a later retry from another address cannot
	// overwrite the evidence of which connection the page was loaded on.
	m.matchProbe(qname, "lz0423", netip.MustParseAddr("198.51.100.7"))

	hit, ok = m.ProbeStatus("uid-one", label)
	require.True(t, ok)
	assert.Equal(t, "192.0.2.10", hit.IP)
}

// TestProbeIsolation checks that a probe only answers to the account that
// issued it, and stops answering once it expires.
func TestProbeIsolation(t *testing.T) {
	m, now := newTestManager(t)

	label := m.RegisterProbe("uid-one")
	m.matchProbe(label+".example.com.", "uid-one", netip.MustParseAddr("192.0.2.1"))

	// Another account asking about the same label learns nothing.
	_, ok := m.ProbeStatus("uid-two", label)
	assert.False(t, ok)

	*now = now.Add(ProbeTTL + time.Second)

	_, ok = m.ProbeStatus("uid-one", label)
	assert.False(t, ok, "an expired probe must not answer")

	// Expired entries are dropped rather than kept forever.
	m.RegisterProbe("uid-three")

	m.probesMu.Lock()
	n := len(m.probes)
	m.probesMu.Unlock()

	assert.Equal(t, 1, n)
}
