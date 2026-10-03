package querylog

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghnet"
	"github.com/AdguardTeam/AdGuardHome/internal/filtering"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClientIDs are the client identifiers the search tests use.
const (
	testClientIDOne = "one"
	testClientIDTwo = "two"
)

// testClientIPv6 is another test client address.
var testClientIPv6 = net.IPv4(198, 51, 100, 7)

// newSearchTestQueryLog returns a query log with three entries: two belong to
// the client identified as testClientIDOne and one to the client identified as
// testClientIDTwo.
func newSearchTestQueryLog(tb testing.TB) (l *queryLog) {
	tb.Helper()

	l, err := newQueryLog(Config{
		Logger:      testLogger,
		Enabled:     true,
		FileEnabled: true,
		RotationIvl: timeutil.Day,
		MemSize:     100,
		BaseDir:     tb.TempDir(),
		Anonymizer:  aghnet.NewIPMut(nil),
	})
	require.NoError(tb, err)

	addIdentifiedEntry(tb, l, "one.example", testClientIDOne, testClientIPv4)
	addIdentifiedEntry(tb, l, "two.example", testClientIDOne, testClientIPv4)
	addIdentifiedEntry(tb, l, "other.example", testClientIDTwo, testClientIPv6)

	return l
}

// addIdentifiedEntry adds a test entry that belongs to the given client.  It
// is [addTestEntry] with the client identifier set, which is what the portal
// filters on.
func addIdentifiedEntry(tb testing.TB, l *queryLog, host, clientID string, client net.IP) {
	tb.Helper()

	q := dns.Msg{
		Question: []dns.Question{{
			Name:   host + ".",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}

	a := dns.Msg{
		Question: q.Question,
		Answer: []dns.RR{&dns.A{
			Hdr: dns.RR_Header{
				Name:   q.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
			},
			A: net.IP(testAnswerIPv4),
		}},
	}

	res := filtering.Result{Reason: filtering.NotFilteredNotFound}

	l.Add(&AddParams{
		Question:   &q,
		Answer:     &a,
		OrigAnswer: &a,
		Result:     &res,
		Upstream:   "upstream",
		ClientID:   clientID,
		ClientIP:   client,
	})
}

// hostsOfEntries returns the host names of the entries in the response.
func hostsOfEntries(tb testing.TB, resp *SearchResponse) (hosts []string) {
	tb.Helper()

	data, ok := resp.Entries["data"].([]jobject)
	require.True(tb, ok, "unexpected data type %T", resp.Entries["data"])

	for _, e := range data {
		host, _ := e["question"].(map[string]any)["name"].(string)
		hosts = append(hosts, host)
	}

	return hosts
}

func TestQueryLogSearch(t *testing.T) {
	ctx := testutil.ContextWithTimeout(t, testTimeout)

	l := newSearchTestQueryLog(t)

	// The file path must return the same entries as the memory one, so the
	// test runs both.
	t.Run("memory", func(t *testing.T) {
		testQueryLogSearch(ctx, t, l)
	})

	t.Run("file", func(t *testing.T) {
		err := l.flushLogBuffer(ctx)
		require.NoError(t, err)

		testQueryLogSearch(ctx, t, l)
	})
}

// testQueryLogSearch checks the client filtering against a query log that may
// hold its entries in memory or on disk.
func testQueryLogSearch(ctx context.Context, t *testing.T, l *queryLog) {
	t.Helper()

	t.Run("by_client_id", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{testClientIDOne},
			Limit:     10,
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"two.example", "one.example"}, hostsOfEntries(t, resp))
	})

	t.Run("by_client_id_other", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{testClientIDTwo},
			Limit:     10,
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"other.example"}, hostsOfEntries(t, resp))
	})

	t.Run("by_client_id_case_insensitive", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{"ONE"},
			Limit:     10,
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"two.example", "one.example"}, hostsOfEntries(t, resp))
	})

	t.Run("by_address", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientNets: []netip.Prefix{netip.MustParsePrefix("198.51.100.7/32")},
			Limit:      10,
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"other.example"}, hostsOfEntries(t, resp))
	})

	t.Run("by_network", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			Limit:      10,
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"two.example", "one.example"}, hostsOfEntries(t, resp))
	})

	t.Run("client_id_and_network_are_ored", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs:  []string{testClientIDTwo},
			ClientNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
			Limit:      10,
		})
		require.NoError(t, err)

		assert.Equal(
			t,
			[]string{"other.example", "two.example", "one.example"},
			hostsOfEntries(t, resp),
		)
	})

	t.Run("unknown_client", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{"nobody"},
			Limit:     10,
		})
		require.NoError(t, err)
		assert.Empty(t, hostsOfEntries(t, resp))
	})

	t.Run("term", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{testClientIDOne},
			Term:      "one.example",
			Limit:     10,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"one.example"}, hostsOfEntries(t, resp))
	})

	t.Run("limit", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{
			ClientIDs: []string{testClientIDOne},
			Limit:     1,
		})
		require.NoError(t, err)
		assert.Len(t, hostsOfEntries(t, resp), 1)
	})
}

