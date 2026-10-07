package users

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// latencyOf returns the day and lifetime averages of one account as the pages
// read them.
func latencyOf(t *testing.T, m *Manager, uid string) (today float64, todayN int64, total float64, totalN int64) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	us := m.usage[uid]
	require.NotNil(t, us)

	today, todayN = us.latencyToday(m.now())
	total, totalN = us.latencyTotal()

	return today, todayN, total, totalN
}

func TestLatencyAveragesWhatWasRecorded(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	// Three queries of 10, 20 and 30 milliseconds average 20.
	m.ObserveLatency(checkinIP.String(), checkinIP, 10*time.Millisecond)
	m.ObserveLatency(checkinIP.String(), checkinIP, 20*time.Millisecond)
	m.ObserveLatency(checkinIP.String(), checkinIP, 30*time.Millisecond)

	today, todayN, total, totalN := latencyOf(t, m, u.UID)
	assert.InDelta(t, 20.0, today, 0.001)
	assert.Equal(t, int64(3), todayN)
	assert.InDelta(t, 20.0, total, 0.001)
	assert.Equal(t, int64(3), totalN)
}

func TestLatencyIgnoresUnknownClients(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	other := netip.MustParseAddr("198.51.100.7")
	m.ObserveLatency(other.String(), other, 500*time.Millisecond)

	today, todayN, _, totalN := latencyOf(t, m, u.UID)
	assert.Zero(t, today)
	assert.Zero(t, todayN)
	assert.Zero(t, totalN)
}

func TestLatencyMatchesByAddressNotOnlyByClientID(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	// The account owns checkinIP, so a query from it counts even though the
	// client identifier of the request is something else entirely.  This is
	// the same matching the quota check does.
	m.ObserveLatency("some-other-client", checkinIP, 8*time.Millisecond)

	_, todayN, _, _ := latencyOf(t, m, u.UID)
	assert.Equal(t, int64(1), todayN)
}

func TestLatencyNegativeDurationIsDropped(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	m.ObserveLatency(checkinIP.String(), checkinIP, -time.Second)

	_, todayN, _, totalN := latencyOf(t, m, u.UID)
	assert.Zero(t, todayN)
	assert.Zero(t, totalN)
}

func TestLatencyTodayDropsTheHoursBeforeMidnight(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	// The test clock is 12:00 UTC, which is 20:00 in Beijing.  A query now
	// and one two hours later are both inside the Beijing day.
	m.ObserveLatency(checkinIP.String(), checkinIP, 10*time.Millisecond)
	flushHours(t, m)

	*now = now.Add(2 * time.Hour)
	flushHours(t, m)
	m.ObserveLatency(checkinIP.String(), checkinIP, 30*time.Millisecond)
	flushHours(t, m)

	today, todayN, total, totalN := latencyOf(t, m, u.UID)
	assert.InDelta(t, 20.0, today, 0.001)
	assert.Equal(t, int64(2), todayN)

	// The lifetime figures keep both, which is what the total board shows.
	assert.InDelta(t, 20.0, total, 0.001)
	assert.Equal(t, int64(2), totalN)

	// Half a minute into the next Beijing day the day figure is empty while
	// the lifetime one still holds everything.
	*now = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	flushHours(t, m)

	today, todayN, total, totalN = latencyOf(t, m, u.UID)
	assert.Zero(t, today)
	assert.Zero(t, todayN)
	assert.InDelta(t, 20.0, total, 0.001)
	assert.Equal(t, int64(2), totalN)
}

func TestInfoReportsBothLatencyWindows(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	m.ObserveLatency(checkinIP.String(), checkinIP, 40*time.Millisecond)
	flushHours(t, m)

	// A new Beijing day: the day figure resets, the lifetime one does not.
	*now = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	flushHours(t, m)

	i := m.InfoOf(u.UID)
	require.NotNil(t, i)

	assert.Zero(t, i.AvgLatencyTodayMS)
	assert.Zero(t, i.LatencyTodaySamples)
	assert.InDelta(t, 40.0, i.AvgLatencyMS, 0.001)
	assert.Equal(t, int64(1), i.LatencySamples)
}

func TestRankingShowsTheLatencyOfItsWindow(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 100000, PeriodTotal)

	// The board only lists accounts over the request threshold.
	allowQueries(t, m, checkinIP.String(), minRankRequests+10)
	m.ObserveLatency(checkinIP.String(), checkinIP, 25*time.Millisecond)
	flushHours(t, m)

	entries := m.Ranking(10, RankByToday)
	require.Len(t, entries, 1)
	assert.Equal(t, u.Name, entries[0].Name)
	assert.InDelta(t, 25.0, entries[0].AvgLatencyMS, 0.001)
	assert.Equal(t, int64(1), entries[0].LatencySamples)

	// A new Beijing day empties the day board's average but not the total
	// board's: the two boards answer for their own window.
	*now = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	flushHours(t, m)

	// The day board drops accounts that have no requests today, so the
	// account is not on it at all rather than on it with an empty average.
	assert.Empty(t, m.Ranking(10, RankByToday))

	entries = m.Ranking(10, RankByTotal)
	require.Len(t, entries, 1)
	assert.InDelta(t, 25.0, entries[0].AvgLatencyMS, 0.001)
	assert.Equal(t, int64(1), entries[0].LatencySamples)
}

func TestLatencySurvivesAReload(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	m.ObserveLatency(checkinIP.String(), checkinIP, 12*time.Millisecond)
	m.ObserveLatency(checkinIP.String(), checkinIP, 20*time.Millisecond)
	flushHours(t, m)

	// A save writes the buckets out; a reload has to find the same averages,
	// otherwise the numbers would reset on every restart.
	require.NoError(t, m.save())

	m2, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.path,
		Location: time.UTC,
	})
	require.NoError(t, err)

	// The reloaded manager gets the same clock: without it "today" would be
	// the real day and the day window would have nothing in it, which says
	// nothing about whether the numbers were stored.
	m2.now = m.now

	i := m2.InfoOf(u.UID)
	require.NotNil(t, i)

	assert.InDelta(t, 16.0, i.AvgLatencyMS, 0.001)
	assert.Equal(t, int64(2), i.LatencySamples)
	assert.InDelta(t, 16.0, i.AvgLatencyTodayMS, 0.001)
	assert.Equal(t, int64(2), i.LatencyTodaySamples)
}
