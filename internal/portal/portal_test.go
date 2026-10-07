package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sort"
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
	// portalToken is the deployment token the fake store accepts.
	portalToken string

	// defs maps the user UIDs to the users.
	defs map[string]*users.User

	// passwords maps the user UIDs to their portal passwords.
	passwords map[string]string

	// settings are the AGHub-wide settings.
	settings *users.Settings

	// domain is the domain of the DoT and DoH endpoints.
	domain string

	// summary is what the store reports as the aggregate state of all
	// accounts.  A nil value stands for an empty one.
	summary *users.Summary
	// ranking is what Ranking returns.
	ranking []users.RankEntry

	// rankingOrder is the order Ranking was last called with.
	rankingOrder users.RankOrder

	// probeOwner is the owner RegisterProbe was last called with.
	probeOwner string

	// logUnlocked is what LogUnlocked reports.
	logUnlocked bool

	// feedback collects what AddFeedback stored.
	feedback []*users.Feedback

	// checkin is what CheckinStatus reports.  A nil value stands for an
	// account that has never checked in.
	checkin *users.CheckinStatus

	// checkinResult is what Checkin returns.
	checkinResult *users.CheckinResult

	// checkinErr is returned by Checkin when it is set.
	checkinErr error

	// checkinCalls counts the Checkin calls.
	checkinCalls int

	// remembers holds the remembered devices by token hash.
	remembers map[string]*testRemember
}

// testRemember is one remembered device of the fake store.
type testRemember struct {
	// uid is the account the device signs in.
	uid string

	// live is false once the token has been exchanged, which is what makes a
	// token single use.
	live bool

	// expire is when the device stops working.
	expire int64

	// userAgent and ip describe the device.
	userAgent, ip string
}

// rememberPut records a token for an account.
func (s *testUserStore) rememberPut(tok, uid, userAgent, ip string) {
	if s.remembers == nil {
		s.remembers = map[string]*testRemember{}
	}

	s.remembers[users.RememberHash(tok)] = &testRemember{
		uid:       uid,
		live:      true,
		expire:    time.Now().Add(24 * time.Hour).Unix(),
		userAgent: userAgent,
		ip:        ip,
	}
}

// rememberNext mints a token of the shape the real store produces, which the
// handlers check for before touching the store.
func (s *testUserStore) rememberNext() (tok string) {
	return fmt.Sprintf("%064x", len(s.remembers)+1)
}

// IssueRemember implements the [UserStore] interface.
func (s *testUserStore) IssueRemember(uid, userAgent, ip string) (tok string, info *users.RememberInfo, err error) {
	if s.defs[uid] == nil {
		return "", nil, fmt.Errorf("no such user %q", uid)
	}

	tok = s.rememberNext()
	s.rememberPut(tok, uid, userAgent, ip)

	return tok, s.Remembered(uid)[0], nil
}

// ExchangeRemember implements the [UserStore] interface.
func (s *testUserStore) ExchangeRemember(tok, userAgent, ip string) (
	uid, newTok string,
	info *users.RememberInfo,
	err error,
) {
	rec := s.remembers[users.RememberHash(tok)]
	if rec == nil || !rec.live {
		return "", "", nil, users.ErrRememberUnknown
	}

	rec.live = false

	newTok = s.rememberNext()
	s.rememberPut(newTok, rec.uid, userAgent, ip)

	list := s.Remembered(rec.uid)

	return rec.uid, newTok, list[0], nil
}

// ForgetRemember implements the [UserStore] interface.
func (s *testUserStore) ForgetRemember(tok string) (err error) {
	delete(s.remembers, users.RememberHash(tok))

	return nil
}

// ForgetRememberID implements the [UserStore] interface.
func (s *testUserStore) ForgetRememberID(uid, id string) (err error) {
	if rec := s.remembers[id]; rec != nil && rec.uid == uid {
		delete(s.remembers, id)
	}

	return nil
}

// ForgetRememberAll implements the [UserStore] interface.
func (s *testUserStore) ForgetRememberAll(uid, keep string) (n int) {
	for hash, rec := range s.remembers {
		if rec.uid != uid || hash == keep {
			continue
		}

		delete(s.remembers, hash)
		n++
	}

	return n
}

