package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortalTokenReplacesTheOriginList checks that a front-end carrying the
// deployment token may call the portal API from any origin.
//
// The token is what removes the settings: a header works over http and https
// from anywhere, while the origin allow-list required the administrator to
// register every address a front-end could be served from, and a cross-origin
// cookie forced HTTPS on the API on top of that.
func TestPortalTokenReplacesTheOriginList(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"

	m, store := newTestPortalWithToken(t, token)

	// No origins are configured at all.
	require.Empty(t, store.GetSettings().PortalOrigins)

	req := httptest.NewRequest(http.MethodGet, "/portal/api/public", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	req.Header.Set("X-Portal-Token", token)

	assert.Equal(t, "https://anywhere.example", m.allowedOrigin(req))
}

// TestPortalTokenIsRequired checks that an unknown origin without the token is
// still refused, so that dropping the allow-list does not open the API to
// every page on the internet.
func TestPortalTokenIsRequired(t *testing.T) {
	m, _ := newTestPortalWithToken(t, "0123456789abcdef0123456789abcdef")

	testCases := []struct {
		name   string
		origin string
		token  string
	}{{
		name:   "no token",
		origin: "https://anywhere.example",
	}, {
		name:   "wrong token",
		origin: "https://anywhere.example",
		token:  "ffffffffffffffffffffffffffffffff",
	}, {
		name:  "no origin",
		token: "0123456789abcdef0123456789abcdef",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/portal/api/public", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			if tc.token != "" {
				req.Header.Set("X-Portal-Token", tc.token)
			}

			assert.Empty(t, m.allowedOrigin(req))
		})
	}
}

// TestPreflightIsAnsweredWithoutTheToken checks that the preflight of a
// request carrying the token is answered.
//
// A preflight cannot carry custom headers, so the token cannot be checked on
// it.  Answering it leaks nothing: the request it precedes still has to
// present a valid token.
func TestPreflightIsAnsweredWithoutTheToken(t *testing.T) {
	m, _ := newTestPortalWithToken(t, "0123456789abcdef0123456789abcdef")

	req := httptest.NewRequest(http.MethodOptions, "/portal/api/public", nil)
	req.Header.Set("Origin", "https://anywhere.example")

	assert.Equal(t, "https://anywhere.example", m.allowedOrigin(req))

	w := httptest.NewRecorder()
	require.True(t, m.handleCORS(w, req))
	assert.Equal(t, http.StatusNoContent, w.Code)

	// The headers the front-end actually sends have to be allowed, or the
	// browser refuses the real request.
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "X-Portal-Token")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
}

// TestSessionTokenFromHeader checks that the session can be presented as a
// bearer token.
//
// This is what makes a cross-origin deployment work without cookies: a
// cross-origin cookie needs SameSite=None, which needs Secure, which a plain
// HTTP response cannot set.
func TestSessionTokenFromHeader(t *testing.T) {
	m, _ := newTestPortalWithToken(t, "0123456789abcdef0123456789abcdef")

	req := httptest.NewRequest(http.MethodGet, "/portal/api/me", nil)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")

	assert.False(t, isZeroToken(m.sessionToken(req)))
}

// newTestPortalWithToken returns a portal manager whose user store knows token.
func newTestPortalWithToken(t *testing.T, token string) (m *Manager, store *testUserStore) {
	t.Helper()

	// GetSettings must not return nil: the portal reads the settings on every
	// request.
	store = &testUserStore{portalToken: token, settings: &users.Settings{}}
	m, _ = newTestPortal(t, store)

	return m, store
}
