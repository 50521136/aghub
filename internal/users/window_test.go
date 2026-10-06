package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flushHours rotates the hourly buckets the way the background flusher does,
// under the same lock, so that the test exercises the real rotation.
func flushHours(t *testing.T, m *Manager) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushHoursLocked(m.now())
}

// requests24h returns the rolling figure of one account.
func requests24h(t *testing.T, m *Manager, uid string) (n int64) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	us := m.usage[uid]
	require.NotNil(t, us)

	return us.requests24h(m.now())
}

func TestRequests24hCountsTheHourInProgress(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	allowQueries(t, m, checkinIP.String(), 5)
	assert.Equal(t, int64(5), requests24h(t, m, u.UID))

	// A rotation must not lose the hour that is still running.
	flushHours(t, m)
	assert.Equal(t, int64(5), requests24h(t, m, u.UID))
}

func TestRequests24hKeepsOnlyTheWindow(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000000, PeriodTotal)

	allowQueries(t, m, checkinIP.String(), 3)
	flushHours(t, m)

	// One hour later the hour that just ended is still inside the window.
	// The rotation comes before the new requests, the way the background
	// flusher runs every half minute, so they land in the new bucket.
	*now = now.Add(time.Hour)
	flushHours(t, m)
	allowQueries(t, m, checkinIP.String(), 2)
	flushHours(t, m)
	assert.Equal(t, int64(5), requests24h(t, m, u.UID))

	// Twenty-three hours on, the oldest bucket has just left it.
	*now = now.Add(23 * time.Hour)
	flushHours(t, m)
	assert.Equal(t, int64(2), requests24h(t, m, u.UID))

	// A day after that there is nothing left to count.
	*now = now.Add(24 * time.Hour)
	flushHours(t, m)
	assert.Zero(t, requests24h(t, m, u.UID))
}

func TestRequests24hSurvivesReload(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	allowQueries(t, m, checkinIP.String(), 4)
	flushHours(t, m)

	require.NoError(t, m.save())

	// A second manager reading the same state must see the same window, so
	// that a restart does not empty the recent board.
	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.path,
		Location: time.UTC,
	})
	require.NoError(t, err)

	reopened.now = func() (t time.Time) { return *now }

	assert.Equal(t, int64(4), requests24h(t, reopened, u.UID))
}

func TestRankingBy24hLeavesOutTheIdle(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000000, PeriodTotal)

	// Enough lifetime requests to qualify for the board, and recent ones too.
	allowQueries(t, m, checkinIP.String(), minRankRequests+1)
	flushHours(t, m)

	byTotal := m.Ranking(20, RankByTotal)
	require.Len(t, byTotal, 1)
	assert.Equal(t, int64(minRankRequests+1), byTotal[0].TotalRequests)
	assert.Equal(t, int64(minRankRequests+1), byTotal[0].Requests24h)
	assert.Equal(t, 1, byTotal[0].Rank)

	by24h := m.Ranking(20, RankBy24h)
	require.Len(t, by24h, 1)
	assert.Equal(t, byTotal[0].ID, by24h[0].ID)

	// Two days on the lifetime count is unchanged, but the window is empty:
	// the recent board has nothing to show and must not pad itself with a row
	// of zeroes.
	*now = now.Add(48 * time.Hour)
	flushHours(t, m)

	assert.Len(t, m.Ranking(20, RankByTotal), 1)
	assert.Empty(t, m.Ranking(20, RankBy24h))
	assert.Zero(t, requests24h(t, m, u.UID))
}

func TestRankingSortsByTheChosenFigure(t *testing.T) {
	m, now := newTestManager(t)
	alice := newCheckinUser(t, m, 1000000, PeriodTotal)

	bobIP := checkinIP.Next()
	bob := addUser(t, m, &AddParams{
		Name:         "bob",
		IDs:          []string{bobIP.String()},
		RequestLimit: iptr(int64(1000000)),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	// Both accounts build a lifetime count that puts them on the board.
	allowQueries(t, m, checkinIP.String(), 2000)
	allowQueries(t, m, bobIP.String(), 1100)
	flushHours(t, m)

	// A day later that whole day has left the 24-hour window, so both
	// accounts are at zero inside it.
	*now = now.Add(25 * time.Hour)
	flushHours(t, m)
	require.Zero(t, requests24h(t, m, alice.UID))
	require.Zero(t, requests24h(t, m, bob.UID))

	// Now bob is the busier one, although alice still leads on lifetime.
	allowQueries(t, m, bobIP.String(), 500)
	allowQueries(t, m, checkinIP.String(), 100)
	flushHours(t, m)

	byTotal := m.Ranking(20, RankByTotal)
	require.Len(t, byTotal, 2)
	assert.Equal(t, alice.Name, byTotal[0].Name, "the lifetime board leads with the older account")
	assert.Equal(t, int64(2100), byTotal[0].TotalRequests)

	by24h := m.Ranking(20, RankBy24h)
	require.Len(t, by24h, 2)
	assert.Equal(t, bob.Name, by24h[0].Name, "the recent board leads with the busier hour")
	assert.Equal(t, int64(500), by24h[0].Requests24h)
	assert.Equal(t, int64(100), by24h[1].Requests24h)
	assert.Equal(t, 1, by24h[0].Rank)
	assert.Equal(t, 2, by24h[1].Rank)
}

func TestParseRankOrder(t *testing.T) {
	assert.Equal(t, RankByTotal, ParseRankOrder("total"))
	assert.Equal(t, RankBy24h, ParseRankOrder("24h"))
	assert.Equal(t, RankBy24h, ParseRankOrder(""))
	assert.Equal(t, RankBy24h, ParseRankOrder("nonsense"))
}
