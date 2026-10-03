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
		ok, reason := m.AllowQuery(id, ip)
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
	// counted before it happened within the same flush interval.
	flushHistory(m, *now)
	assert.Empty(t, historyOf(t, m, "alice"))

	// The rollover to the next day closes the bucket of the first one.
	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history := historyOf(t, m, "alice")
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(3), history[0].Requests)

	// The next day is counted on its own.
	queryUser(t, m, "alice", 2)

	*now = now.AddDate(0, 0, 1)
	flushHistory(m, *now)

	history = historyOf(t, m, "alice")
	require.Len(t, history, 2)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(3), history[0].Requests)
	assert.Equal(t, "2026-10-03", history[1].Date)
	assert.Equal(t, int64(2), history[1].Requests)
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
	// oldest first.
	for i := 1; i < len(history); i++ {
		assert.Less(t, history[i-1].Date, history[i].Date)
	}

	assert.Equal(t, "2026-10-07", history[0].Date)
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
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(7), history[0].Requests)
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
	require.Len(t, history, 3)

	// The idle day is kept as a zero rather than omitted, so that the report
	// shows a continuous timeline.
	assert.Equal(t, []UsagePoint{
		{Date: "2026-10-02", Requests: 4},
		{Date: "2026-10-03", Requests: 0},
		{Date: "2026-10-04", Requests: 1},
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
	require.Len(t, history, 1)
	assert.Equal(t, "2026-10-02", history[0].Date)
	assert.Equal(t, int64(5), history[0].Requests)
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
				m.AllowQuery("alice", ip)
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
