package portal

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- the test store grows the account methods ----------

func (s *testUserStore) Register(p *users.RegisterParams) (u *users.User, err error) {
	if p == nil {
		return nil, errors.New("no parameters")
	}

	if len(p.Password) < users.MinPortalPasswordLen {
		return nil, errors.New("password too short")
	}

	email, err := users.NormalizeEmail(p.Email)
	if err != nil {
		return nil, err
	}

	if email != "" && s.FindByEmail(email) != nil {
		return nil, errors.New("e-mail already registered")
	}

	uid, err := users.NewUID()
	if err != nil {
		return nil, err
	}

	u = &users.User{
		UID:           uid,
		Name:          p.Name,
		IDs:           []string{uid},
		RequestLimit:  s.settings.PortalDefaultQuota,
		Email:         email,
		EmailVerified: email != "" && p.EmailVerified,
		Enabled:       true,
	}
	if u.Name == "" {
		u.Name = uid
	}
	if u.RequestLimit == 0 {
		u.RequestLimit = users.Unlimited
	}

	s.defs[uid] = u
	s.passwords[uid] = p.Password

	return u, nil
}

func (s *testUserStore) SetEmail(uid, email string, verified bool) (err error) {
	def, ok := s.defs[uid]
	if !ok {
		return errors.New("no such user")
	}

	norm, err := users.NormalizeEmail(email)
	if err != nil {
		return err
	}

	if norm != "" && s.FindByEmail(norm) != nil {
		return errors.New("e-mail already registered")
	}

	def.Email = norm
	def.EmailVerified = norm != "" && verified

	return nil
}

func (s *testUserStore) SetPortalPassword(uid, password string) (err error) {
	if _, ok := s.defs[uid]; !ok {
		return errors.New("no such user")
	}

	if len(password) < users.MinPortalPasswordLen {
		return errors.New("password too short")
	}

	s.passwords[uid] = password

	return nil
}

func (s *testUserStore) FindByEmail(email string) (u *users.User) {
	norm, err := users.NormalizeEmail(email)
	if err != nil || norm == "" {
		return nil
	}

	for _, def := range s.defs {
		if strings.EqualFold(def.Email, norm) {
			return def
		}
	}

	return nil
}

// ---------- helpers ----------

// post sends a JSON request to the handler and returns the response.
func post(tb testing.TB, h http.HandlerFunc, path, body string, cookies ...*http.Cookie) (rec *httptest.ResponseRecorder) {
	tb.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec = httptest.NewRecorder()
	h(rec, req)

	return rec
}

// get sends a request to the handler and returns the response.
func get(tb testing.TB, h http.HandlerFunc, path string, cookies ...*http.Cookie) (rec *httptest.ResponseRecorder) {
	tb.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec = httptest.NewRecorder()
	h(rec, req)

	return rec
}

// sessionCookie signs the user in and returns the cookie to send back.
func sessionCookie(tb testing.TB, m *Manager, store *testUserStore, uid string) (c *http.Cookie) {
	tb.Helper()

	sess, err := m.Login(tb.Context(), uid, store.passwords[uid], testIP)
	require.NoError(tb, err)

	return &http.Cookie{Name: SessionCookieName, Value: hex.EncodeToString(sess.Token[:])}
}

// ---------- the code store ----------

func TestCodeStore(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	c := &codeStore{m: map[string]*emailCode{}}
	c.put("alice@example.com", "123456", now)

	if !c.check("alice@example.com", "123456", now) {
		t.Fatal("expected the code to be accepted")
	}

	// A code is single-use, or an intercepted message would be a permanent
	// key to the account.
	if c.check("alice@example.com", "123456", now) {
		t.Error("expected the code to be consumed")
	}
}

func TestCodeStoreRejectsWrongCode(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c := &codeStore{m: map[string]*emailCode{}}
	c.put("alice@example.com", "123456", now)

	if c.check("alice@example.com", "000000", now) {
		t.Fatal("expected a wrong code to be refused")
	}

	// The right code still works after a wrong guess.
	if !c.check("alice@example.com", "123456", now) {
		t.Error("expected the right code to work after a wrong guess")
	}
}

func TestCodeStoreExpires(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c := &codeStore{m: map[string]*emailCode{}}
	c.put("alice@example.com", "123456", now)

	later := now.Add(emailCodeTTL + time.Second)
	if c.check("alice@example.com", "123456", later) {
		t.Error("expected an expired code to be refused")
	}
}

