package portal

import (
	"context"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghuser"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/timeutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// testPassword is the portal password of the test users.
	testPassword = "correct horse battery staple"

	// testUID is the UID of the test user.
	testUID = "uid-1"

	// testDomain is the domain of the DoT and DoH endpoints.
	testDomain = "adguardhome.example"
)

// testLogger is the logger for the tests.
var testLogger = slogutil.NewDiscardLogger()

// testUserStore is a fake [UserStore].
type testUserStore struct {
	// defs maps the user UIDs to the users.
	defs map[string]*users.User

	// passwords maps the user UIDs to their portal passwords.
	passwords map[string]string

	// settings are the AGHub-wide settings.
	settings *users.Settings

	// domain is the domain of the DoT and DoH endpoints.
	domain string
}

// type check
var _ UserStore = (*testUserStore)(nil)

// FindByLogin implements the [UserStore] interface.  It mirrors the real
// lookup: identifiers first, then the e-mail address, then the name.
func (s *testUserStore) FindByLogin(login string) (u *users.User) {
	login = strings.ToLower(strings.TrimSpace(login))

	var byName []*users.User

	for _, def := range s.defs {
		if def.UID == login {
			return def
		}

		for _, id := range def.IDs {
			if strings.ToLower(id) == login {
				return def
			}
		}

		if def.Email != "" && strings.ToLower(def.Email) == login {
			return def
		}

		if strings.ToLower(def.Name) == login {
			byName = append(byName, def)
		}
	}

	if len(byName) == 1 {
		return byName[0]
	}

	return nil
}

// AuthenticatePortal implements the [UserStore] interface.
func (s *testUserStore) AuthenticatePortal(uid, password string) (ok bool) {
	want, found := s.passwords[uid]

	return found && want == password
}

// HasPortalPassword implements the [UserStore] interface.
func (s *testUserStore) HasPortalPassword(uid string) (ok bool) {
	_, found := s.passwords[uid]

	return found
}

// Get implements the [UserStore] interface.
func (s *testUserStore) Get(uid string) (u *users.User) {
	return s.defs[uid]
}

// InfoOf implements the [UserStore] interface.
func (s *testUserStore) InfoOf(uid string) (i *users.Info) {
	u := s.defs[uid]
	if u == nil {
		return nil
	}

	return &users.Info{
		User:          u,
		Requests:      42,
		TotalRequests: 4200,
	}
}

// Domain implements the [UserStore] interface.
func (s *testUserStore) Domain() (domain string) {
	return s.domain
}

// GetSettings implements the [UserStore] interface.
func (s *testUserStore) GetSettings() (set *users.Settings) {
	return s.settings
}

// testLogSource is a fake [LogSource].
type testLogSource struct {
	// last is the request of the last search.
	last *LogRequest

	// err is returned by the search.
	err error
}

// type check
var _ LogSource = (*testLogSource)(nil)

// Search implements the [LogSource] interface.
func (s *testLogSource) Search(_ context.Context, req *LogRequest) (resp *LogResponse, err error) {
	s.last = req
	if s.err != nil {
		return nil, s.err
	}

	return &LogResponse{Entries: map[string]any{"data": []any{}, "oldest": ""}}, nil
}

// newTestPortal creates a portal with a real session storage on a temporary
// database, so that the sessions behave exactly as in production.
func newTestPortal(tb testing.TB, store *testUserStore) (m *Manager, log *testLogSource) {
	tb.Helper()

	ctx := context.Background()
	userDB := aghuser.NewDefaultDB()
	for uid := range store.defs {
		err := userDB.Create(ctx, SessionUser(uid))
		require.NoError(tb, err)
	}

	sessions, err := aghuser.NewDefaultSessionStorage(ctx, &aghuser.DefaultSessionStorageConfig{
		Logger:     testLogger,
		Clock:      timeutil.SystemClock{},
		UserDB:     userDB,
		DBPath:     filepath.Join(tb.TempDir(), "portal_sessions.db"),
		SessionTTL: DefaultSessionTTL,
	})
	require.NoError(tb, err)

	log = &testLogSource{}

	m, err = New(&Config{
		Logger:   testLogger,
		Users:    store,
		Sessions: sessions,
		Log:      log,
	})
	require.NoError(tb, err)

	return m, log
}

