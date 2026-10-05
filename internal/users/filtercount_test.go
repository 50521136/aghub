package users

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordResult(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")

	m.RecordResult("alice", true, false)
	m.RecordResult("alice", true, false)
	m.RecordResult("alice", false, true)

	// A client that belongs to no user must be dropped, not turned into a
	// new one, and a query that no rule touched must not move a counter.
	m.RecordResult("nobody", true, false)
	m.RecordResult("alice", false, false)
	m.RecordResult("", true, true)

	list := m.List()
	require.Len(t, list, 1)
	assert.Equal(t, int64(2), list[0].Blocked)
	assert.Equal(t, int64(1), list[0].Passed)

	sum := m.Summary()
	assert.Equal(t, int64(2), sum.Blocked)
	assert.Equal(t, int64(1), sum.Passed)
}

func TestRecordResultSurvivesReload(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")

	m.RecordResult("alice", true, false)
	m.RecordResult("alice", false, true)

	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	list := reopened.List()
	require.Len(t, list, 1)
	assert.Equal(t, int64(1), list[0].Blocked)
	assert.Equal(t, int64(1), list[0].Passed)
}

// TestRankingMinRequests checks that the public board only lists accounts that
// have made more than the minimum number of lifetime requests.
func TestRankingMinRequests(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "small")
	addTestUser(t, m, "big")

	ip := netip.MustParseAddr("10.0.0.1")

	queryN(t, m, "small", ip, minRankRequests)
	queryN(t, m, "big", ip, minRankRequests+1)

	big := m.FindByLogin("big")
	require.NotNil(t, big)

	require.NoError(t, m.SetAvatar(big.UID, "🐱"))
	m.RecordResult("big", true, false)
	m.RecordResult("big", false, true)

	r := m.Ranking(0)
	require.Len(t, r, 1, "an account with exactly the minimum must be left out")
	assert.Equal(t, "big", r[0].Name)
	assert.Equal(t, int64(minRankRequests+1), r[0].TotalRequests)
	assert.Equal(t, int64(1), r[0].Blocked)
	assert.Equal(t, int64(1), r[0].Passed)
	assert.Equal(t, "🐱", r[0].Avatar)
}

// queryN makes n allowed requests on behalf of the given client identifier.
func queryN(t *testing.T, m *Manager, id string, ip netip.Addr, n int) {
	t.Helper()

	for range n {
		ok, reason := m.AllowQuery(id, ip, "")
		require.True(t, ok, "query rejected: %s", reason)
	}
}

func TestSetAvatar(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")

	u := m.FindByLogin("alice")
	require.NotNil(t, u)

	require.NoError(t, m.SetAvatar(u.UID, "🐱"))
	assert.Equal(t, "🐱", m.Get(u.UID).Avatar)

	// An unknown value is refused and leaves the stored one alone.
	require.Error(t, m.SetAvatar(u.UID, "cat"))
	assert.Equal(t, "🐱", m.Get(u.UID).Avatar)

	// A user that does not exist is an error as well.
	require.Error(t, m.SetAvatar("no-such-uid", "🐱"))

	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	assert.Equal(t, "🐱", reopened.Get(u.UID).Avatar)
}

func TestAvatarPresets(t *testing.T) {
	presets := AvatarPresets()
	assert.GreaterOrEqual(t, len(presets), 12)
	assert.LessOrEqual(t, len(presets), 20)

	for _, p := range presets {
		assert.True(t, IsValidAvatar(p), "preset %q must validate", p)
	}

	assert.False(t, IsValidAvatar("cat"))
	assert.False(t, IsValidAvatar(""))

	// The returned slice is a copy, so a caller cannot change the list for
	// everyone else.
	presets[0] = "changed"
	assert.NotEqual(t, "changed", AvatarPresets()[0])
}
