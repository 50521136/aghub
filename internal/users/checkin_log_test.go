package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckinGrantsTenThousandRequests(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	res, err := m.Checkin(u.UID)
	require.NoError(t, err)
	assert.Equal(t, int64(10000), res.TempBonus)

	// The allowance is part of the quota of the day, so the account can spend
	// it: its own thousand plus the ten thousand the check-in added.
	allowQueries(t, m, checkinIP.String(), 11000)

	ok, reason := m.AllowQuery(checkinIP.String(), checkinIP, "")
	assert.False(t, ok)
	assert.Equal(t, ReasonQuota, reason)
}

func TestCheckinUnlocksTheLogOnTheThirdDay(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	assert.False(t, m.LogUnlocked(u.UID))
	assert.Equal(t, LogUnlockStreak, m.CheckinStatus(u.UID).LogUnlockStreak)

	for day := int64(1); day <= LogUnlockStreak; day++ {
		if day > 1 {
			advanceDay(t, m, now, 1)
		}

		res, err := m.Checkin(u.UID)
		require.NoError(t, err)
		require.Equal(t, day, res.Streak)

		st := m.CheckinStatus(u.UID)
		assert.Equal(t, day >= LogUnlockStreak, st.LogUnlocked, "day %d", day)
		assert.Equal(t, day >= LogUnlockStreak, m.LogUnlocked(u.UID), "day %d", day)
	}

	// The unlock outlives the streak that earned it: a missed day restarts
	// the count but does not take the log away again.
	advanceDay(t, m, now, 5)

	_, err := m.Checkin(u.UID)
	require.NoError(t, err)

	st := m.CheckinStatus(u.UID)
	require.Equal(t, int64(1), st.Streak)
	assert.True(t, st.LogUnlocked)
	assert.True(t, m.LogUnlocked(u.UID))
}

func TestLogUnlockedIsFalseForUnknownUser(t *testing.T) {
	m, _ := newTestManager(t)

	assert.False(t, m.LogUnlocked("no-such-uid"))
}

func TestLogUnlockSurvivesReload(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 1000, PeriodDay)

	for day := int64(1); day <= LogUnlockStreak; day++ {
		if day > 1 {
			advanceDay(t, m, now, 1)
		}

		_, err := m.Checkin(u.UID)
		require.NoError(t, err)
	}

	require.True(t, m.LogUnlocked(u.UID))
	require.NoError(t, m.save())

	reopened, err := New(&Config{
		Logger:   testLogger(),
		Path:     m.path,
		Location: time.UTC,
	})
	require.NoError(t, err)

	assert.True(t, reopened.LogUnlocked(u.UID))
	assert.Equal(t, LogUnlockStreak, reopened.CheckinStatus(u.UID).Streak)
}