// newTestStore returns a store with a single user that has the portal password
// set.
func newTestStore() (s *testUserStore) {
	return &testUserStore{
		defs: map[string]*users.User{
			testUID: {
				UID:  testUID,
				Name: "Test User",
				IDs:  []string{"testuser"},
			},
		},
		passwords: map[string]string{testUID: testPassword},
		settings:  &users.Settings{},
		domain:    testDomain,
	}
}

// testIP is the address the sign-in attempts come from.
var testIP = netip.MustParseAddr("192.0.2.1")

func TestNew(t *testing.T) {
	store := newTestStore()

	t.Run("success", func(t *testing.T) {
		_, _ = newTestPortal(t, store)
	})

	t.Run("nil_config", func(t *testing.T) {
		m, err := New(nil)
		require.Error(t, err)
		assert.Nil(t, m)
	})

	t.Run("no_log", func(t *testing.T) {
		m, err := New(&Config{Logger: testLogger, Users: store, Sessions: nopSessions{}})
		require.Error(t, err)
		assert.Nil(t, m)
	})
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		m, _ := newTestPortal(t, newTestStore())

		s, err := m.Login(ctx, "testuser", testPassword, testIP)
		require.NoError(t, err)
		require.NotNil(t, s)
		assert.Equal(t, testUID, s.User.UID)
		assert.True(t, s.Expire.After(time.Now()))

		u := m.Authenticate(ctx, s.Token)
		require.NotNil(t, u)
		assert.Equal(t, testUID, u.UID)
	})

	t.Run("by_name", func(t *testing.T) {
		m, _ := newTestPortal(t, newTestStore())

		s, err := m.Login(ctx, "Test User", testPassword, testIP)
		require.NoError(t, err)
		require.NotNil(t, s)
	})

	t.Run("wrong_password", func(t *testing.T) {
		m, _ := newTestPortal(t, newTestStore())

		s, err := m.Login(ctx, "testuser", "nope", testIP)
		require.ErrorIs(t, err, ErrInvalidLogin)
		assert.Nil(t, s)
	})

	t.Run("unknown_user", func(t *testing.T) {
		m, _ := newTestPortal(t, newTestStore())

		s, err := m.Login(ctx, "nobody", testPassword, testIP)
		require.ErrorIs(t, err, ErrInvalidLogin)
		assert.Nil(t, s)
	})

	t.Run("no_portal_password", func(t *testing.T) {
		store := newTestStore()
		delete(store.passwords, testUID)

		m, _ := newTestPortal(t, store)

		s, err := m.Login(ctx, "testuser", testPassword, testIP)
		require.ErrorIs(t, err, ErrInvalidLogin)
		assert.Nil(t, s)
	})
}

