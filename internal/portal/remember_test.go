package portal

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loginRemembered signs the test user in the way a portal does when it wants
// the visitor to stay signed in, and returns the token the browser would keep.
func loginRemembered(tb testing.TB, m *Manager, store *testUserStore) (tok string) {
	tb.Helper()

	rec := post(tb, m.handleLogin, "/portal/api/login",
		`{"login":"testuser","password":"`+testPassword+`","remember":true}`)
	require.Equal(tb, http.StatusOK, rec.Code, rec.Body.String())

	got := &loginResponse{}
	require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), got))
	require.NotEmpty(tb, got.RememberToken)

	return got.RememberToken
}

// A sign-in that asks to be remembered hands back a long-lived token; one that
// does not gets nothing extra.
func TestLoginIssuesRememberToken(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleLogin, "/portal/api/login",
		`{"login":"testuser","password":"`+testPassword+`","remember":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := &loginResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), got))
	require.NotEmpty(t, got.RememberToken)
	assert.Positive(t, got.RememberExpire)

	rec = post(t, m.handleLogin, "/portal/api/login",
		`{"login":"testuser","password":"`+testPassword+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got = &loginResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), got))
	assert.Empty(t, got.RememberToken, "a caller that did not ask must not be handed one")
}

// The exchange is what turns a remembered device into a session, and the token
// it hands back is the only one that works afterwards.
func TestRememberExchangeResumesTheSession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	tok := loginRemembered(t, m, store)

	rec := post(t, m.handleRememberExchange, "/portal/api/remember/exchange",
		`{"token":"`+tok+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := &loginResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), got))
	require.NotNil(t, got.User)
	assert.Equal(t, testUID, got.User.UID)
	require.NotEmpty(t, got.Token)
	require.NotEmpty(t, got.RememberToken)
	assert.NotEqual(t, tok, got.RememberToken, "the token has to be rotated")

	// The session it handed out is a real one.
	c := &http.Cookie{Name: SessionCookieName, Value: got.Token}
	rec = get(t, m.handleMe, "/portal/api/me", c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The token that was presented is spent, the one it handed back is not.
	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+tok+`"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange",
		`{"token":"`+got.RememberToken+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// Anything that cannot be one of our tokens is refused before the store is
// asked, so a malformed request costs nothing.
func TestRememberExchangeRejectsGarbage(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	for _, tok := range []string{"", "short", strings.Repeat("z", 64), strings.Repeat("a", 63)} {
		rec := post(t, m.handleRememberExchange, "/portal/api/remember/exchange",
			`{"token":"`+tok+`"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "token %q", tok)
	}

	// A well-formed token that nobody issued is refused as well.
	rec := post(t, m.handleRememberExchange, "/portal/api/remember/exchange",
		`{"token":"`+strings.Repeat("ab", 32)+`"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Signing out revokes the device, which is the whole reason the token is
// revocable.
func TestRememberForgetSignsTheDeviceOut(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	tok := loginRemembered(t, m, store)

	rec := post(t, m.handleRememberForget, "/portal/api/remember/forget", `{"token":"`+tok+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+tok+`"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Signing out twice is not an error: the caller cannot be asked to know
	// whether the token was still there.
	rec = post(t, m.handleRememberForget, "/portal/api/remember/forget", `{"token":"`+tok+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The list is what makes the tokens manageable, and revoking from it really
// takes a device away.
func TestRememberListAndRevoke(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	phone := loginRemembered(t, m, store)
	laptop := loginRemembered(t, m, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := get(t, m.handleRememberList, "/portal/api/remember", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := &rememberListResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), got))
	require.Len(t, got.Devices, 2)
	assert.NotEmpty(t, got.Devices[0].ID)
	assert.NotContains(t, rec.Body.String(), phone, "the list must not hand the tokens back")

	// Signing the other devices out leaves the one that asked alone.
	keep := users.RememberHash(phone)
	rec = post(t, m.handleRememberRevoke, "/portal/api/remember/revoke",
		`{"all":true,"keep":"`+keep+`"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got = &rememberListResponse{}
	require.NoError(t, json.Unmarshal(get(t, m.handleRememberList, "/portal/api/remember", cookie).
		Body.Bytes(), got))
	require.Len(t, got.Devices, 1)
	assert.Equal(t, keep, got.Devices[0].ID)

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+laptop+`"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+phone+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The list and the revoke need a session; a request without one is refused.
func TestRememberListNeedsASession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	loginRemembered(t, m, store)

	rec := get(t, m.handleRememberList, "/portal/api/remember")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = post(t, m.handleRememberRevoke, "/portal/api/remember/revoke", `{"all":true}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// A session that asks for nothing in particular is a bad request, not a
	// silent no-op.
	cookie := sessionCookie(t, m, store, testUID)
	rec = post(t, m.handleRememberRevoke, "/portal/api/remember/revoke", `{}`, cookie)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Changing the password signs the other devices out and spares the one that
// asked, which is what keeps the feature from being a nuisance.
func TestPasswordChangeSparesTheCurrentDevice(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	phone := loginRemembered(t, m, store)
	laptop := loginRemembered(t, m, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handlePassword, "/portal/api/password",
		`{"old_password":"`+testPassword+`","new_password":"a-much-longer-one",`+
			`"remember_token":"`+phone+`"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+laptop+`"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "the other device has to be signed out")

	rec = post(t, m.handleRememberExchange, "/portal/api/remember/exchange", `{"token":"`+phone+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
