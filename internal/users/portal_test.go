package users

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// portalTestPassword is a password that satisfies the length limits.
const portalTestPassword = "correct-horse-battery"

// addPortalUser adds a user whose identifier is its name and returns it.
func addPortalUser(t *testing.T, m *Manager, name string) (u *User) {
	t.Helper()

	u, err := m.Add(&AddParams{Name: name, IDs: []string{name}, Enabled: true})
	require.NoError(t, err)

	return u
}

func TestSetPortalPasswordAndAuthenticate(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	assert.False(t, m.HasPortalPassword(u.UID))

	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))

	assert.True(t, m.HasPortalPassword(u.UID))
	assert.True(t, m.AuthenticatePortal(u.UID, portalTestPassword))
	assert.False(t, m.AuthenticatePortal(u.UID, "wrong-password"))
}

func TestSetPortalPasswordReplacesTheOldOne(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))
	require.NoError(t, m.SetPortalPassword(u.UID, "another-password"))

	assert.True(t, m.AuthenticatePortal(u.UID, "another-password"))
	assert.False(t, m.AuthenticatePortal(u.UID, portalTestPassword))
}

func TestSetPortalPasswordRejectsBadLengths(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	err := m.SetPortalPassword(u.UID, strings.Repeat("a", minPortalPasswordLen-1))
	assert.Error(t, err)
	assert.False(t, m.HasPortalPassword(u.UID))

	err = m.SetPortalPassword(u.UID, strings.Repeat("a", maxPortalPasswordLen+1))
	assert.Error(t, err)
	assert.False(t, m.HasPortalPassword(u.UID))
}

func TestSetPortalPasswordUnknownUser(t *testing.T) {
	m, _ := newTestManager(t)

	assert.Error(t, m.SetPortalPassword("nobody", portalTestPassword))
	assert.Error(t, m.ClearPortalPassword("nobody"))
}

func TestClearPortalPassword(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))
	require.NoError(t, m.ClearPortalPassword(u.UID))

	assert.False(t, m.HasPortalPassword(u.UID))
	assert.False(t, m.AuthenticatePortal(u.UID, portalTestPassword))

	// Clearing twice is not an error.
	assert.NoError(t, m.ClearPortalPassword(u.UID))
}

// TestAuthenticatePortalWithoutPassword makes sure that a user without a portal
// password cannot sign in, whatever is sent as the password.
func TestAuthenticatePortalWithoutPassword(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	for _, passwd := range []string{"", "alice", portalTestPassword} {
		assert.False(t, m.AuthenticatePortal(u.UID, passwd))
	}
}

func TestFindByLoginByIdentifier(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	// The identifiers are host name labels, so the lookup ignores the case.
	for _, login := range []string{"alice", "ALICE", "  Alice  "} {
		found := m.FindByLogin(login)
		require.NotNil(t, found, "login %q", login)
		assert.Equal(t, u.UID, found.UID)
	}
}

func TestFindByLoginByName(t *testing.T) {
	m, _ := newTestManager(t)

	u, err := m.Add(&AddParams{Name: "张三", IDs: []string{"zhangsan"}, Enabled: true})
	require.NoError(t, err)

	found := m.FindByLogin("张三")
	require.NotNil(t, found)
	assert.Equal(t, u.UID, found.UID)
}

// TestFindByLoginAmbiguousName makes sure that a name shared by two users does
// not resolve, because signing in as the wrong user would leak their log.
func TestFindByLoginAmbiguousName(t *testing.T) {
	m, _ := newTestManager(t)

	_, err := m.Add(&AddParams{Name: "同名", IDs: []string{"first"}, Enabled: true})
	require.NoError(t, err)
	_, err = m.Add(&AddParams{Name: "同名", IDs: []string{"second"}, Enabled: true})
	require.NoError(t, err)

	assert.Nil(t, m.FindByLogin("同名"))

	// The identifiers of those users still resolve, because they are unique.
	require.NotNil(t, m.FindByLogin("first"))
	require.NotNil(t, m.FindByLogin("second"))
}

func TestFindByLoginUnknown(t *testing.T) {
	m, _ := newTestManager(t)
	addPortalUser(t, m, "alice")

	for _, login := range []string{"", "   ", "bob"} {
		assert.Nil(t, m.FindByLogin(login), "login %q", login)
	}
}

func TestPortalPasswordPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	m, err := New(&Config{Logger: testLogger(), Path: path})
	require.NoError(t, err)

	u, err := m.Add(&AddParams{Name: "alice", IDs: []string{"alice"}, Enabled: true})
	require.NoError(t, err)
	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))

	require.NoError(t, m.save())

	reloaded, err := New(&Config{Logger: testLogger(), Path: path})
	require.NoError(t, err)

	assert.True(t, reloaded.HasPortalPassword(u.UID))
	assert.True(t, reloaded.AuthenticatePortal(u.UID, portalTestPassword))
}

// TestPortalPasswordIsNotExposed makes sure that the hash never reaches the
// administrator API, which serializes [Info] with [User] embedded.
func TestPortalPasswordIsNotExposed(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))

	infos := m.List()
	require.Len(t, infos, 1)

	data, err := json.Marshal(infos)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "$2a$")
	assert.NotContains(t, string(data), "portal_password_hash")
	assert.Contains(t, string(data), `"has_portal_password":true`)

	// The state file does keep the hash, otherwise the password would be lost
	// on every restart.
	state, err := json.Marshal(&stateFile{PortalPasswords: m.portalPasswords})
	require.NoError(t, err)
	assert.Contains(t, string(state), "$2a$")
}

func TestRemoveDropsPortalPassword(t *testing.T) {
	m, _ := newTestManager(t)
	u := addPortalUser(t, m, "alice")

	require.NoError(t, m.SetPortalPassword(u.UID, portalTestPassword))
	require.NoError(t, m.Remove(u.UID))

	assert.False(t, m.HasPortalPassword(u.UID))

	// A new user must not inherit the password through a reused UID.
	_, ok := m.portalPasswords[u.UID]
	assert.False(t, ok)
}

// TestImportDropsPasswordsOfRemovedUsers makes sure that replacing the user
// list also drops the credentials of the users that are gone.
func TestImportDropsPasswordsOfRemovedUsers(t *testing.T) {
	m, _ := newTestManager(t)

	kept := addPortalUser(t, m, "kept")
	gone := addPortalUser(t, m, "gone")

	require.NoError(t, m.SetPortalPassword(kept.UID, portalTestPassword))
	require.NoError(t, m.SetPortalPassword(gone.UID, portalTestPassword))

	_, err := m.ImportUsers([]*User{{
		UID:     kept.UID,
		Name:    "kept",
		IDs:     []string{"kept"},
		Enabled: true,
	}})
	require.NoError(t, err)

	assert.True(t, m.HasPortalPassword(kept.UID))
	assert.False(t, m.HasPortalPassword(gone.UID))
}

// TestPortalPasswordSurvivesAReloadWithoutIt makes sure that a state file
// written before the portal existed still loads.
func TestPortalPasswordSurvivesAReloadWithoutIt(t *testing.T) {
	m, _ := newTestManager(t)
	addPortalUser(t, m, "alice")

	require.NoError(t, m.save())

	reloaded, err := New(&Config{Logger: testLogger(), Path: m.path})
	require.NoError(t, err)

	require.Len(t, reloaded.List(), 1)
	assert.False(t, reloaded.HasPortalPassword(reloaded.List()[0].UID))
}
