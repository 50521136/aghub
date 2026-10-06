package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// portalRequest builds a request as a portal deployment makes it: from the web
// host, carrying the portal token and the visitor it is acting for.
func portalRequest(tb testing.TB, tok, fwdIP, fwdUA string) (r *http.Request) {
	tb.Helper()

	r = httptest.NewRequest(http.MethodGet, "http://example.com/portal/api/me", nil)
	r.RemoteAddr = "192.0.2.9:1234"
	r.Header.Set("User-Agent", "curl/8.5.0")

	if tok != "" {
		r.Header.Set("X-Portal-Token", tok)
	}

	if fwdIP != "" {
		r.Header.Set(portalClientIPHeader, fwdIP)
	}

	if fwdUA != "" {
		r.Header.Set(portalClientUAHeader, fwdUA)
	}

	return r
}

// The portal calls AGHub from the host that serves it, so the address of the
// request is the web server's and the connection card would name the wrong
// machine.  A portal forwards its visitor instead, and the forwarded values are
// honoured only for a request that proves it is the portal.
func TestInfoForRequestVisitor(t *testing.T) {
	ctx := context.Background()

	// The card is built from the forwarded values when the portal sent them.
	t.Run("portal_forwards_visitor", func(t *testing.T) {
		store := newTestStore()
		m, _ := newTestPortal(t, store)

		tok, err := store.EnsurePortalToken()
		require.NoError(t, err)

		info := m.InfoForRequest(ctx, portalRequest(t, tok, "203.0.113.7", "Mozilla/5.0 (iPhone)"), store.defs[testUID])
		require.NotNil(t, info)
		assert.Equal(t, "203.0.113.7", info.Client.IP)
		assert.NotEqual(t, "curl/8.5.0", info.Client.Device)
	})

	// Without the token the headers are just something a client made up, and
	// the card describes the request itself.
	t.Run("no_token_ignores_headers", func(t *testing.T) {
		store := newTestStore()
		m, _ := newTestPortal(t, store)

		info := m.InfoForRequest(ctx, portalRequest(t, "", "203.0.113.7", "Mozilla/5.0 (iPhone)"), store.defs[testUID])
		require.NotNil(t, info)
		assert.Equal(t, "192.0.2.9", info.Client.IP)
	})

	// A wrong token is worth no more than no token.
	t.Run("wrong_token_ignores_headers", func(t *testing.T) {
		store := newTestStore()
		m, _ := newTestPortal(t, store)

		_, err := store.EnsurePortalToken()
		require.NoError(t, err)

		info := m.InfoForRequest(ctx, portalRequest(t, "not-the-token", "203.0.113.7", "Mozilla/5.0"), store.defs[testUID])
		require.NotNil(t, info)
		assert.Equal(t, "192.0.2.9", info.Client.IP)
	})

	// A portal behind a reverse proxy passes the list on, and the visitor is
	// the first entry.
	t.Run("forwarded_list", func(t *testing.T) {
		store := newTestStore()
		m, _ := newTestPortal(t, store)

		tok, err := store.EnsurePortalToken()
		require.NoError(t, err)

		info := m.InfoForRequest(ctx, portalRequest(t, tok, "203.0.113.7, 10.0.0.1", ""), store.defs[testUID])
		require.NotNil(t, info)
		assert.Equal(t, "203.0.113.7", info.Client.IP)
	})

	// Garbage in the header must not cost the card its address.
	t.Run("bad_forwarded_ip", func(t *testing.T) {
		store := newTestStore()
		m, _ := newTestPortal(t, store)

		tok, err := store.EnsurePortalToken()
		require.NoError(t, err)

		info := m.InfoForRequest(ctx, portalRequest(t, tok, "not an address", ""), store.defs[testUID])
		require.NotNil(t, info)
		assert.Equal(t, "192.0.2.9", info.Client.IP)
	})
}
