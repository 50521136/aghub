package users

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addRememberedUser adds a user that can sign in to the portal.
func addRememberedUser(t *testing.T, m *Manager) (u *User) {
	t.Helper()

	u = addUser(t, m, &AddParams{
		Name:         "alice",
		IDs:          []string{"alice"},
		RequestLimit: iptr(1000),
		Period:       PeriodDay,
		Enabled:      true,
	})

	require.NoError(t, m.SetPortalPassword(u.UID, "portal-password"))

	return u
}

// A remembered device is issued once and then rotated on every use, so the
// token a browser holds is never the one it was given at sign-in.
func TestRememberIssueAndRotate(t *testing.T) {
	m, now := newTestManager(t)
	u := addRememberedUser(t, m)

	first, info, err := m.IssueRemember(u.UID, "Mozilla/5.0 (iPhone)", "203.0.113.7")
	require.NoError(t, err)
	require.NotEmpty(t, first)
	assert.Equal(t, RememberHash(first), info.ID)
	assert.Equal(t, "Mozilla/5.0 (iPhone)", info.UserAgent)

	uid, second, info2, err := m.ExchangeRemember(first, "Mozilla/5.0 (iPhone)", "203.0.113.7")
	require.NoError(t, err)
	assert.Equal(t, u.UID, uid)
	assert.NotEqual(t, first, second, "the token has to be rotated")
	assert.Equal(t, RememberHash(second), info2.ID)

	// The device is still listed, once: the rotated token replaced the old one
	// rather than being added next to it.
	list := m.Remembered(u.UID)
	require.Len(t, list, 1)
	assert.Equal(t, RememberHash(second), list[0].ID)

	// The replacement works, and the grace period for the replaced one is over
	// once the clock moves past it.
	*now = now.Add(2 * rememberGrace)
	_, third, _, err := m.ExchangeRemember(second, "", "")
	require.NoError(t, err)
	assert.NotEqual(t, second, third)
}

// Two tabs of one browser resume at the same moment and both hold the same
// token.  Only one can win the rotation, and the other must not be signed out
// for it.
func TestRememberGraceKeepsTwoTabs(t *testing.T) {
	m, now := newTestManager(t)
	u := addRememberedUser(t, m)

	tok, _, err := m.IssueRemember(u.UID, "Mozilla/5.0", "203.0.113.7")
	require.NoError(t, err)

	_, first, _, err := m.ExchangeRemember(tok, "", "")
	require.NoError(t, err)

	*now = now.Add(rememberGrace / 2)

	_, second, _, err := m.ExchangeRemember(tok, "", "")
	require.NoError(t, err, "the replaced token still works inside the grace window")
	assert.NotEqual(t, first, second)

	// Both of them work afterwards.
	_, _, _, err = m.ExchangeRemember(first, "", "")
	require.NoError(t, err)

	_, _, _, err = m.ExchangeRemember(second, "", "")
	require.NoError(t, err)
}

// A token that comes back long after it was replaced is a copy somebody else
// is holding.  Both the copy and the device it was taken from are signed out:
// that is the only answer that does not leave a thief with a working token.
func TestRememberReuseRevokesTheDevice(t *testing.T) {
	m, now := newTestManager(t)
	u := addRememberedUser(t, m)

	tok, _, err := m.IssueRemember(u.UID, "Mozilla/5.0", "203.0.113.7")
	require.NoError(t, err)

	_, rotated, _, err := m.ExchangeRemember(tok, "", "")
	require.NoError(t, err)

	*now = now.Add(2 * rememberGrace)

	_, _, _, err = m.ExchangeRemember(tok, "", "")
	assert.ErrorIs(t, err, ErrRememberUnknown)

	_, _, _, err = m.ExchangeRemember(rotated, "", "")
	assert.ErrorIs(t, err, ErrRememberUnknown, "the whole device is signed out")
	assert.Empty(t, m.Remembered(u.UID))

	// A device that raced itself within the grace period has two live tokens,
	// and it is still one device.
	m2, _ := newTestManager(t)
	u2 := addRememberedUser(t, m2)

	first, _, err := m2.IssueRemember(u2.UID, "", "")
	require.NoError(t, err)

	_, second, _, err := m2.ExchangeRemember(first, "", "")
	require.NoError(t, err)

	_, third, _, err := m2.ExchangeRemember(first, "", "")
	require.NoError(t, err)

	assert.NotEqual(t, second, third)
	assert.Len(t, m2.Remembered(u2.UID), 1, "two tabs are still one device")

	// A replaced token that comes back only after the reuse window has passed
	// is simply unknown: there is nothing left to revoke, and the device it
	// was taken from has moved on.
	tok2, _, err := m.IssueRemember(u.UID, "", "")
	require.NoError(t, err)

	_, rotated2, _, err := m.ExchangeRemember(tok2, "", "")
	require.NoError(t, err)

	*now = now.Add(rememberReuseWindow + time.Hour)

	_, _, _, err = m.ExchangeRemember(tok2, "", "")
	assert.ErrorIs(t, err, ErrRememberUnknown)

	_, _, _, err = m.ExchangeRemember(rotated2, "", "")
	assert.NoError(t, err, "the device itself is untouched")
}

