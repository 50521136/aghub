package dnsforward

import (
	"net/netip"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghnet"
	"github.com/AdguardTeam/AdGuardHome/internal/filtering"
	"github.com/AdguardTeam/AdGuardHome/internal/querylog"
	"github.com/AdguardTeam/AdGuardHome/internal/stats"
	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testQueryLog is a simple [querylog.QueryLog] implementation for tests.
type testQueryLog struct {
	// QueryLog is embedded here simply to make testQueryLog
	// a [querylog.QueryLog] without actually implementing all methods.
	querylog.QueryLog

	lastParams *querylog.AddParams
}

// Add implements the [querylog.QueryLog] interface for *testQueryLog.
func (l *testQueryLog) Add(p *querylog.AddParams) {
	l.lastParams = p
}

// ShouldLog implements the [querylog.QueryLog] interface for *testQueryLog.
func (l *testQueryLog) ShouldLog(string, uint16, uint16, []string) bool {
	return true
}

// testStats is a simple [stats.Interface] implementation for tests.
type testStats struct {
	// Stats is embedded here simply to make testStats a [stats.Interface]
	// without actually implementing all methods.
	stats.Interface

	lastEntry *stats.Entry
}

// Update implements the [stats.Interface] interface for *testStats.
func (l *testStats) Update(e *stats.Entry) {
	if e.Domain == "" {
		return
	}

	l.lastEntry = e
}

// ShouldCount implements the [stats.Interface] interface for *testStats.
func (l *testStats) ShouldCount(string, uint16, uint16, []string) bool {
	return true
}

// testUserQuotas records the last filtering outcome it was handed.
type testUserQuotas struct {
	lastClient  string
	lastBlocked bool
	lastPassed  bool

	// The latency observations are only counted, because the tests that use
	// this double are about the filtering outcome; a test that cares about
	// the durations checks the sum and the count instead.
	latencySum   time.Duration
	latencyCount int
}

// type check
var _ UserQuotas = (*testUserQuotas)(nil)

// AllowQuery implements the [UserQuotas] interface for *testUserQuotas.
func (u *testUserQuotas) AllowQuery(string, netip.Addr, string) (ok bool, reason string) {
	return true, ""
}

// ObserveLatency implements the [UserQuotas] interface for *testUserQuotas.
func (u *testUserQuotas) ObserveLatency(_ string, _ netip.Addr, d time.Duration) {
	u.latencySum += d
	u.latencyCount++
}

// RecordResult implements the [UserQuotas] interface for *testUserQuotas.
func (u *testUserQuotas) RecordResult(clientID string, blocked, passed bool) {
	u.lastClient = clientID
	u.lastBlocked = blocked
	u.lastPassed = passed
}

// TestFilteringOutcome checks how a filtering reason maps to the per-user
// counters: only an allow-list pass counts as passed, and only a rule
// rejection counts as blocked.
func TestFilteringOutcome(t *testing.T) {
	testCases := []struct {
		name        string
		reason      filtering.Reason
		wantBlocked bool
		wantPassed  bool
	}{{
		name:   "not_filtered",
		reason: filtering.NotFilteredNotFound,
	}, {
		name:       "allow_list_is_passed",
		reason:     filtering.NotFilteredAllowList,
		wantPassed: true,
	}, {
		name:        "block_list_is_blocked",
		reason:      filtering.FilteredBlockList,
		wantBlocked: true,
	}, {
		name:        "safe_browsing_is_blocked",
		reason:      filtering.FilteredSafeBrowsing,
		wantBlocked: true,
	}, {
		name:        "safe_search_is_blocked",
		reason:      filtering.FilteredSafeSearch,
		wantBlocked: true,
	}, {
		name:        "parental_is_blocked",
		reason:      filtering.FilteredParental,
		wantBlocked: true,
	}, {
		name:        "blocked_service_is_blocked",
		reason:      filtering.FilteredBlockedService,
		wantBlocked: true,
	}, {
		name:   "rewrite_is_neither",
		reason: filtering.Rewritten,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, passed := filteringOutcome(tc.reason)
			assert.Equal(t, tc.wantBlocked, blocked)
			assert.Equal(t, tc.wantPassed, passed)
		})
	}
}