func TestQueryLogSearchErrors(t *testing.T) {
	ctx := testutil.ContextWithTimeout(t, testTimeout)

	l := newSearchTestQueryLog(t)

	t.Run("nil_request", func(t *testing.T) {
		resp, err := l.Search(ctx, nil)
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("no_limit", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{ClientIDs: []string{"one"}})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("negative_limit", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{ClientIDs: []string{"one"}, Limit: -1})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("no_client_filter", func(t *testing.T) {
		// Without a client filter the search would return the entries of
		// every user, which must never happen.
		resp, err := l.Search(ctx, &SearchRequest{Limit: 10})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("empty_client_filter", func(t *testing.T) {
		resp, err := l.Search(ctx, &SearchRequest{ClientIDs: []string{}, Limit: 10})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSearchParamsMatchesClient(t *testing.T) {
	testCases := []struct {
		name     string
		clientID string
		ip       net.IP
		wantOK   bool
		params   *searchParams
	}{{
		name:     "unrestricted",
		params:   &searchParams{},
		clientID: "any",
		ip:       testClientIPv4,
		wantOK:   true,
	}, {
		name:     "matching_id",
		params:   &searchParams{clientIDs: []string{"one"}},
		clientID: "one",
		ip:       testClientIPv4,
		wantOK:   true,
	}, {
		name:     "matching_id_other_case",
		params:   &searchParams{clientIDs: []string{"one"}},
		clientID: "ONE",
		ip:       testClientIPv4,
		wantOK:   true,
	}, {
		name:     "other_id",
		params:   &searchParams{clientIDs: []string{"one"}},
		clientID: "two",
		ip:       testClientIPv4,
		wantOK:   false,
	}, {
		name:     "empty_id_never_matches",
		params:   &searchParams{clientIDs: []string{""}},
		clientID: "",
		ip:       testClientIPv4,
		wantOK:   false,
	}, {
		name:     "matching_net",
		params:   &searchParams{clientNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}},
		clientID: "",
		ip:       testClientIPv4,
		wantOK:   true,
	}, {
		name:     "other_net",
		params:   &searchParams{clientNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}},
		clientID: "",
		ip:       testClientIPv6,
		wantOK:   false,
	}, {
		name:     "invalid_ip",
		params:   &searchParams{clientNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}},
		clientID: "",
		ip:       nil,
		wantOK:   false,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantOK, tc.params.matchesClient(tc.clientID, tc.ip))
		})
	}
}

func TestSearchRequestOlderThan(t *testing.T) {
	ctx := testutil.ContextWithTimeout(t, testTimeout)

	l := newSearchTestQueryLog(t)

	// Every test entry is created now, so none of them is older than an hour
	// ago.  OlderThan keeps the entries that are older than it, which is what
	// the pagination of the log view relies on.
	resp, err := l.Search(ctx, &SearchRequest{
		ClientIDs: []string{testClientIDOne},
		OlderThan: time.Now().Add(-time.Hour),
		Limit:     10,
	})
	require.NoError(t, err)
	assert.Empty(t, hostsOfEntries(t, resp))

	// A moment in the future keeps everything.
	resp, err = l.Search(ctx, &SearchRequest{
		ClientIDs: []string{testClientIDOne},
		OlderThan: time.Now().Add(time.Hour),
		Limit:     10,
	})
	require.NoError(t, err)
	assert.Len(t, hostsOfEntries(t, resp), 2)
}
