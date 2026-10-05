package users

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryUser makes n allowed requests on behalf of the given client identifier.
func queryUser(t *testing.T, m *Manager, id string, n int) {
	t.Helper()

	ip := netip.MustParseAddr("10.0.0.1")

	for range n {
		ok, reason := m.AllowQuery(id, ip, "")
		require.True(t, ok, "query rejected: %s", reason)
	}
}

// flushHistory runs the background history rotation for the given moment.
func flushHistory(m *Manager, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushHistoryLocked(now)
}

// historyOf returns the usage history of the user with the given name.
func historyOf(t *testing.T, m *Manager, name string) (history []UsagePoint) {
	t.Helper()

	for _, i := range m.List() {
		if i.Name == name {
			return i.History
		}
	}

	t.Fatalf("user %q not found", name)

	return nil
}

func TestUsageHistoryRecordsTheDay(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	queryUser(t, m, "alice", 3)

	// The first flush only adopts the day, since the requests that were
	// counted before it happened within the same flush interval.  Nothing has
	// been stored yet, so the report shows the open day alone.
	flushHistory(m, *now)

	history := historyOf(t, m, "alice")
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(3), history[0].Requests)

	// The rollover to the next day closes the bucket of the first one, and
	// the new day opens empty.
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history = historyOf(t, m, "alice")
	require.Len(t, history, 2)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(3), history[0].Requests)
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(0), history[1].Requests)

	// The next day is counted on its own.
	queryUser(t, m, "alice", 2)

	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history = historyOf(t, m, "alice")
	require.Len(t, history, 3)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(3), history[0].Requests)
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(2), history[1].Requests)
	assert.Equal(t, "2026-10-04", history[2].Date)
	assert.Equal(t, int64(0), history[2].Requests)
}

func TestUsageHistoryKeepsOnlyRecentDays(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	flushHistory(m, *now)

	// Make a single request a day for longer than the retention window.
	for range historyDays + 5 {
		queryUser(t, m, "alice", 1)

		*now = now.AddDate(0, 0, 1)
		flushHistory(m, *now)
	}

	history := historyOf(t, m, "alice")
	require.Len(t, history, historyDays)

	// The dropped entries are the oldest ones, so what is left is ordered
	// oldest first and ends with the current day.
	for i := 1; i < len(history); i++ {
		assert.Less(t, history[i-1].Date, history[i].Date)
	}

	assert.Equal(t, "2026-10-08", history[0].Date)
	assert.Equal(t, "2026-11-06", history[len(history)-1].Date)
}

func TestUsageHistorySurvivesReload(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	queryUser(t, m, "alice", 7)

	flushHistory(m, *now)
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	history := historyOf(t, reopened, "alice")
	require.Len(t, history, 2)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(7), history[0].Requests)

	// The day the process was restarted into is still open.
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(0), history[1].Requests)
}

func TestUsageHistoryEmptyForIdleUser(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	flushHistory(m, *now)
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	assert.Empty(t, historyOf(t, m, "alice"))
}

func TestUsageHistoryKeepsIdleDaysAfterActivity(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	queryUser(t, m, "alice", 4)
	flushHistory(m, *now)

	// Two days without a single request, then one more active day.
	for range 2 {
		*now = now.AddDate(0, 0, 1)
		flushHistory(m, *now)
	}

	queryUser(t, m, "alice", 1)
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history := historyOf(t, m, "alice")

	// The idle day is kept as a zero rather than omitted, so that the report
	// shows a continuous timeline.  The current day is appended as well: it
	// has no requests yet, but it is the day the report is looked at on.
	require.Len(t, history, 4)
	assert.Equal(t, []UsagePoint{
		{Date: "2026-10-02", Requests: 4},
		{Date: "2026-10-03", Requests: 0},
		{Date: "2026-10-04", Requests: 1},
		{Date: "2026-10-05", Requests: 0},
	}, history)
}

func TestUsageHistorySurvivesRestartWithinADay(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	queryUser(t, m, "alice", 5)

	// The day has to be adopted and the state flushed before the restart.
	flushHistory(m, *now)
	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	clock := *now
	reopened.now = func() (t time.Time) { return clock }

	// The restarted process must keep counting the same day instead of
	// starting the bucket from zero.
	clock = clock.AddDate(0, 0, 1)
	flushHistory(reopened, clock)

	history := historyOf(t, reopened, "alice")
	require.Len(t, history, 2)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(5), history[0].Requests)

	// The new day is open and empty.
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(0), history[1].Requests)
}

func TestUsageHistoryMarksTheStateForSaving(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	queryUser(t, m, "alice", 3)

	// The request path marks the manager dirty on its own, so clear the flag to
	// see what the rotation does by itself.
	m.dirty.Store(false)
	flushHistory(m, *now)

	// Adopting the day is a change to the state.  An idle user still gets a
	// bucket written for the day that ended, so the rotation cannot rely on a
	// DNS request to trigger the save.
	assert.True(t, m.dirty.Load(), "the day adoption must mark the state dirty")

	// Nothing is left to rotate within the same day, so a second flush must be
	// a no-op instead of keeping the state dirty forever.
	m.dirty.Store(false)
	flushHistory(m, *now)
	assert.False(t, m.dirty.Load(), "a no-op flush must not mark the state dirty")
}

// TestUsageHistoryConcurrentAccess makes sure that the readers of the history
// do not race with the background flusher.  It is meant to be run with the
// race detector enabled.
func TestUsageHistoryConcurrentAccess(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	flushHistory(m, *now)

	ip := netip.MustParseAddr("10.0.0.1")
	wg := &sync.WaitGroup{}

	for range 4 {
		wg.Add(3)

		go func() {
			defer wg.Done()

			for range 200 {
				_ = m.List()
			}
		}()

		go func() {
			defer wg.Done()

			for range 200 {
				flushHistory(m, *now)
			}
		}()

		go func() {
			defer wg.Done()

			for range 200 {
				m.AllowQuery("alice", ip, "")
			}
		}()
	}

	wg.Wait()

	// The history must stay consistent regardless of the interleaving.
	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	for _, point := range historyOf(t, reopened, "alice") {
		assert.GreaterOrEqual(t, point.Requests, int64(0))
	}
}

func TestUsageHistoryIncludesTheCurrentDay(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	flushHistory(m, *now)

	// A user who has never made a request has no history to show.
	assert.Empty(t, historyOf(t, m, "alice"))

	// Once there is a request, the current day appears with its live count.
	// The rotation has not run for it yet, so it can only come from the
	// counter of the open bucket.
	queryUser(t, m, "alice", 7)

	history := historyOf(t, m, "alice")
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(7), history[0].Requests)

	queryUser(t, m, "alice", 3)

	history = historyOf(t, m, "alice")
	require.Len(t, history, 1)
	assert.Equal(t, int64(10), history[0].Requests, "the count must be live")

	// After the rotation the day becomes a stored one, and the new current
	// day is appended after it rather than replacing it.
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history = historyOf(t, m, "alice")
	require.Len(t, history, 2)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(10), history[0].Requests)
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(0), history[1].Requests)
}

func TestUsageHistoryBeforeTheFirstRotation(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")

	// No rotation has run yet, so no day bucket is open for the user.  The
	// requests are counted all the same, and the user must see them instead
	// of an empty chart.  This is the state of a user created between two
	// rotations.
	queryUser(t, m, "alice", 6)

	history := historyOf(t, m, "alice")
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(6), history[0].Requests)
}