func TestLoginRateLimit(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	m, _ := newTestPortal(t, store)

	// Exhaust the attempts.
	for i := range DefaultLoginAttempts {
		_, err := m.Login(ctx, "testuser", "nope", testIP)
		require.ErrorIs(t, err, ErrInvalidLogin, "attempt %d", i)
	}

	// The correct password must not help anymore.
	s, err := m.Login(ctx, "testuser", testPassword, testIP)
	require.ErrorIs(t, err, ErrTooManyAttempts)
	assert.Nil(t, s)

	// Another address is unaffected.
	other := netip.MustParseAddr("192.0.2.2")
	s, err = m.Login(ctx, "testuser", testPassword, other)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestLoginResetsRateLimit(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	m, _ := newTestPortal(t, store)

	for range DefaultLoginAttempts - 1 {
		_, err := m.Login(ctx, "testuser", "nope", testIP)
		require.ErrorIs(t, err, ErrInvalidLogin)
	}

	_, err := m.Login(ctx, "testuser", testPassword, testIP)
	require.NoError(t, err)

	// The counter is back to zero, so a fresh run of failures is needed.
	for range DefaultLoginAttempts - 1 {
		_, err = m.Login(ctx, "testuser", "nope", testIP)
		require.ErrorIs(t, err, ErrInvalidLogin)
	}

	_, err = m.Login(ctx, "testuser", testPassword, testIP)
	require.NoError(t, err)
}

func TestLogout(t *testing.T) {
	ctx := context.Background()

	m, _ := newTestPortal(t, newTestStore())

	s, err := m.Login(ctx, "testuser", testPassword, testIP)
	require.NoError(t, err)

	err = m.Logout(ctx, s.Token)
	require.NoError(t, err)

	assert.Nil(t, m.Authenticate(ctx, s.Token))
}

func TestAuthenticateRevoked(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	m, _ := newTestPortal(t, store)

	s, err := m.Login(ctx, "testuser", testPassword, testIP)
	require.NoError(t, err)
	require.NotNil(t, m.Authenticate(ctx, s.Token))

	// Clearing the portal password has to invalidate the existing sessions at
	// once, not when they expire.
	delete(store.passwords, testUID)

	assert.Nil(t, m.Authenticate(ctx, s.Token))
}

func TestAuthenticateDeletedUser(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	m, _ := newTestPortal(t, store)

	s, err := m.Login(ctx, "testuser", testPassword, testIP)
	require.NoError(t, err)

	delete(store.defs, testUID)

	assert.Nil(t, m.Authenticate(ctx, s.Token))
}

func TestAuthenticateUnknownToken(t *testing.T) {
	m, _ := newTestPortal(t, newTestStore())

	assert.Nil(t, m.Authenticate(context.Background(), aghuser.SessionToken{}))
}

func TestInfo(t *testing.T) {
	m, _ := newTestPortal(t, newTestStore())

	u := m.users.Get(testUID)
	require.NotNil(t, u)

	info := m.Info(u)
	require.NotNil(t, info)
	assert.Equal(t, testUID, info.UID)
	assert.Equal(t, testDomain, info.Domain)
	assert.Equal(t, []string{"testuser." + testDomain}, info.Hosts)
	assert.EqualValues(t, 42, info.Requests)
}

func TestSearchLog(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	store.defs[testUID].IDs = []string{"testuser", "192.0.2.0/24", "198.51.100.7"}

	m, log := newTestPortal(t, store)

	u := m.users.Get(testUID)
	require.NotNil(t, u)

	_, err := m.SearchLog(ctx, u, &LogRequest{Term: "example.com"})
	require.NoError(t, err)

	req := log.last
	require.NotNil(t, req)
	assert.Equal(t, []string{"testuser"}, req.ClientIDs)
	assert.Equal(t, "example.com", req.Term)
	assert.Equal(t, DefaultLogLimit, req.Limit)

	wantNets := []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.7/32"),
	}
	assert.Equal(t, wantNets, req.ClientNets)
}

func TestSearchLogLimits(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	m, log := newTestPortal(t, store)

	u := m.users.Get(testUID)
	require.NotNil(t, u)

	testCases := []struct {
		name string
		in   int
		want int
	}{{
		name: "default",
		in:   0,
		want: DefaultLogLimit,
	}, {
		name: "negative",
		in:   -1,
		want: DefaultLogLimit,
	}, {
		name: "custom",
		in:   25,
		want: 25,
	}, {
		name: "clamped",
		in:   maxLogLimit + 1,
		want: maxLogLimit,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.SearchLog(ctx, u, &LogRequest{Limit: tc.in})
			require.NoError(t, err)
			assert.Equal(t, tc.want, log.last.Limit)
		})
	}
}

