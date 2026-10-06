package users

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkinIP is the identifier of the accounts the check-in tests use.
var checkinIP = netip.MustParseAddr("192.0.2.1")

// newCheckinUser adds a single enabled account and returns it.
func newCheckinUser(t *testing.T, m *Manager, limit int64, period Period) (u *User) {
	t.Helper()

	return addUser(t, m, &AddParams{
		Name:         "alice",
		IDs:          []string{checkinIP.String()},
		RequestLimit: iptr(limit),
		Period:       period,
		Enabled:      true,
	})
}

// advanceDay moves the controllable clock forward and refreshes the cached
// period boundaries, so that the next query sees the new day.  It never sleeps:
// the clock is injected by newTestManager.
func advanceDay(t *testing.T, m *Manager, now *time.Time, days int) {
	t.Helper()

	*now = now.AddDate(0, 0, days)
	m.refreshPeriods()
}

// allowQueries makes n allowed queries and fails the test on the first refusal.
func allowQueries(t *testing.T, m *Manager, clientID string, n int) {
	t.Helper()

	for i := range n {
		ok, reason := m.AllowQuery(clientID, checkinIP, "")
		require.True(t, ok, "query %d refused: %s", i, reason)
	}
}

func TestCheckinFirstTime(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	res, err := m.Checkin(u.UID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Streak)
	assert.Equal(t, checkinTempBonus, res.TempBonus)
	assert.Zero(t, res.PermanentBonus)
	assert.Zero(t, res.Milestone)

	st := m.CheckinStatus(u.UID)
	require.NotNil(t, st)
	assert.True(t, st.CheckedInToday)
	assert.Equal(t, int64(1), st.Streak)
	assert.Equal(t, checkinTempBonus, st.TempBonus)
	assert.Equal(t, int64(7), st.NextMilestone)
	assert.Equal(t, int64(10000), st.NextMilestoneBonus)

	// A plain check-in never changes the permanent quota.
	assert.Equal(t, int64(1000), m.Get(u.UID).RequestLimit)
}

func TestCheckinSameDayIsIdempotent(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	res, err := m.Checkin(u.UID)
	require.NoError(t, err)

	// The repeat reports the current state, so the allowance that is active
	// for today is still reported, but nothing new is granted.
	assert.Equal(t, int64(1), res.Streak)
	assert.Equal(t, checkinTempBonus, res.TempBonus)
	assert.Zero(t, res.PermanentBonus)
	assert.Zero(t, res.Milestone)

	st := m.CheckinStatus(u.UID)
	require.NotNil(t, st)
	assert.Equal(t, int64(1), st.Streak)
	assert.Equal(t, checkinTempBonus, st.TempBonus, "the allowance must not stack")
	assert.Equal(t, int64(1000), m.Get(u.UID).RequestLimit)
}

func TestCheckinStreakAccumulates(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	for day := int64(1); day <= 3; day++ {
		if day > 1 {
			advanceDay(t, m, now, 1)
		}

		res, err := m.Checkin(u.UID)
		require.NoError(t, err)
		assert.Equal(t, day, res.Streak, "day %d", day)
	}
}

func TestCheckinBreakResetsStreak(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	advanceDay(t, m, now, 1)

	res, err := m.Checkin(u.UID)
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Streak)

	// Skip a whole day: the streak is broken.
	advanceDay(t, m, now, 2)

	res, err = m.Checkin(u.UID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Streak, "a missed day starts a new streak")
}

func TestCheckinTempBonusCountsTowardQuota(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	// The base quota is 1000; the extra queries only pass because the
	// temporary allowance is added to the limit.
	allowQueries(t, m, checkinIP.String(), 1000+int(checkinTempBonus))
}

func TestCheckinTempBonusExpiresNextDay(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodTotal)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	// The allowance carries the account past its base quota.
	allowQueries(t, m, checkinIP.String(), 1000+int(checkinTempBonus))

	advanceDay(t, m, now, 1)

	ok, reason := m.AllowQuery(checkinIP.String(), checkinIP, "")
	assert.False(t, ok, "the allowance must not survive the day it was granted for")
	assert.Equal(t, ReasonQuota, reason)

	// The account definition itself was never touched by the temporary
	// allowance.
	assert.Equal(t, int64(1000), m.Get(u.UID).RequestLimit)
}

