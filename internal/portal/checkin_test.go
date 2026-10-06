package portal

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CheckinStatus implements the [UserStore] interface.
func (s *testUserStore) CheckinStatus(uid string) (st *users.CheckinStatus) {
	if s.checkin != nil {
		return s.checkin
	}

	return &users.CheckinStatus{NextMilestone: 7, NextMilestoneBonus: 10000}
}

// Checkin implements the [UserStore] interface.
func (s *testUserStore) Checkin(uid string) (res *users.CheckinResult, err error) {
	s.checkinCalls++

	if s.checkinErr != nil {
		return nil, s.checkinErr
	}

	if s.checkinResult != nil {
		return s.checkinResult, nil
	}

	return &users.CheckinResult{Streak: 1, TempBonus: 3000}, nil
}

func TestCheckinRequiresSession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleCheckinAny, "/portal/api/checkin")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = post(t, m.handleCheckinAny, "/portal/api/checkin", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// A request without a session must not reach the store at all.
	assert.Zero(t, store.checkinCalls)
}

func TestCheckinStatusResponse(t *testing.T) {
	store := newTestStore()
	store.checkin = &users.CheckinStatus{
		CheckedInToday:     true,
		Streak:             3,
		TempBonus:          3000,
		NextMilestone:      7,
		NextMilestoneBonus: 10000,
	}

	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := get(t, m.handleCheckinAny, "/portal/api/checkin", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got users.CheckinStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, *store.checkin, got)
}

func TestCheckinResponse(t *testing.T) {
	store := newTestStore()
	store.checkinResult = &users.CheckinResult{
		Streak:         7,
		TempBonus:      3000,
		PermanentBonus: 10000,
		Milestone:      7,
	}

	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleCheckinAny, "/portal/api/checkin", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got checkinResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, checkinResponse{
		OK:             true,
		Streak:         7,
		TempBonus:      3000,
		PermanentBonus: 10000,
		Milestone:      7,
	}, got)

	assert.Equal(t, 1, store.checkinCalls)
}

func TestCheckinStoreError(t *testing.T) {
	store := newTestStore()
	store.checkinErr = errors.New("boom")

	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleCheckinAny, "/portal/api/checkin", "", cookie)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestMeIncludesCheckin checks that the account endpoint carries the check-in
// state as a sibling of the user object, which is what the portal reads.
func TestMeIncludesCheckin(t *testing.T) {
	store := newTestStore()
	store.checkin = &users.CheckinStatus{
		Streak:             2,
		NextMilestone:      7,
		NextMilestoneBonus: 10000,
	}

	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := get(t, m.handleMe, "/portal/api/me", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got meResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Checkin)
	assert.Equal(t, *store.checkin, *got.Checkin)
}