// Remembered implements the [UserStore] interface.
func (s *testUserStore) Remembered(uid string) (list []*users.RememberInfo) {
	for hash, rec := range s.remembers {
		if rec.uid == uid && rec.live {
			list = append(list, &users.RememberInfo{
				ID:        hash,
				UserAgent: rec.userAgent,
				IP:        rec.ip,
				Expire:    rec.expire,
			})
		}
	}

	sort.Slice(list, func(i, j int) (less bool) { return list[i].ID < list[j].ID })

	return list
}

// Summary implements the [UserStore] interface.
func (s *testUserStore) EnsurePortalToken() (tok string, err error) {
	if s.portalToken == "" {
		s.portalToken = "0123456789abcdef0123456789abcdef"
	}

	return s.portalToken, nil
}

func (s *testUserStore) CheckPortalToken(tok string) (ok bool) {
	return s.portalToken != "" && tok == s.portalToken
}

func (s *testUserStore) Ranking(limit int, order users.RankOrder) (r []users.RankEntry) {
	s.rankingOrder = order

	return s.ranking
}

func (s *testUserStore) LogUnlocked(uid string) (ok bool) {
	return s.logUnlocked
}

func (s *testUserStore) RegisterProbe(uid string) (label string) {
	s.probeOwner = uid

	return "aghub-probe-test"
}

func (s *testUserStore) ProbeStatus(uid, label string) (hit *users.ProbeHit, ok bool) {
	return nil, false
}

func (s *testUserStore) ListFeedback(limit int) (r []*users.Feedback) {
	if len(s.feedback) > limit && limit > 0 {
		return s.feedback[len(s.feedback)-limit:]
	}

	return s.feedback
}

func (s *testUserStore) ListFeedbackFor(uid string, limit int) (r []*users.Feedback) {
	for k := len(s.feedback) - 1; k >= 0 && len(r) < limit; k-- {
		f := s.feedback[k]
		if f.IsPublic() || (uid != "" && f.UID == uid) {
			r = append(r, f)
		}
	}

	return r
}

func (s *testUserStore) UpdateFeedback(id string, u *users.FeedbackUpdate) (saved *users.Feedback, err error) {
	for _, f := range s.feedback {
		if f.ID != id {
			continue
		}

		if u.Resolved != nil {
			f.Resolved = *u.Resolved
		}

		if u.Private != nil {
			f.Private = *u.Private
		}

		if u.Reply != nil {
			f.Reply = *u.Reply
		}

		f.Read = true

		return f, nil
	}

	return nil, fmt.Errorf("feedback: no message with id %q", id)
}

func (s *testUserStore) CountOpenFeedback() (n int) {
	for _, f := range s.feedback {
		if !f.Resolved {
			n++
		}
	}

	return n
}

func (s *testUserStore) DeleteFeedback(id string) (err error) {
	kept := s.feedback[:0]
	for _, f := range s.feedback {
		if f.ID != id {
			kept = append(kept, f)
		}
	}

	s.feedback = kept

	return nil
}

func (s *testUserStore) MarkFeedbackRead() (err error) {
	for _, f := range s.feedback {
		f.Read = true
	}

	return nil
}

func (s *testUserStore) CountUnreadFeedback() (n int) {
	for _, f := range s.feedback {
		if !f.Read {
			n++
		}
	}

	return n
}

func (s *testUserStore) AddFeedback(f *users.Feedback) (saved *users.Feedback, err error) {
	if f.Content == "" {
		return nil, fmt.Errorf("feedback: empty message")
	}

	f.ID = "test-feedback"
	f.CreatedAt = 1700000000
	s.feedback = append(s.feedback, f)

	return f, nil
}

func (s *testUserStore) Summary() (sum *users.Summary) {
	if s.summary == nil {
		return &users.Summary{}
	}

	return s.summary
}

// type check
var _ UserStore = (*testUserStore)(nil)

// testFiltering is a filter engine that reports a fixed status.
type testFiltering struct {
	rules   uint64
	lists   int
	custom  int
	enabled bool
}

// FilteringStatus implements the [FilteringSource] interface.
func (f testFiltering) FilteringStatus() (s FilteringStatus) {
	return FilteringStatus{
		Rules:       f.rules,
		Lists:       f.lists,
		CustomRules: f.custom,
		Enabled:     f.enabled,
	}
}

// type check
var _ FilteringSource = testFiltering{}

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

	// entries are returned as the "data" array of the response.  The type is
	// deliberately loose: the real source hands back a slice of maps, and a
	// fake that only ever produces []any hid exactly that difference.
	entries any
}

