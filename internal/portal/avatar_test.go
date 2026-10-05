package portal

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAvatarPresetsArePublic checks that the picker list is served without a
// session and comes straight from the user module, which is the single source
// of truth for both the front-end and the validation.
func TestAvatarPresetsArePublic(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleAvatarPresets, "/portal/api/avatar")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp avatarPresetsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, users.AvatarPresets(), resp.Presets)
}

func TestSetAvatar(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleAvatar, "/portal/api/avatar", `{"avatar":"🐱"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp avatarResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.Equal(t, "🐱", resp.Avatar)
	assert.Equal(t, "🐱", store.defs[testUID].Avatar)

	// The account response carries the avatar, so the panel does not need a
	// second request to draw it.
	me := get(t, m.handleMe, "/portal/api/me", cookie)
	require.Equal(t, http.StatusOK, me.Code)
	assert.Contains(t, me.Body.String(), "🐱")
}

func TestSetAvatarRejectsUnknown(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleAvatar, "/portal/api/avatar", `{"avatar":"cat"}`, cookie)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.defs[testUID].Avatar)
}

func TestSetAvatarNeedsASession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleAvatar, "/portal/api/avatar", `{"avatar":"🐱"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.defs[testUID].Avatar)
}
