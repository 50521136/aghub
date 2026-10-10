package users

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这个文件锁住一个曾经真实存在的缺陷：今日榜上的「拦截量」和「防误杀」
// 显示的是**累计值**。
//
// Ranking() 里 RequestsToday 和 AvgLatencyMS 都按榜单口径切换，但
// Blocked/Passed 无条件取 i.Blocked / i.Passed。于是今日榜的一张卡上会同时
// 出现「今日 14983 次解析」和「拦截 9478 + 防误杀 17533」—— 两个过滤数字
// 加起来比今日总量还大。线上实测就是这样的数字。
//
// 修法与 avg_latency_ms 一致：过滤计数也按小时环求和，跟着榜单窗口走。

func TestRankingShowsTheFilteringOfItsWindow(t *testing.T) {
	m, now := newTestManager(t)
	newCheckinUser(t, m, 100000, PeriodTotal)

	// 先造出「过去」的过滤量：两次拦截、一次防误杀。
	// 账号要过 minRankRequests 才有资格上榜。
	allowQueries(t, m, checkinIP.String(), minRankRequests+10)
	m.RecordResult(checkinIP.String(), true, false)
	m.RecordResult(checkinIP.String(), true, false)
	m.RecordResult(checkinIP.String(), false, true)
	flushHours(t, m)

	// 跨过北京时间午夜，进入新的一天（与延迟测试用的是同一个时刻）。
	*now = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	flushHours(t, m)

	// 新的一天必须有自己的解析量，否则今日榜会把这个账号整个剔除
	// —— 今日榜不列今天没有请求的账号。
	allowQueries(t, m, checkinIP.String(), 10)
	m.RecordResult(checkinIP.String(), false, true)
	flushHours(t, m)

	entries := m.Ranking(10, RankByToday)
	require.Len(t, entries, 1)

	// 今日榜：只该算今天那一次防误杀。昨天的两次拦截不能出现，
	// 否则就是「一天的量配上一辈子的过滤」。
	assert.Equal(t, int64(0), entries[0].Blocked,
		"今日榜的拦截量不该是累计值")
	assert.Equal(t, int64(1), entries[0].Passed,
		"今日榜的防误杀应该只算今天的")

	// 累计榜：两次拦截 + 两次防误杀，全部计入。
	entries = m.Ranking(10, RankByTotal)
	require.Len(t, entries, 1)
	assert.Equal(t, int64(2), entries[0].Blocked,
		"累计榜的拦截量应该含全部历史")
	assert.Equal(t, int64(2), entries[0].Passed,
		"累计榜的防误杀应该含全部历史")
}

// Info 同时给出两个窗口的过滤量，供门户的「我的」页直接使用 ——
// 那里和榜单一样，一天的数字旁边不能摆一辈子的数字。
func TestInfoReportsBothFilteringWindows(t *testing.T) {
	m, now := newTestManager(t)
	u := newCheckinUser(t, m, 100000, PeriodTotal)

	allowQueries(t, m, checkinIP.String(), 5)
	m.RecordResult(checkinIP.String(), true, false)
	m.RecordResult(checkinIP.String(), false, true)
	flushHours(t, m)

	// 新的一天，再造一次拦截。
	*now = time.Date(2026, 10, 2, 16, 1, 0, 0, time.UTC)
	flushHours(t, m)
	allowQueries(t, m, checkinIP.String(), 5)
	m.RecordResult(checkinIP.String(), true, false)
	flushHours(t, m)

	info := infoOf(t, m, u.UID)
	require.NotNil(t, info)

	// 累计口径：两次拦截、一次防误杀。
	assert.Equal(t, int64(2), info.Blocked)
	assert.Equal(t, int64(1), info.Passed)

	// 今日口径：只有今天那一次拦截，昨天的防误杀不计入。
	assert.Equal(t, int64(1), info.BlockedToday)
	assert.Equal(t, int64(0), info.PassedToday)
}

// 开着的那个小时桶必须落盘，否则重启会丢掉当天已经统计的部分。
func TestFilteringOfTheOpenHourSurvivesAReload(t *testing.T) {
	m, _ := newTestManager(t)
	u := newCheckinUser(t, m, 100000, PeriodTotal)

	allowQueries(t, m, checkinIP.String(), 5)
	m.RecordResult(checkinIP.String(), true, false)
	m.RecordResult(checkinIP.String(), false, true)
	flushHours(t, m)

	// 直接落盘再读回来，模拟进程重启。
	require.NoError(t, m.save())
	require.NoError(t, m.load())

	info := infoOf(t, m, u.UID)
	require.NotNil(t, info)
	assert.Equal(t, int64(1), info.BlockedToday,
		"重启不该丢掉当天已统计的拦截量")
	assert.Equal(t, int64(1), info.PassedToday,
		"重启不该丢掉当天已统计的防误杀")
}

// infoOf returns the API representation of one account, going through the same
// list the administrator API uses rather than reaching into the manager.
func infoOf(t *testing.T, m *Manager, uid string) (found *Info) {
	t.Helper()

	for _, i := range m.List() {
		if i.UID == uid {
			return i
		}
	}

	return nil
}