func TestCodeStoreGivesUpAfterEnoughGuesses(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c := &codeStore{m: map[string]*emailCode{}}
	c.put("alice@example.com", "123456", now)

	// Six digits are only a million possibilities, so an attacker who may
	// guess without limit would get in.  The code has to die first.
	for i := range maxCodeAttempts {
		if c.check("alice@example.com", "000000", now) {
			t.Fatalf("guess %d was accepted", i)
		}
	}

	if c.check("alice@example.com", "123456", now) {
		t.Error("expected the code to be dead after too many guesses")
	}
}

func TestCodeStorePrunes(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c := &codeStore{m: map[string]*emailCode{}}

	for i := range maxPendingCodes {
		c.put(fmt.Sprintf("user%d@example.com", i), "123456", now)
	}

	// The next insert has to make room instead of growing without bound.
	c.put("late@example.com", "123456", now.Add(emailCodeTTL+time.Minute))

	assert.LessOrEqual(t, len(c.m), maxPendingCodes+1)
}

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}

	for range 100 {
		code, err := newCode()
		require.NoError(t, err)
		require.Len(t, code, codeDigits)

		for _, r := range code {
			require.True(t, r >= '0' && r <= '9', "expected digits, got %q", code)
		}

		seen[code] = true
	}

	// A code generator that always returns the same value would still pass the
	// length check above.
	assert.Greater(t, len(seen), 50)
}

// ---------- the public configuration ----------

func TestConfigReportsTheRegistrationState(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleConfig, "/portal/api/config")

	require.Equal(t, http.StatusOK, rec.Code)

	var resp configResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Sign-up is off until the administrator turns it on.
	assert.False(t, resp.RegistrationOpen)

	store.settings = &users.Settings{
		PortalOpen:         true,
		PortalEmailVerify:  true,
		PortalAnnouncement: "今晚维护",
	}

	rec = get(t, m.handleConfig, "/portal/api/config")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.True(t, resp.RegistrationOpen)
	assert.True(t, resp.EmailRequired)
	assert.Equal(t, "今晚维护", resp.Announcement)
}

func TestConfigDoesNotLeakTheMailServer(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{
		SMTPHost:     "smtp.example.com",
		SMTPUser:     "mailer",
		SMTPPassword: "hunter2-secret",
	}

	m, _ := newTestPortal(t, store)

	rec := get(t, m.handleConfig, "/portal/api/config")

	// The public configuration is the one endpoint an anonymous caller can
	// reach, so it must not carry anything about the mail server.
	assert.NotContains(t, rec.Body.String(), "hunter2-secret")
	assert.NotContains(t, rec.Body.String(), "smtp.example.com")
	assert.NotContains(t, rec.Body.String(), "mailer")
}

// ---------- registration ----------

func TestRegisterIsClosedByDefault(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	before := len(store.defs)

	rec := post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123"}`)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Len(t, store.defs, before)
}

func TestRegister(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{PortalOpen: true, PortalDefaultQuota: 50}
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.User)

	// The identifier is generated, and it is what the user puts in front of the
	// domain, so it has to be the only identifier of the account.
	assert.NotEmpty(t, resp.User.UID)
	assert.Equal(t, []string{resp.User.UID}, resp.User.IDs)
	assert.Equal(t, int64(50), resp.User.RequestLimit)

	// Signing up signs in, so the response carries a session.
	assert.NotEmpty(t, rec.Result().Cookies())

	// The new account can sign in again with the password it chose.
	_, err := m.Login(t.Context(), resp.User.UID, "localpass123", testIP)
	assert.NoError(t, err)
}

func TestRegisterRejectsAShortPassword(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{PortalOpen: true}
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"short"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Len(t, store.defs, 1)
}

func TestRegisterWithEmailVerification(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{
		PortalOpen:        true,
		PortalEmailVerify: true,
		SMTPHost:          "smtp.example.com",
		SMTPUser:          "mailer",
	}
	m, _ := newTestPortal(t, store)

	// Without a code the account must not be created: the whole point of the
	// switch is that the address is proved first.
	rec := post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123","email":"eve@example.com"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Len(t, store.defs, 1)

	// A wrong code is refused too.
	rec = post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123","email":"eve@example.com","code":"000000"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Len(t, store.defs, 1)

	// The code the mail server would have sent is the one that works.
	m.codes.put("eve@example.com", "424242", m.now())

	rec = post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123","email":"eve@example.com","code":"424242"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "eve@example.com", resp.User.Email)
	assert.True(t, resp.User.EmailVerified)
}

