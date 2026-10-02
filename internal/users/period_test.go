package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextPeriodStart(t *testing.T) {
	loc := time.UTC

	testCases := []struct {
		name string
		in   time.Time
		want time.Time
	}{{
		name: "day_midday",
		in:   time.Date(2026, 10, 2, 15, 30, 0, 0, loc),
		want: time.Date(2026, 10, 3, 0, 0, 0, 0, loc),
	}, {
		name: "day_just_after_midnight",
		in:   time.Date(2026, 10, 2, 0, 0, 1, 0, loc),
		want: time.Date(2026, 10, 3, 0, 0, 0, 0, loc),
	}, {
		name: "day_just_before_midnight",
		in:   time.Date(2026, 10, 2, 23, 59, 59, 0, loc),
		want: time.Date(2026, 10, 3, 0, 0, 0, 0, loc),
	}, {
		name: "day_across_month_end",
		in:   time.Date(2026, 10, 31, 12, 0, 0, 0, loc),
		want: time.Date(2026, 11, 1, 0, 0, 0, 0, loc),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want.Unix(), nextPeriodStart(tc.in, loc, PeriodDay))
		})
	}

	t.Run("month_uses_the_month_boundary", func(t *testing.T) {
		got := nextPeriodStart(time.Date(2026, 10, 15, 12, 0, 0, 0, loc), loc, PeriodMonth)
		assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, loc).Unix(), got)
	})

	t.Run("total_never_resets", func(t *testing.T) {
		got := nextPeriodStart(time.Date(2026, 10, 15, 12, 0, 0, 0, loc), loc, PeriodTotal)
		assert.Equal(t, int64(0), got)
	})

	t.Run("boundaries_are_midnight_aligned", func(t *testing.T) {
		// The reset must not depend on when the user was created: two users on
		// the same day share the same next reset.
		early := time.Date(2026, 10, 2, 0, 5, 0, 0, loc)
		late := time.Date(2026, 10, 2, 23, 55, 0, 0, loc)

		assert.Equal(
			t,
			nextPeriodStart(early, loc, PeriodDay),
			nextPeriodStart(late, loc, PeriodDay),
		)
	})

	t.Run("time_zone_shifts_the_boundary", func(t *testing.T) {
		shanghai := time.FixedZone("CST", 8*3600)
		in := time.Date(2026, 10, 2, 22, 0, 0, 0, shanghai)

		got := nextPeriodStart(in, shanghai, PeriodDay)
		assert.Equal(t, time.Date(2026, 10, 3, 0, 0, 0, 0, shanghai).Unix(), got)
	})
}
