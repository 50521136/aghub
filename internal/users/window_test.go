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

// requestsToday returns the day figure of one account.
func requestsToday(t *testing.T, m *Manager, uid string) (n int64) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	us := m.usage[uid]
	require.NotNil(t, us)

	return us.requestsToday(m.now())
}

func TestRequestsTodayCountsTheHourInProgress(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	allowQueries(t, m, checkinIP.String(), 5)
	assert.Equal(t, int64(5), requestsToday(t, m, u.UID))

	// A rotation must not lose the hour that is still running.
	flushHours(t, m)
	assert.Equal(t, int64(5), requestsToday(t, m, u.UID))
}

func TestRequestsTodayKeepsOnlyTheDay(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000000, PeriodTotal)

	// The clock of the test manager is 12:00 UTC, which is 20:00 in Beijing.
	allowQueries(t, m, checkinIP.String(), 3)
	flushHours(t, m)

	// An hour later the hour that just ended is still part of the day.  The
	// rotation comes before the new requests, the way the background flusher
	// runs every half minute, so they land in the new bucket.
	*now = now.Add(time.Hour)
	flushHours(t, m)
	allowQueries(t, m, checkinIP.String(), 2)
	flushHours(t, m)
	assert.Equal(t, int64(5), requestsToday(t, m, u.UID))

	// The last minute of the day still holds everything that was counted in
	// it.
	*now = time.Date(2026, 10, 2, 15, 59, 0, 0, time.UTC)
	flushHours(t, m)
	assert.Equal(t, int64(5), requestsToday(t, m, u.UID))

	// Half a minute into the next Beijing day the same buckets are behind
	// midnight, and the figure starts from zero.  Nothing is lost on the way:
	// the buckets are still in the ring, they just belong to yesterday.
	*now = time.Date(2026, 10, 2, 16, 0, 30, 0, time.UTC)
	flushHours(t, m)
	assert.Zero(t, requestsToday(t, m, u.UID))

	allowQueries(t, m, checkinIP.String(), 2)
	assert.Equal(t, int64(2), requestsToday(t, m, u.UID))
}

// TestRequestsTodayStartsAtBeijingMidnight checks the boundary itself rather
// than the day around it: the board follows Beijing whatever the host is set
// to, and the test manager runs on UTC.
func TestRequestsTodayStartsAtBeijingMidnight(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000000, PeriodTotal)

	m.mu.Lock()
	us := m.usage[u.UID]

	// 23:00 in Beijing on the second of October, which is 15:00 UTC.
	us.recordHour(time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC).Unix(), 7)
	m.mu.Unlock()

	*now = time.Date(2026, 10, 2, 15, 30, 0, 0, time.UTC)
	assert.Equal(t, int64(7), requestsToday(t, m, u.UID))

	*now = time.Date(2026, 10, 2, 16, 30, 0, 0, time.UTC)
	assert.Zero(t, requestsToday(t, m, u.UID))

	// Midnight of the third of October in Beijing is 16:00 UTC, so a bucket
	// that starts there is the first one of the new day.
	m.mu.Lock()
	us.recordHour(time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC).Unix(), 9)
	m.mu.Unlock()

	assert.Equal(t, int64(9), requestsToday(t, m, u.UID))
}

func TestRequestsTodaySurvivesReload(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	allowQueries(t, m, checkinIP.String(), 4)
	flushHours(t, m)

	require.NoError(t, m.save())

	// A second manager reading the same state must see the same day, so that
	// a restart does not empty the board.
	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.path,
		Location: time.UTC,
	})
	require.NoError(t, err)

	reopened.now = func() (t time.Time) { return *now }

	assert.Equal(t, int64(4), requestsToday(t, reopened, u.UID))
}

func TestRankingTodayLeavesOutTheIdle(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000000, PeriodTotal)

	// Enough lifetime requests to qualify for the board, and today's too.
	allowQueries(t, m, checkinIP.String(), minRankRequests+1)
	flushHours(t, m)

	byTotal := m.Ranking(20, RankByTotal)
	require.Len(t, byTotal, 1)
	assert.Equal(t, int64(minRankRequests+1), byTotal[0].TotalRequests)
	assert.Equal(t, int64(minRankRequests+1), byTotal[0].RequestsToday)
	assert.Equal(t, 1, byTotal[0].Rank)

	byToday := m.Ranking(20, RankByToday)
	require.Len(t, byToday, 1)
	assert.Equal(t, byTotal[0].ID, byToday[0].ID)

	// Two days on the lifetime count is unchanged, but the day is over: the
	// board of today has nothing to show and must not pad itself with a row
	// of zeroes.
	*now = now.Add(48 * time.Hour)
	flushHours(t, m)

	assert.Len(t, m.Ranking(20, RankByTotal), 1)
	assert.Empty(t, m.Ranking(20, RankByToday))
	assert.Zero(t, requestsToday(t, m, u.UID))
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

	// The next day that whole day is behind midnight, so both accounts are at
	// zero for the day that is running.
	*now = now.Add(25 * time.Hour)
	flushHours(t, m)
	require.Zero(t, requestsToday(t, m, alice.UID))
	require.Zero(t, requestsToday(t, m, bob.UID))

	// Now bob is the busier one, although alice still leads on lifetime.
	allowQueries(t, m, bobIP.String(), 500)
	allowQueries(t, m, checkinIP.String(), 100)
	flushHours(t, m)

	byTotal := m.Ranking(20, RankByTotal)
	require.Len(t, byTotal, 2)
	assert.Equal(t, alice.Name, byTotal[0].Name, "the lifetime board leads with the older account")
	assert.Equal(t, int64(2100), byTotal[0].TotalRequests)

	byToday := m.Ranking(20, RankByToday)
	require.Len(t, byToday, 2)
	assert.Equal(t, bob.Name, byToday[0].Name, "the board of the day leads with the busier account")
	assert.Equal(t, int64(500), byToday[0].RequestsToday)
	assert.Equal(t, int64(100), byToday[1].RequestsToday)
	assert.Equal(t, 1, byToday[0].Rank)
	assert.Equal(t, 2, byToday[1].Rank)
}

func TestParseRankOrder(t *testing.T) {
	assert.Equal(t, RankByTotal, ParseRankOrder("total"))
	assert.Equal(t, RankByToday, ParseRankOrder("today"))
	assert.Equal(t, RankByToday, ParseRankOrder(""))
	assert.Equal(t, RankByToday, ParseRankOrder("nonsense"))

	// The tab was called "24h" before the window was aligned to the day, and
	// links carrying it are still around.
	assert.Equal(t, RankByToday, ParseRankOrder("24h"))
}