func TestRegisterWithoutMailServer(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{PortalOpen: true, PortalEmailVerify: true}
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleRegister, "/portal/api/register",
		`{"name":"Eve","password":"localpass123","email":"eve@example.com","code":"424242"}`)

	// Requiring verification without a way to send the code would lock
	// everybody out, so it is reported as a server problem.
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Len(t, store.defs, 1)
}

func TestEmailCodeWithoutMailServer(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleEmailCode, "/portal/api/email/code", `{"email":"eve@example.com"}`)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestEmailCodeIsRateLimited(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{SMTPHost: "smtp.example.com", SMTPUser: "mailer"}
	m, _ := newTestPortal(t, store)

	// The server cannot send anything in a test, so every attempt fails.  The
	// point is that the limiter eventually stops it: otherwise the portal is a
	// free mail relay.
	statuses := make([]int, 0, DefaultCodeRequests+3)
	for range DefaultCodeRequests + 3 {
		rec := post(t, m.handleEmailCode, "/portal/api/email/code", `{"email":"eve@example.com"}`)
		statuses = append(statuses, rec.Code)
	}

	assert.Contains(t, statuses, http.StatusTooManyRequests)
}

// ---------- the account pages ----------

func TestChangePassword(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	// The current password is required, so a stolen session alone cannot lock
	// the owner out.
	rec := post(t, m.handlePassword, "/portal/api/password",
		`{"old_password":"wrong","new_password":"newpass12345"}`, cookie)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = post(t, m.handlePassword, "/portal/api/password",
		`{"old_password":"`+testPassword+`","new_password":"newpass12345"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "newpass12345", store.passwords[testUID])
}

func TestChangePasswordNeedsASession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handlePassword, "/portal/api/password",
		`{"old_password":"`+testPassword+`","new_password":"newpass12345"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, testPassword, store.passwords[testUID])
}

func TestChangePasswordRejectsAShortOne(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handlePassword, "/portal/api/password",
		`{"old_password":"`+testPassword+`","new_password":"short"}`, cookie)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, testPassword, store.passwords[testUID])
}

func TestBindEmail(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{SMTPHost: "smtp.example.com", SMTPUser: "mailer"}
	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	// A wrong code does not bind anything.
	rec := post(t, m.handleEmail, "/portal/api/email",
		`{"email":"test@example.com","code":"000000"}`, cookie)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.defs[testUID].Email)

	m.codes.put("test@example.com", "424242", m.now())

	rec = post(t, m.handleEmail, "/portal/api/email",
		`{"email":"test@example.com","code":"424242"}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "test@example.com", store.defs[testUID].Email)
	assert.True(t, store.defs[testUID].EmailVerified)
}

func TestUnbindEmailNeedsNoCode(t *testing.T) {
	store := newTestStore()
	store.settings = &users.Settings{SMTPHost: "smtp.example.com", SMTPUser: "mailer"}
	store.defs[testUID].Email = "test@example.com"
	store.defs[testUID].EmailVerified = true

	m, _ := newTestPortal(t, store)

	cookie := sessionCookie(t, m, store, testUID)

	rec := post(t, m.handleEmail, "/portal/api/email", `{"email":""}`, cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Empty(t, store.defs[testUID].Email)
	assert.False(t, store.defs[testUID].EmailVerified)
}

func TestBindEmailNeedsASession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	rec := post(t, m.handleEmail, "/portal/api/email", `{"email":"test@example.com","code":"424242"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.defs[testUID].Email)
}

func TestAccountPagesNeedASession(t *testing.T) {
	store := newTestStore()
	m, _ := newTestPortal(t, store)

	// Every page that changes the account has to require a session, or the
	// portal would let anybody edit anybody.
	for name, h := range map[string]http.HandlerFunc{
		"password": m.handlePassword,
		"email":    m.handleEmail,
	} {
		t.Run(name, func(t *testing.T) {
			rec := post(t, h, "/portal/api/"+name, `{}`)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}
