package portal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankingDefaultsToToday(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleRanking, "/portal/api/ranking")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, users.RankByToday, store.rankingOrder)

	var got rankingResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, users.RankByToday, got.Order)
}

func TestRankingOrderParameter(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleRanking, "/portal/api/ranking?order=total")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, users.RankByTotal, store.rankingOrder)

	var got rankingResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, users.RankByTotal, got.Order)

	// An order the portal does not know is not an error: the board is public
	// and reached by links, so a stale one falls back to the default instead
	// of failing.
	rec = get(t, m.handleRanking, "/portal/api/ranking?order=nonsense")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, users.RankByToday, store.rankingOrder)

	// The old name of the tab still names the same board.
	rec = get(t, m.handleRanking, "/portal/api/ranking?order=24h")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, users.RankByToday, store.rankingOrder)
}

func TestLogIsLockedUntilTheCheckinStreak(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := get(t, m.handleLog, "/portal/api/log", cookie)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// A session is still required: the lock is not a replacement for it.
	rec = get(t, m.handleLog, "/portal/api/log")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	store.logUnlocked = true

	rec = get(t, m.handleLog, "/portal/api/log", cookie)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestProbeForAVisitorWithoutASession(t *testing.T) {
	store := newTestStore()
	store.portalToken = "0123456789abcdef0123456789abcdef"

	m, _ := newTestPortal(t, store)

	const owner = "0123456789abcdef0123"

	probe := func(tok, query string) (rec *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodPost, "/portal/api/probe?"+query, nil)
		if tok != "" {
			req.Header.Set("X-Portal-Token", tok)
		}

		rec = httptest.NewRecorder()
		m.handleProbe(rec, req)

		return rec
	}

	// Without the portal token an unauthenticated caller cannot add to the
	// probe table, which is what keeps it from being filled from outside.
	assert.Equal(t, http.StatusBadRequest, probe("", "owner="+owner).Code)

	// A portal token but no usable owner is refused as well.
	assert.Equal(t, http.StatusBadRequest, probe(store.portalToken, "").Code)
	assert.Equal(t, http.StatusBadRequest, probe(store.portalToken, "owner=short").Code)
	assert.Equal(t, http.StatusBadRequest, probe(store.portalToken, "owner=0123456789abcde.").Code)

	rec := probe(store.portalToken, "owner="+owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The key is namespaced, so a visitor can never name an account and read
	// that account's probe.
	assert.Equal(t, anonymousProbePrefix+owner, store.probeOwner)
}

func TestProbeOfASignedInUserUsesTheAccount(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)
	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleProbe, "/portal/api/probe?owner=0123456789abcdef0123", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, testUID, store.probeOwner)
}

func TestIsValidProbeOwner(t *testing.T) {
	assert.True(t, isValidProbeOwner("0123456789abcdef"))
	assert.True(t, isValidProbeOwner("aB3-_0123456789abcdef"))

	assert.False(t, isValidProbeOwner(""))
	assert.False(t, isValidProbeOwner("short"))
	assert.False(t, isValidProbeOwner("0123456789abcde."))
	assert.False(t, isValidProbeOwner("0123456789abcdef "))

	// An account identifier must not be usable as an anonymous key, because
	// the namespace prefix is what keeps the two apart.
	assert.False(t, isValidProbeOwner("anon:0123456789abcdef"))
}