// type check
var _ LogSource = (*testLogSource)(nil)

// Search implements the [LogSource] interface.
func (s *testLogSource) Search(_ context.Context, req *LogRequest) (resp *LogResponse, err error) {
	s.last = req
	if s.err != nil {
		return nil, s.err
	}

	data := s.entries
	if data == nil {
		data = []any{}
	}

	return &LogResponse{Entries: map[string]any{"data": data, "oldest": ""}}, nil
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

// TestDeviceFromUserAgent checks that the panel names the device when the user
// agent says something useful, and says nothing rather than guessing otherwise.
func TestDeviceFromUserAgent(t *testing.T) {
	testCases := []struct {
		name string
		ua   string
		want string
	}{{
		name: "iphone",
		ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
		want: "iPhone",
	}, {
		name: "ipad",
		ua:   "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15",
		want: "iPad",
	}, {
		name: "android",
		ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/120.0",
		want: "Android 设备",
	}, {
		name: "windows",
		ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0",
		want: "Windows 设备",
	}, {
		name: "mac",
		ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15",
		want: "Mac",
	}, {
		name: "empty",
		ua:   "",
		want: "",
	}, {
		name: "unknown",
		ua:   "curl/8.5.0",
		want: "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeviceFromUserAgent(tc.ua))
		})
	}
}

// TestIsBlockedReason checks that only the filter rejections count as blocked,
// so that a rewrite is not reported as an advert that was stopped.
func TestIsBlockedReason(t *testing.T) {
	testCases := []struct {
		reason string
		want   bool
	}{
		{reason: "FilteredBlockList", want: true},
		{reason: "FilteredSafeBrowsing", want: true},
		{reason: "FilteredParental", want: true},
		{reason: "Rewritten", want: false},
		{reason: "NotFilteredNotFound", want: false},
		{reason: "", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.reason, func(t *testing.T) {
			assert.Equal(t, tc.want, isBlockedReason(tc.reason))
		})
	}
}

// TestClientInfoCountsTheLog checks that the panel counts the blocked and the
// allowed entries of the account, and that it survives a broken log instead of
// failing to render.
func TestClientInfoCountsTheLog(t *testing.T) {
	entry := func(reason string) any {
		return map[string]any{"reason": reason}
	}

	// The two shapes a log source really produces: a typed slice of maps, which
	// is what the bundled one returns, and a loose slice of any.  Counting only
	// worked for the second one at first, and the panel reported zero for every
	// real account.
	shapes := []struct {
		name string
		data any
	}{
		{
			name: "slice of maps",
			data: []map[string]any{
				{"reason": "FilteredBlockList"},
				{"reason": "NotFilteredNotFound"},
				{"reason": "FilteredSafeBrowsing"},
				{"reason": "Rewritten"},
			},
		},
		{
			name: "slice of any with a malformed entry",
			data: []any{
				entry("FilteredBlockList"),
				entry("NotFilteredNotFound"),
				entry("FilteredSafeBrowsing"),
				entry("Rewritten"),
				"not an object",
			},
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			store := newTestStore()
			u := store.defs[testUID]

			m, logSrc := newTestPortal(t, store)
			logSrc.entries = shape.data

			info := &users.Info{LastSeen: time.Now().Unix()}

			c := m.clientInfoOf(t.Context(), u, info, testIP,
				"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)")

			assert.Equal(t, "192.0.2.1", c.IP)
			assert.Equal(t, "iPhone", c.Device)
			assert.True(t, c.Connected)
			assert.Equal(t, int64(2), c.Blocked)
			assert.Equal(t, int64(2), c.Allowed)
			assert.Equal(t, int64(4), c.Scanned,
				"the malformed entry must not be counted")

			// A log that cannot be read must not take the whole panel down
			// with it.
			logSrc.err = errors.New("log is gone")

			c = m.clientInfoOf(t.Context(), u, info, testIP, "")
			assert.Zero(t, c.Scanned)
			assert.Equal(t, "192.0.2.1", c.IP)
		})
	}
}

// TestClientInfoNotConnected checks that an account that has not been used for
// a long time is reported as not connected.
func TestClientInfoNotConnected(t *testing.T) {
	store := newTestStore()
	u := store.defs[testUID]

	m, _ := newTestPortal(t, store)

	c := m.clientInfoOf(t.Context(), u, &users.Info{
		LastSeen: time.Now().Add(-24 * time.Hour).Unix(),
	}, testIP, "")

	assert.False(t, c.Connected)

	// A user that has never queried is not connected either.
	c = m.clientInfoOf(t.Context(), u, &users.Info{}, testIP, "")
	assert.False(t, c.Connected)
}

