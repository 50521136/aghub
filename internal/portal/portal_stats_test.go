package portal

import (
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testStats is a [StatsSource] that reports a fixed summary.
type testStats struct {
	summary StatsSummary
}

// type check
var _ StatsSource = testStats{}

// Stats24h implements the [StatsSource] interface.
func (s testStats) Stats24h() (sum StatsSummary) {
	return s.summary
}

func TestPublicStatsWithStats(t *testing.T) {
	store := newTestStore()
	// Passed is the cumulative per-account figure; the public page must
	// report the 24-hour figure from the stats module instead of it.
	store.summary = &users.Summary{Total: 1, TotalRequests: 5, Passed: 7}

	m, _ := newTestPortal(t, store)
	m.stats = testStats{summary: StatsSummary{Queries: 100, Blocked: 20, Passed: 3}}

	s := m.PublicStats()

	assert.Equal(t, int64(100), s.Queries24h)
	assert.Equal(t, int64(20), s.Blocked24h)
	assert.Equal(t, int64(3), s.Passed24h)
}

// TestPublicStatsWithoutStats checks that a deployment without the statistics
// module still renders the public page instead of panicking, and that the
// cumulative per-account figure does not leak into the 24-hour field.
func TestPublicStatsWithoutStats(t *testing.T) {
	store := newTestStore()
	store.summary = &users.Summary{Total: 1, TotalRequests: 5, Passed: 7}

	m, _ := newTestPortal(t, store)
	require.Nil(t, m.stats)

	s := m.PublicStats()

	assert.Zero(t, s.Queries24h)
	assert.Zero(t, s.Blocked24h)
	assert.Zero(t, s.Passed24h)
}