func TestCheckinTempBonusCountedInRemaining(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	info := m.InfoOf(u.UID)
	require.NotNil(t, info)
	assert.Equal(t, 1000+checkinTempBonus, info.RemainingRequests)
	assert.Equal(t, StatusActive, info.Status)

	allowQueries(t, m, checkinIP.String(), 500)

	info = m.InfoOf(u.UID)
	require.NotNil(t, info)
	assert.Equal(t, 1000+checkinTempBonus-500, info.RemainingRequests)
}

func TestCheckinMilestoneGrantsPermanentBonus(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	for day := int64(1); day <= 7; day++ {
		if day > 1 {
			advanceDay(t, m, now, 1)
		}

		res, err := m.Checkin(u.UID)
		require.NoError(t, err)

		if day == 7 {
			assert.Equal(t, int64(10000), res.PermanentBonus)
			assert.Equal(t, int64(7), res.Milestone)
		} else {
			assert.Zero(t, res.PermanentBonus, "day %d must not grant a milestone", day)
		}
	}

	assert.Equal(t, int64(11000), m.Get(u.UID).RequestLimit)

	// The increase is permanent: breaking the streak does not take it back.
	advanceDay(t, m, now, 3)

	res, err := m.Checkin(u.UID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Streak)
	assert.Equal(t, int64(11000), m.Get(u.UID).RequestLimit)
}

func TestCheckinNextMilestoneProgresses(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	st := m.CheckinStatus(u.UID)
	require.NotNil(t, st)
	assert.Equal(t, int64(7), st.NextMilestone)
	assert.Equal(t, int64(10000), st.NextMilestoneBonus)

	for day := int64(1); day <= 7; day++ {
		if day > 1 {
			advanceDay(t, m, now, 1)
		}

		_, err := m.Checkin(u.UID)
		require.NoError(t, err)
	}

	st = m.CheckinStatus(u.UID)
	require.NotNil(t, st)
	assert.Equal(t, int64(30), st.NextMilestone)
	assert.Equal(t, int64(50000), st.NextMilestoneBonus)
}

// TestCheckinUnlimitedStaysUnlimited is the guard for the case that is easy to
// get wrong: an account without a finite quota must be able to check in and
// earn a streak, but no bonus may turn it into a quota account.  Both the
// sentinel [Unlimited] and a zero limit are covered, because the portal
// presents both as unlimited.
func TestCheckinUnlimitedStaysUnlimited(t *testing.T) {
	for _, limit := range []int64{Unlimited, 0} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			m, now := newTestManager(t)
			u := newCheckinUser(t, m, limit, PeriodDay)

			res, err := m.Checkin(u.UID)
			require.NoError(t, err)
			assert.Equal(t, int64(1), res.Streak)
			assert.Zero(t, res.TempBonus)
			assert.Zero(t, res.PermanentBonus)
			assert.Equal(t, limit, m.Get(u.UID).RequestLimit)

			// Even reaching a milestone must leave the account alone.
			for day := 2; day <= 7; day++ {
				advanceDay(t, m, now, 1)

				res, err = m.Checkin(u.UID)
				require.NoError(t, err)
			}

			assert.Equal(t, int64(7), res.Streak)
			assert.Zero(t, res.PermanentBonus)
			assert.Equal(t, limit, m.Get(u.UID).RequestLimit)

			st := m.CheckinStatus(u.UID)
			require.NotNil(t, st)
			assert.True(t, st.CheckedInToday)
			assert.Equal(t, int64(7), st.Streak)
			assert.Zero(t, st.TempBonus)
		})
	}
}

func TestCheckinStatusUnknownUser(t *testing.T) {
	m, _ := newTestManager(t)

	assert.Nil(t, m.CheckinStatus("nobody"))

	_, err := m.Checkin("nobody")
	assert.Error(t, err)
}

// TestCheckinSurvivesReload checks that the streak and the active temporary
// allowance are persisted, so that a restart does not silently hand the user a
// fresh streak or drop the allowance they already earned.
func TestCheckinSurvivesReload(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)
	require.NoError(t, m.save())

	clock := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	m2, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.Path(),
		Location: time.UTC,
	})
	require.NoError(t, err)

	m2.now = func() (now time.Time) { return clock }
	require.NoError(t, m2.refreshPeriods())

	st := m2.CheckinStatus(u.UID)
	require.NotNil(t, st)
	assert.True(t, st.CheckedInToday)
	assert.Equal(t, int64(1), st.Streak)
	assert.Equal(t, checkinTempBonus, st.TempBonus)
}