// A device that is not used for longer than the window stops working, and a
// device that keeps being used does not.
func TestRememberExpiry(t *testing.T) {
	m, now := newTestManager(t)
	u := addRememberedUser(t, m)

	tok, _, err := m.IssueRemember(u.UID, "", "")
	require.NoError(t, err)

	// Using it every month keeps it alive: the expiry slides.
	for range 3 {
		*now = now.Add(30 * 24 * time.Hour)

		_, tok, _, err = m.ExchangeRemember(tok, "", "")
		require.NoError(t, err)
	}

	// Leaving it alone for the whole window does not.
	*now = now.Add(RememberTTL + time.Hour)

	_, _, _, err = m.ExchangeRemember(tok, "", "")
	assert.ErrorIs(t, err, ErrRememberUnknown)
	assert.Empty(t, m.Remembered(u.UID))
}

// Signing out drops the device; changing the password drops every device but
// the one that asked.
func TestRememberRevoke(t *testing.T) {
	m, _ := newTestManager(t)
	u := addRememberedUser(t, m)

	phone, _, err := m.IssueRemember(u.UID, "phone", "203.0.113.7")
	require.NoError(t, err)

	laptop, _, err := m.IssueRemember(u.UID, "laptop", "203.0.113.8")
	require.NoError(t, err)

	require.Len(t, m.Remembered(u.UID), 2)

	require.NoError(t, m.ForgetRemember(phone))
	require.Len(t, m.Remembered(u.UID), 1)

	// Revoking an unknown token is not an error: the caller is signing out.
	require.NoError(t, m.ForgetRemember("0123456789abcdef"))

	// A password change spares the device that made it.
	phone, _, err = m.IssueRemember(u.UID, "phone", "203.0.113.7")
	require.NoError(t, err)

	keep := RememberHash(phone)
	assert.Equal(t, 1, m.ForgetRememberAll(u.UID, keep))

	list := m.Remembered(u.UID)
	require.Len(t, list, 1)
	assert.Equal(t, keep, list[0].ID)
	assert.NotEqual(t, RememberHash(laptop), list[0].ID)

	// Revoking by id is what the device list uses.
	require.NoError(t, m.ForgetRememberID(u.UID, list[0].ID))
	assert.Empty(t, m.Remembered(u.UID))

	// A device of another account cannot be revoked by id.
	other := addUser(t, m, &AddParams{Name: "bob", IDs: []string{"bob"}, Enabled: true})
	otherTok, _, err := m.IssueRemember(other.UID, "", "")
	require.NoError(t, err)

	require.NoError(t, m.ForgetRememberID(u.UID, RememberHash(otherTok)))
	assert.Len(t, m.Remembered(other.UID), 1)
}

// The state file must never contain a working token: a backup of it would
// otherwise be a set of credentials.
func TestRememberStateHoldsHashesOnly(t *testing.T) {
	m, now := newTestManager(t)
	u := addRememberedUser(t, m)

	tok, _, err := m.IssueRemember(u.UID, "phone", "203.0.113.7")
	require.NoError(t, err)

	require.NoError(t, m.save())

	data, err := os.ReadFile(m.path)
	require.NoError(t, err)

	assert.NotContains(t, string(data), tok, "the token itself is in the state file")
	assert.Contains(t, string(data), RememberHash(tok), "the hash is what gets stored")

	// A device survives a restart: this is what makes it worth having.
	reopened, err := New(&Config{Logger: testLogger(), Path: m.path, Location: time.UTC})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	reopened.now = func() (t time.Time) { return *now }

	uid, _, _, err := reopened.ExchangeRemember(tok, "", "")
	require.NoError(t, err, "a remembered device has to outlive a restart")
	assert.Equal(t, u.UID, uid)

	// An expired one does not come back from the file.
	*now = now.Add(RememberTTL + time.Hour)
	require.NoError(t, reopened.save())

	again, err := New(&Config{Logger: testLogger(), Path: m.path, Location: time.UTC})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, again.Close()) })

	again.now = func() (t time.Time) { return *now }
	assert.Empty(t, again.Remembered(u.UID))
}

