package stats

import (
	"cmp"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/agh"
	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger is the common logger for tests.
var testLogger = slogutil.NewDiscardLogger()

// newTestStatsCtx returns StatsCtx initialised with given values.  All empty
// values from c will be replaced with defaults.
func newTestStatsCtx(tb testing.TB, c Config) (s *StatsCtx) {
	c.Logger = cmp.Or(c.Logger, testLogger)
	c.ConfigModifier = cmp.Or[agh.ConfigModifier](c.ConfigModifier, agh.EmptyConfigModifier{})
	c.HTTPReg = cmp.Or[aghhttp.Registrar](c.HTTPReg, aghhttp.EmptyRegistrar{})
	c.Filename = cmp.Or(c.Filename, filepath.Join(tb.TempDir(), "./stats.db"))
	c.Limit = cmp.Or(c.Limit, timeutil.Day)
	if c.ShouldCountClient == nil {
		c.ShouldCountClient = func([]string) bool { return true }
	}

	if c.UnitID == nil {
		c.UnitID = newUnitID
	}

	var err error
	s, err = New(c)
	require.NoError(tb, err)

	return s
}

func TestStats_races(t *testing.T) {
	var currentRound atomic.Uint32
	s := newTestStatsCtx(t, Config{
		UnitID:  currentRound.Load,
		Enabled: true,
	})

	s.Start()
	startTime := time.Now()
	testutil.CleanupAndRequireSuccess(t, s.Close)

	writeFunc := func(start, fin *sync.WaitGroup, waitCh <-chan unit, i int) {
		e := &Entry{
			Domain:         fmt.Sprintf("example-%d.org", i),
			Client:         fmt.Sprintf("client_%d", i),
			Result:         Result(i)%(resultLast-1) + 1,
			ProcessingTime: time.Since(startTime),
		}

		start.Done()
		defer fin.Done()

		<-waitCh

		s.Update(e)
	}
	readFunc := func(start, fin *sync.WaitGroup, waitCh <-chan unit) {
		start.Done()
		defer fin.Done()

		<-waitCh

		_, _ = s.getData(24)
	}

	const (
		roundsNum uint32 = 3

		writersNum = 10
		readersNum = 5
	)

	for round := range roundsNum {
		currentRound.Store(round)

		startWG, finWG := &sync.WaitGroup{}, &sync.WaitGroup{}
		waitCh := make(chan unit)

		for i := range writersNum {
			startWG.Add(1)
			finWG.Add(1)
			go writeFunc(startWG, finWG, waitCh, i)
		}

		for range readersNum {
			startWG.Add(1)
			finWG.Add(1)
			go readFunc(startWG, finWG, waitCh)
		}

		startWG.Wait()
		close(waitCh)
		finWG.Wait()
	}
}

func TestStatsCtx_FillCollectedStats_daily(t *testing.T) {
	const (
		daysCount = 10

		timeUnits = "days"
	)

	s := newTestStatsCtx(t, Config{
		Limit:   time.Hour,
		Enabled: true,
	})

	testutil.CleanupAndRequireSuccess(t, s.Close)

	sum := make([][]uint64, resultLast)
	sum[RFiltered] = make([]uint64, daysCount)
	sum[RSafeBrowsing] = make([]uint64, daysCount)
	sum[RParental] = make([]uint64, daysCount)

	total := make([]uint64, daysCount)

	dailyData := []*unitDB{}

	for i := range daysCount * 24 {
		n := uint64(i)
		nResult := make([]uint64, resultLast)
		nResult[RFiltered] = n
		nResult[RSafeBrowsing] = n
		nResult[RParental] = n

		day := i / 24
		sum[RFiltered][day] += n
		sum[RSafeBrowsing][day] += n
		sum[RParental][day] += n

		t := n * 3

		total[day] += t

		dailyData = append(dailyData, &unitDB{
			NTotal:  t,
			NResult: nResult,
		})
	}

	data := &StatsResp{}

	// In this way we will not skip first hours.
	curID := uint32(daysCount * 24)

	s.fillCollectedStats(data, dailyData, curID)

	assert.Equal(t, timeUnits, data.TimeUnits)
	assert.Equal(t, sum[RFiltered], data.BlockedFiltering)
	assert.Equal(t, sum[RSafeBrowsing], data.ReplacedSafebrowsing)
	assert.Equal(t, sum[RParental], data.ReplacedParental)
	assert.Equal(t, total, data.DNSQueries)
}

func TestStatsCtx_DataFromUnits_month(t *testing.T) {
	const hoursInMonth = 720

	s := newTestStatsCtx(t, Config{
		Limit:   time.Hour,
		Enabled: true,
	})

	testutil.CleanupAndRequireSuccess(t, s.Close)

	units, curID := s.loadUnits(hoursInMonth)
	require.Len(t, units, hoursInMonth)

	var h uint32
	for h = 1; h <= hoursInMonth; h++ {
		data := s.dataFromUnits(units[:h], curID)
		require.NotNil(t, data)
	}
}

// TestUnit_Add_AllowListed checks that the allow-list counter accumulates
// within one hourly unit and survives serialization.
func TestUnit_Add_AllowListed(t *testing.T) {
	u := newUnit(1)

	entries := []*Entry{{
		Domain:      "allowed.example",
		Client:      "client",
		Result:      RNotFiltered,
		AllowListed: true,
	}, {
		Domain:      "allowed.example",
		Client:      "client",
		Result:      RNotFiltered,
		AllowListed: true,
	}, {
		Domain: "blocked.example",
		Client: "client",
		Result: RFiltered,
	}}

	for _, e := range entries {
		u.add(e)
	}

	assert.Equal(t, uint64(2), u.nAllowed)

	udb := u.serialize()
	assert.Equal(t, uint64(2), udb.NAllowed)

	got := &unit{}
	got.deserialize(udb)
	assert.Equal(t, uint64(2), got.nAllowed)
}

// TestStatsCtx_GetStats24h_window checks that GetStats24h counts only the last
// 24 hourly units and leaves the older ones out.  The rollover is driven by
// moving the unit ID and flushing by hand instead of by Start, so no real time
// has to pass and no background flusher can race the test.
func TestStatsCtx_GetStats24h_window(t *testing.T) {
	const (
		startHour = 10_000
		hoursNum  = 30
		wantHours = 24
	)

	var curHour atomic.Uint32
	curHour.Store(startHour)

	s := newTestStatsCtx(t, Config{
		Limit:   timeutil.Day,
		Enabled: true,
		UnitID:  curHour.Load,
	})
	testutil.CleanupAndRequireSuccess(t, s.Close)

	for h := range hoursNum {
		curHour.Store(startHour + uint32(h))

		// Flush the previous hour into the database before counting the
		// new one, which is what the periodic flusher does in production.
		_, _ = s.flush()

		s.Update(&Entry{
			Domain:      fmt.Sprintf("domain.hour%d", h),
			Client:      "client",
			Result:      RNotFiltered,
			AllowListed: true,
		})
	}

	got := s.GetStats24h()
	assert.Equal(t, uint64(wantHours), got.Queries)
	assert.Equal(t, uint64(wantHours), got.Allowed)
	assert.Zero(t, got.Blocked)
}