func TestSearchLogWithoutIdentifiers(t *testing.T) {
	ctx := context.Background()

	store := newTestStore()
	store.defs[testUID].IDs = nil

	m, log := newTestPortal(t, store)

	u := m.users.Get(testUID)
	require.NotNil(t, u)

	resp, err := m.SearchLog(ctx, u, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// A user that is identified by nothing owns no entry, so the log must not
	// even be searched.
	assert.Nil(t, log.last)
	assert.Empty(t, resp.Entries["data"])
}

func TestSplitIDs(t *testing.T) {
	testCases := []struct {
		name        string
		in          []string
		wantIDs     []string
		wantNetsLen int
	}{{
		name:        "hostnames",
		in:          []string{"alice", "bob"},
		wantIDs:     []string{"alice", "bob"},
		wantNetsLen: 0,
	}, {
		name:        "address",
		in:          []string{"192.0.2.1"},
		wantIDs:     nil,
		wantNetsLen: 1,
	}, {
		name:        "prefix",
		in:          []string{"192.0.2.0/24"},
		wantIDs:     nil,
		wantNetsLen: 1,
	}, {
		name:        "mixed",
		in:          []string{"alice", "192.0.2.0/24", "", "  ", "2001:db8::1"},
		wantIDs:     []string{"alice"},
		wantNetsLen: 2,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ids, nets := splitIDs(tc.in)
			assert.Equal(t, tc.wantIDs, ids)
			assert.Len(t, nets, tc.wantNetsLen)
		})
	}
}

func TestHostsOf(t *testing.T) {
	assert.Equal(
		t,
		[]string{"alice." + testDomain},
		hostsOf([]string{"alice", "192.0.2.1"}, testDomain),
	)
	assert.Empty(t, hostsOf([]string{"alice"}, ""))
}

func TestNormalizeOrigins(t *testing.T) {
	testCases := []struct {
		name string
		in   []string
		want []string
	}{{
		name: "urls",
		in:   []string{"https://portal.example.com/", "http://other.example:8080/x"},
		want: []string{"https://portal.example.com", "http://other.example:8080"},
	}, {
		name: "bare_host",
		in:   []string{"portal.example.com", " portal.example.com:3000 "},
		want: []string{"portal.example.com", "portal.example.com:3000"},
	}, {
		name: "junk",
		in:   []string{"", "   ", "https://"},
		want: nil,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeOrigins(tc.in))
		})
	}
}

func TestLoginLimiterWindow(t *testing.T) {
	now := time.Now()
	clock := &now

	l := newLoginLimiter(2, time.Minute, func() (t time.Time) { return *clock })

	assert.True(t, l.allow(testIP))

	l.fail(testIP)
	l.fail(testIP)
	assert.False(t, l.allow(testIP))

	*clock = now.Add(time.Minute + time.Second)
	assert.True(t, l.allow(testIP))
}

// nopSessions is a [aghuser.SessionStorage] that does nothing.  It is only
// used to reach the configuration checks in [New].
type nopSessions struct{}

// type check
var _ aghuser.SessionStorage = nopSessions{}

// New implements the [aghuser.SessionStorage] interface.
func (nopSessions) New(context.Context, *aghuser.User) (s *aghuser.Session, err error) {
	panic("not implemented")
}

// FindByToken implements the [aghuser.SessionStorage] interface.
func (nopSessions) FindByToken(context.Context, aghuser.SessionToken) (s *aghuser.Session, err error) {
	panic("not implemented")
}

// DeleteByToken implements the [aghuser.SessionStorage] interface.
func (nopSessions) DeleteByToken(context.Context, aghuser.SessionToken) (err error) {
	panic("not implemented")
}

// Close implements the [aghuser.SessionStorage] interface.
func (nopSessions) Close() (err error) {
	panic("not implemented")
}