// TestServer_UpdateStatsRecordsTheOutcome checks that the DNS path hands the
// filtering outcome of a query to the user manager, keyed by the client
// identifier that the statistics entry carries.
func TestServer_UpdateStatsRecordsTheOutcome(t *testing.T) {
	testCases := []struct {
		name        string
		reason      filtering.Reason
		wantBlocked bool
		wantPassed  bool
	}{{
		name:       "allow_list_is_passed",
		reason:     filtering.NotFilteredAllowList,
		wantPassed: true,
	}, {
		name:        "block_list_is_blocked",
		reason:      filtering.FilteredBlockList,
		wantBlocked: true,
	}, {
		name:   "not_filtered_is_neither",
		reason: filtering.NotFilteredNotFound,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uq := &testUserQuotas{}
			srv := &Server{
				baseLogger: testLogger,
				logger:     testLogger,
				stats:      &testStats{},
				userQuotas: uq,
				anonymizer: aghnet.NewIPMut(nil),
			}

			req := &dns.Msg{
				Question: []dns.Question{{Name: "example.com."}},
			}
			pctx := &proxy.DNSContext{
				Proto: proxy.ProtoTLS,
				Req:   req,
				Res:   &dns.Msg{},
				Addr:  testClientAddrPort,
			}
			dctx := &dnsContext{
				proxyCtx:  pctx,
				startTime: time.Now(),
				result:    &filtering.Result{Reason: tc.reason},
				clientID:  "cli42",
			}

			srv.updateStats(dctx, "1.2.3.4", time.Millisecond)

			assert.Equal(t, "cli42", uq.lastClient)
			assert.Equal(t, tc.wantBlocked, uq.lastBlocked)
			assert.Equal(t, tc.wantPassed, uq.lastPassed)
		})
	}
}

func TestServer_ProcessQueryLogsAndStats(t *testing.T) {
	const domain = "example.com."

	testCases := []struct {
		name           string
		domain         string
		proto          proxy.Proto
		addr           netip.AddrPort
		clientID       string
		wantLogProto   querylog.ClientProto
		wantStatClient string
		wantCode       resultCode
		reason         filtering.Reason
		wantStatResult stats.Result
	}{{
		name:           "success_udp",
		domain:         domain,
		proto:          proxy.ProtoUDP,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_tls_clientid",
		domain:         domain,
		proto:          proxy.ProtoTLS,
		addr:           testClientAddrPort,
		clientID:       "cli42",
		wantLogProto:   querylog.ClientProtoDoT,
		wantStatClient: "cli42",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_tls",
		domain:         domain,
		proto:          proxy.ProtoTLS,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   querylog.ClientProtoDoT,
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_quic",
		domain:         domain,
		proto:          proxy.ProtoQUIC,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   querylog.ClientProtoDoQ,
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_https",
		domain:         domain,
		proto:          proxy.ProtoHTTPS,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   querylog.ClientProtoDoH,
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_dnscrypt",
		domain:         domain,
		proto:          proxy.ProtoDNSCrypt,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   querylog.ClientProtoDNSCrypt,
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.NotFilteredNotFound,
		wantStatResult: stats.RNotFiltered,
	}, {
		name:           "success_udp_filtered",
		domain:         domain,
		proto:          proxy.ProtoUDP,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.FilteredBlockList,
		wantStatResult: stats.RFiltered,
	}, {
		name:           "success_udp_sb",
		domain:         domain,
		proto:          proxy.ProtoUDP,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.FilteredSafeBrowsing,
		wantStatResult: stats.RSafeBrowsing,
	}, {
		name:           "success_udp_ss",
		domain:         domain,
		proto:          proxy.ProtoUDP,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.FilteredSafeSearch,
		wantStatResult: stats.RSafeSearch,
	}, {
		name:           "success_udp_pc",
		domain:         domain,
		proto:          proxy.ProtoUDP,
		addr:           testClientAddrPort,
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "1.2.3.4",
		wantCode:       resultCodeSuccess,
		reason:         filtering.FilteredParental,
		wantStatResult: stats.RParental,
	}, {
		name:           "success_udp_pc_empty_fqdn",
		domain:         ".",
		proto:          proxy.ProtoUDP,
		addr:           netip.MustParseAddrPort("4.3.2.1:1234"),
		clientID:       "",
		wantLogProto:   "",
		wantStatClient: "4.3.2.1",
		wantCode:       resultCodeSuccess,
		reason:         filtering.FilteredParental,
		wantStatResult: stats.RParental,
	}}

	ups, err := upstream.AddressToUpstream("1.1.1.1", nil)
	require.NoError(t, err)

	for _, tc := range testCases {
		ql := &testQueryLog{}
		st := &testStats{}
		srv := &Server{
			baseLogger: testLogger,
			logger:     testLogger,
			queryLog:   ql,
			stats:      st,
			anonymizer: aghnet.NewIPMut(nil),
		}
		t.Run(tc.name, func(t *testing.T) {
			req := &dns.Msg{
				Question: []dns.Question{{
					Name: tc.domain,
				}},
			}
			pctx := &proxy.DNSContext{
				Proto:    tc.proto,
				Req:      req,
				Res:      &dns.Msg{},
				Addr:     tc.addr,
				Upstream: ups,
			}
			dctx := &dnsContext{
				proxyCtx:  pctx,
				startTime: time.Now(),
				result: &filtering.Result{
					Reason: tc.reason,
				},
				clientID: tc.clientID,
			}

			ctx := testutil.ContextWithTimeout(t, testTimeout)
			code := srv.processQueryLogsAndStats(ctx, testLogger, dctx)
			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantLogProto, ql.lastParams.ClientProto)
			assert.Equal(t, tc.wantStatClient, st.lastEntry.Client)
			assert.Equal(t, tc.wantStatResult, st.lastEntry.Result)
		})
	}
}