// TestEveryResponseCarriesTheClientPart checks that the responses which hand a
// user back to the panel all carry the connection part, and not only the one
// from GET /portal/api/me.
//
// The panel draws the signed-in user from the sign-in response, so building
// that one from [Manager.Info] alone left the address and the counters blank
// until the visitor happened to reload the whole page.  This is the guard for
// that: any new response that carries a user belongs here.
func TestEveryResponseCarriesTheClientPart(t *testing.T) {
	type response struct {
		User struct {
			Client *ClientInfo `json:"client"`
		} `json:"user"`
	}

	check := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()

		require.Equal(t, http.StatusOK, rec.Code)

		r := &response{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), r))

		require.NotNil(t, r.User.Client, "the response must carry the client part")
		assert.Equal(t, testIP.String(), r.User.Client.IP)
		assert.Equal(t, int64(1), r.User.Client.Blocked)
		assert.Equal(t, int64(1), r.User.Client.Allowed)
		assert.Equal(t, int64(2), r.User.Client.Scanned)
	}

	store := newTestStore()
	u := store.defs[testUID]

	m, logSrc := newTestPortal(t, store)
	logSrc.entries = []map[string]any{
		{"reason": "FilteredBlackList"},
		{"reason": "NotFilteredNotFound"},
	}

	t.Run("sign in", func(t *testing.T) {
		body := fmt.Sprintf(`{"login":%q,"password":%q}`, u.UID, testPassword)

		check(t, post(t, m.handleLogin, "/portal/api/login", body))
	})

	t.Run("session probe", func(t *testing.T) {
		c := sessionCookie(t, m, store, u.UID)

		check(t, get(t, m.handleMe, "/portal/api/me", c))
	})
}

// TestPublicStats checks the anonymous summary of the service.
func TestPublicStats(t *testing.T) {
	store := newTestStore()
	store.domain = testDomain
	store.summary = &users.Summary{
		Total:         10,
		Disabled:      2,
		Expired:       1,
		OverQuota:     1,
		ExpiringSoon:  1,
		TotalRequests: 12345,
	}

	m, _ := newTestPortal(t, store)
	m.filtering = testFiltering{rules: 1000, lists: 3, custom: 7, enabled: true}

	s := m.PublicStats()

	assert.Equal(t, int64(12345), s.Queries)
	assert.Equal(t, 10, s.Accounts)
	assert.Equal(t, 6, s.Active, "disabled, expired and over-quota accounts are not active")
	assert.Equal(t, uint64(1000), s.Rules)
	assert.Equal(t, 3, s.Lists)
	assert.Equal(t, 7, s.CustomRules)
	assert.True(t, s.Protected)
	assert.Equal(t, testDomain, s.Domain)
}

// TestPublicStatsWithoutFiltering checks that a service without a filter engine
// reports no rules rather than a made-up zero that would read as "nothing is
// blocked".
func TestPublicStatsWithoutFiltering(t *testing.T) {
	store := newTestStore()
	store.summary = &users.Summary{Total: 1, TotalRequests: 5}

	m, _ := newTestPortal(t, store)
	require.Nil(t, m.filtering)

	s := m.PublicStats()

	assert.Zero(t, s.Rules)
	assert.Zero(t, s.Lists)
	assert.Zero(t, s.CustomRules)
	assert.Equal(t, int64(5), s.Queries)
}

// TestPublicStatsHasNoAccountFields is the leak guard for the one portal
// response that anybody can read: its shape must stay the anonymous summary.
func TestPublicStatsHasNoAccountFields(t *testing.T) {
	store := newTestStore()
	store.summary = &users.Summary{Total: 1, TotalRequests: 5}

	m, _ := newTestPortal(t, store)

	rec := get(t, m.handlePublic, "/portal/api/public")
	require.Equal(t, http.StatusOK, rec.Code)

	got := map[string]any{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	want := []string{
		"accounts", "active", "blocked", "blocked_24h", "custom_rules", "domain",
		"lists", "passed", "passed_24h", "protected", "queries", "queries_24h",
		"rules",
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	assert.Equal(t, want, keys)
}