// One account cannot accumulate devices without limit.
func TestRememberDeviceCap(t *testing.T) {
	m, _ := newTestManager(t)
	u := addRememberedUser(t, m)

	for range maxRememberDevices + 3 {
		_, _, err := m.IssueRemember(u.UID, "phone", "203.0.113.7")
		require.NoError(t, err)
	}

	assert.Len(t, m.Remembered(u.UID), maxRememberDevices)
}

// Deleting an account, or taking its portal access away, takes the remembered
// devices with it.
func TestRememberFollowsTheAccount(t *testing.T) {
	t.Run("removed", func(t *testing.T) {
		m, _ := newTestManager(t)
		u := addRememberedUser(t, m)

		tok, _, err := m.IssueRemember(u.UID, "", "")
		require.NoError(t, err)

		require.NoError(t, m.Remove(u.UID))

		_, _, _, err = m.ExchangeRemember(tok, "", "")
		assert.ErrorIs(t, err, ErrRememberUnknown)
	})

	t.Run("portal_access_revoked", func(t *testing.T) {
		m, _ := newTestManager(t)
		u := addRememberedUser(t, m)

		tok, _, err := m.IssueRemember(u.UID, "", "")
		require.NoError(t, err)

		require.NoError(t, m.ClearPortalPassword(u.UID))

		_, _, _, err = m.ExchangeRemember(tok, "", "")
		assert.ErrorIs(t, err, ErrRememberUnknown)
	})

	t.Run("admin_reset", func(t *testing.T) {
		m, _ := newTestManager(t)
		u := addRememberedUser(t, m)

		tok, _, err := m.IssueRemember(u.UID, "", "")
		require.NoError(t, err)

		// The administrator resetting the password is a security action: every
		// device that was signed in against the old one goes.
		require.NoError(t, m.SetPortalPassword(u.UID, "another-long-password"))

		_, _, _, err = m.ExchangeRemember(tok, "", "")
		assert.ErrorIs(t, err, ErrRememberUnknown)
	})

	t.Run("visitor_changes_own_password", func(t *testing.T) {
		m, _ := newTestManager(t)
		u := addRememberedUser(t, m)

		phone, _, err := m.IssueRemember(u.UID, "phone", "")
		require.NoError(t, err)

		laptop, _, err := m.IssueRemember(u.UID, "laptop", "")
		require.NoError(t, err)

		require.NoError(t, m.SetPortalPasswordKeeping(u.UID, "another-long-password",
			RememberHash(phone)))

		// The device that made the change is still signed in; the other one is
		// not, because a token that outlived the password is a way back in.
		_, _, _, err = m.ExchangeRemember(phone, "phone", "")
		assert.NoError(t, err)

		_, _, _, err = m.ExchangeRemember(laptop, "laptop", "")
		assert.ErrorIs(t, err, ErrRememberUnknown)
	})

	t.Run("state_file_dropped", func(t *testing.T) {
		m, _ := newTestManager(t)
		u := addRememberedUser(t, m)

		tok, _, err := m.IssueRemember(u.UID, "", "")
		require.NoError(t, err)
		require.NoError(t, m.save())

		// A token whose account is not in the file any more is not restored.
		require.NoError(t, os.WriteFile(m.path, []byte(`{"users":[],"usage":{},"version":1,`+
			`"remember_tokens":{"`+RememberHash(tok)+`":{"uid":"`+u.UID+`",`+
			`"family":"f","created":"2026-10-02T12:00:00Z","last_used":"2026-10-02T12:00:00Z",`+
			`"expire":"2036-10-02T12:00:00Z"}}}`), 0o600))

		reopened, err := New(&Config{Logger: testLogger(), Path: m.path, Location: time.UTC})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, reopened.Close()) })

		assert.Empty(t, reopened.Remembered(u.UID))
	})
}

// The window is long enough to be worth having and the grace period is short
// enough not to matter, and both are constants of the store rather than of a
// handler.
func TestRememberWindows(t *testing.T) {
	assert.Equal(t, 90*24*time.Hour, RememberTTL)
	assert.LessOrEqual(t, rememberGrace, time.Minute)
}

// The hash of a token is stable and is not the token.
func TestRememberHash(t *testing.T) {
	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	hash := RememberHash(tok)
	assert.Len(t, hash, 64)
	assert.NotEqual(t, tok, hash)
	assert.Equal(t, hash, RememberHash(tok))

	// A one-character change makes it a different device.
	assert.NotEqual(t, hash, RememberHash(tok[:len(tok)-1]+"0"))
}

// The state file of an account that has no remembered devices stays clean.
func TestRememberAbsentFromState(t *testing.T) {
	m, _ := newTestManager(t)
	addRememberedUser(t, m)

	require.NoError(t, m.save())

	data, err := os.ReadFile(filepath.Clean(m.path))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "remember_tokens")
}
