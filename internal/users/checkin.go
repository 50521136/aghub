package users

import (
	"fmt"
	"slices"
	"time"
)

// checkinTempBonus is the temporary request allowance a single daily check-in
// grants.  It is deliberately a few thousand requests: roughly a busy day of
// browsing for one device, which is enough to be felt, but small next to a
// subscription quota, so it rewards showing up without replacing the need to
// subscribe.  The allowance is valid for the local day of the check-in only.
const checkinTempBonus int64 = 3000

// checkinMilestone is a streak length that grants a permanent request-quota
// increase.
type checkinMilestone struct {
	// Days is the number of consecutive check-in days required.
	Days int64

	// Bonus is the permanent number of requests added to the account quota.
	Bonus int64
}

// checkinMilestones are the streak milestones, ascending by days.  The
// permanent increases are an order of magnitude above the daily temporary
// allowance, so that a streak is worth much more than a single check-in, and
// they escalate so that the longer streaks are the ones that pay off.
var checkinMilestones = []checkinMilestone{
	{Days: 7, Bonus: 10000},
	{Days: 30, Bonus: 50000},
	{Days: 100, Bonus: 200000},
}

// CheckinStatus is the state of the daily check-in of an account.  It is what
// GET /portal/api/checkin reports and what the account endpoint embeds.
type CheckinStatus struct {
	// CheckedInToday is true when the account has already checked in on the
	// current local day.
	CheckedInToday bool `json:"checked_in_today"`

	// Streak is the number of consecutive days the account has checked in.
	Streak int64 `json:"streak"`

	// TempBonus is the temporary allowance that is active for the current
	// local day, or zero when there is none.  It is not the amount that a
	// future check-in would grant.
	TempBonus int64 `json:"temp_bonus"`

	// NextMilestone is the streak length of the next milestone, or zero when
	// every milestone has been reached.
	NextMilestone int64 `json:"next_milestone"`

	// NextMilestoneBonus is the permanent increase granted by
	// [CheckinStatus.NextMilestone].
	NextMilestoneBonus int64 `json:"next_milestone_bonus"`
}

// CheckinResult is what a successful check-in granted.
type CheckinResult struct {
	// Streak is the number of consecutive check-in days after the call.
	Streak int64

	// TempBonus is the temporary allowance the account holds for the current
	// local day after the call, or zero when the account has no finite quota.
	// On a repeat it is the allowance granted earlier the same day rather than
	// a second one.
	TempBonus int64

	// PermanentBonus is the permanent quota increase granted by the call, or
	// zero when no milestone was reached.
	PermanentBonus int64

	// Milestone is the streak length that granted
	// [CheckinResult.PermanentBonus], or zero when none was reached.
	Milestone int64
}

// tempBonusOn returns the temporary check-in allowance active for the local day
// that starts at dayStart, or zero when the allowance has expired.  Comparing
// the stored day with the current one is what makes the allowance vanish at
// the day boundary without any cleanup: a stale day never matches.
func (e *usage) tempBonusOn(dayStart int64) (bonus int64) {
	if dayStart != 0 && e.tempBonusDay.Load() == dayStart {
		return e.tempBonus.Load()
	}

	return 0
}

// hasFiniteQuota reports whether the account has a positive request quota, and
// therefore something that a check-in bonus can be added to.
//
// Both [Unlimited] and a zero limit are treated as no quota.  The portal
// presents both as unlimited -- the bundled front-end renders "不限量" for any
// limit at or below zero -- and a bonus must never turn such an account into a
// limited one, so nothing is granted to it.
func hasFiniteQuota(limit int64) (ok bool) {
	return limit > 0
}

// nextMilestone returns the first milestone above streak and its bonus.  Both
// are zero when the last milestone has been passed.
func nextMilestone(streak int64) (days, bonus int64) {
	for _, ms := range checkinMilestones {
		if ms.Days > streak {
			return ms.Days, ms.Bonus
		}
	}

	return 0, 0
}

// milestoneFor returns the milestone that is exactly at streak and its bonus,
// or zeroes when streak is not a milestone.
func milestoneFor(streak int64) (days, bonus int64) {
	for _, ms := range checkinMilestones {
		if ms.Days == streak {
			return ms.Days, ms.Bonus
		}
	}

	return 0, 0
}

// CheckinStatus returns the daily check-in state of the account, or nil when
// there is no such account.
func (m *Manager) CheckinStatus(uid string) (st *CheckinStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.defs[uid]; !ok {
		return nil
	}

	today := periodStart(m.now(), m.loc, PeriodDay)

	st = &CheckinStatus{}
	if us := m.usage[uid]; us != nil {
		st.Streak = us.streak.Load()
		st.CheckedInToday = us.checkinDay.Load() == today
		st.TempBonus = us.tempBonusOn(today)
	}

	st.NextMilestone, st.NextMilestoneBonus = nextMilestone(st.Streak)

	return st
}

// Checkin performs the daily check-in of the account and returns what it
// granted.
//
// Checking in twice on the same local day is a no-op that reports the current
// state, so the call is safe to retry: a lost response costs the user nothing
// and cannot be used to farm allowances.  A missed day restarts the streak at
// one, but a permanent increase that was already earned is never taken back,
// because it lives on the account itself.
func (m *Manager) Checkin(uid string) (res *CheckinResult, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	def, ok := m.defs[uid]
	if !ok {
		return nil, fmt.Errorf("users: no user with uid %q", uid)
	}

	us := m.usage[uid]
	if us == nil {
		us = &usage{}
		m.usage[uid] = us
	}

	today := periodStart(m.now(), m.loc, PeriodDay)

	res = &CheckinResult{}

	if us.checkinDay.Load() == today {
		// Already checked in: report the current state and grant nothing, so
		// that a retry cannot be mistaken for a second allowance.  The
		// allowance that is active for today is still part of that state.
		res.Streak = us.streak.Load()
		res.TempBonus = us.tempBonusOn(today)

		return res, nil
	}

	streak := us.streak.Load()

	// A streak only continues when the previous check-in was on the day
	// immediately before today.  Anything else -- the first check-in ever, or
	// a day that was missed -- starts a new streak at one.
	yesterday := time.Unix(today, 0).In(m.loc).AddDate(0, 0, -1).Unix()
	if us.checkinDay.Load() == yesterday {
		streak++
	} else {
		streak = 1
	}

	us.checkinDay.Store(today)
	us.streak.Store(streak)
	res.Streak = streak

	// The temporary allowance is granted for today only.  An account without a
	// finite quota is left untouched: there is nothing to add to, and the
	// allowance must not turn it into a limited account.
	if hasFiniteQuota(def.RequestLimit) {
		us.tempBonus.Store(checkinTempBonus)
		us.tempBonusDay.Store(today)
		res.TempBonus = checkinTempBonus
	}

	// A milestone grants a permanent increase, written into the account
	// itself so that it outlives the streak that earned it.  The streak grows
	// by one per day, so at most one milestone is reached per call.
	if hasFiniteQuota(def.RequestLimit) {
		if days, bonus := milestoneFor(streak); bonus > 0 {
			next := *def
			next.IDs = slices.Clone(def.IDs)
			next.RequestLimit = def.RequestLimit + bonus
			m.defs[uid] = &next
			m.publishLocked()

			res.PermanentBonus = bonus
			res.Milestone = days
		}
	}

	m.dirty.Store(true)

	return res, nil
}
