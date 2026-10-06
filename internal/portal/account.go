package portal

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
)

const (
	// emailCodeTTL is how long a verification code stays valid.
	emailCodeTTL = 15 * time.Minute

	// maxCodeAttempts is the number of wrong guesses a code survives before it
	// is thrown away.  A six-digit code would otherwise be guessable by brute
	// force within its lifetime.
	maxCodeAttempts = 5

	// maxPendingCodes bounds the memory a flood of requests can occupy.
	maxPendingCodes = 4096

	// codeDigits is the number of digits in a verification code.
	codeDigits = 6
)

// Errors returned by the account API.
var (
	// ErrRegistrationClosed is returned when sign-up is switched off.
	ErrRegistrationClosed = errors.New("registration is closed")

	// ErrMailNotConfigured is returned when a verification code is requested
	// but no mail server is set up.
	ErrMailNotConfigured = errors.New("the mail server is not configured")

	// ErrInvalidCode is returned when a verification code is wrong or expired.
	ErrInvalidCode = errors.New("the verification code is invalid or has expired")

	// ErrInvalidEmail is returned when the address cannot be used.
	ErrInvalidEmail = errors.New("the e-mail address is invalid")
)

// emailCode is a pending verification of an e-mail address.
type emailCode struct {
	// code is the digits the user has to send back.
	code string

	// expires is the moment the code stops being accepted.
	expires time.Time

	// attempts is the number of wrong guesses made so far.
	attempts int
}

// codeStore holds the pending e-mail verifications, keyed by the canonical
// address.
type codeStore struct {
	mu sync.Mutex
	m  map[string]*emailCode
}

// put stores a code for the address, replacing any previous one.
func (c *codeStore) put(email, code string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.m) >= maxPendingCodes {
		c.pruneLocked(now)
	}

	c.m[email] = &emailCode{code: code, expires: now.Add(emailCodeTTL)}
}

// check reports whether the code matches, and consumes it on success.  It
// counts the attempt either way, so a wrong guess cannot be repeated forever.
func (c *codeStore) check(email, code string, now time.Time) (ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, has := c.m[email]
	if !has {
		return false
	}

	if now.After(entry.expires) || entry.attempts >= maxCodeAttempts {
		delete(c.m, email)

		return false
	}

	entry.attempts++

	// The comparison is constant-time so that the response time does not tell
	// an attacker how much of the code they got right.
	if subtle.ConstantTimeCompare([]byte(entry.code), []byte(code)) != 1 {
		return false
	}

	delete(c.m, email)

	return true
}

// drop removes the code of the address.
func (c *codeStore) drop(email string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.m, email)
}

// pruneLocked removes the expired entries.
func (c *codeStore) pruneLocked(now time.Time) {
	for email, entry := range c.m {
		if now.After(entry.expires) {
			delete(c.m, email)
		}
	}
}

// newCode returns a random verification code.
func newCode() (code string, err error) {
	limit := big.NewInt(1)
	for range codeDigits {
		limit.Mul(limit, big.NewInt(10))
	}

	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("generating a code: %w", err)
	}

	return fmt.Sprintf("%0*d", codeDigits, n), nil
}

// configResponse is the response of the GET /portal/api/config HTTP API.  It is
// public: the sign-in page needs to know whether to offer sign-up before
// anybody has an account.
type configResponse struct {
	// RegistrationOpen is true when anyone may create an account.
	RegistrationOpen bool `json:"registration_open"`

	// EmailRequired is true when signing up needs a verified address.
	EmailRequired bool `json:"email_required"`

	// Announcement is the message the administrator wants the users to see.
	Announcement string `json:"announcement,omitempty"`
}

// handleConfig is the handler for the GET /portal/api/config HTTP API.
func (m *Manager) handleConfig(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	s := m.users.GetSettings()

	aghhttp.WriteJSONResponseOK(r.Context(), m.logger, w, r, &configResponse{
		RegistrationOpen: s.PortalOpen,
		EmailRequired:    s.PortalEmailVerify,
		Announcement:     s.PortalAnnouncement,
	})
}

// emailCodeRequest is the request of the POST /portal/api/email/code HTTP API.
type emailCodeRequest struct {
	// Email is the address to verify.
	Email string `json:"email"`
}

// handleEmailCode is the handler for the POST /portal/api/email/code HTTP API.
// It is public: signing up needs a code before an account exists.
func (m *Manager) handleEmailCode(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()
	l := m.logger

	if !m.codeLimiter.allow(clientIP(r)) {
		w.Header().Set("Retry-After", strconv.Itoa(int(m.codeLimiter.window.Seconds())))
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusTooManyRequests, "too many requests")

		return
	}

	req := &emailCodeRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	err = m.SendEmailCode(req.Email)
	switch {
	case errors.Is(err, ErrMailNotConfigured):
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusServiceUnavailable, "%s", err)
	case errors.Is(err, ErrInvalidEmail):
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)
	case err != nil:
		m.codeLimiter.fail(clientIP(r))
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadGateway, "sending the code: %s", err)
	default:
		m.codeLimiter.succeed(clientIP(r))
		aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
	}
}

// SendEmailCode sends a verification code to the address.
func (m *Manager) SendEmailCode(email string) (err error) {
	s := m.users.GetSettings()

	conf := s.MailConfig()
	if conf == nil {
		return ErrMailNotConfigured
	}

	norm, err := users.NormalizeEmail(email)
	if err != nil || norm == "" {
		return ErrInvalidEmail
	}

	code, err := newCode()
	if err != nil {
		return err
	}

	now := m.now()
	m.codes.put(norm, code, now)

	body := fmt.Sprintf(
		"你的验证码是：%s\n\n"+
			"请在 %d 分钟内使用，超时需要重新获取。\n"+
			"如果这不是你本人的操作，忽略这封邮件即可。\n",
		code,
		int(emailCodeTTL.Minutes()),
	)

	err = users.SendMail(conf, norm, "AGHub 邮箱验证码", body)
	if err != nil {
		// A code that was never delivered must not stay usable.
		m.codes.drop(norm)

		return err
	}

	return nil
}

// okResponse is an empty successful response.
type okResponse struct{}

// registerRequest is the request of the POST /portal/api/register HTTP API.
type registerRequest struct {
	// Name is the optional display name.
	Name string `json:"name"`

	// Password is the password of the new account.
	Password string `json:"password"`

	// Email is the address of the new account.
	Email string `json:"email"`

	// Code is the verification code for the address.
	Code string `json:"code"`
}

// handleRegister is the handler for the POST /portal/api/register HTTP API.
//
// The account and the DNS identifier are the same thing: the identifier is
// generated here, and nothing in the API can change it afterwards.
func (m *Manager) handleRegister(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()
	l := m.logger

	s := m.users.GetSettings()
	if !s.PortalOpen {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusForbidden, "%s", ErrRegistrationClosed)

		return
	}

	req := &registerRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	// The name and the address are what identify the account to a human, so
	// they are required whatever the verification switch says.  The switch
	// only decides whether the address has to be *proved*.
	if strings.TrimSpace(req.Name) == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "name is empty")

		return
	}

	email, err := users.NormalizeEmail(req.Email)
	if err != nil || email == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", ErrInvalidEmail)

		return
	}

	verified := false
	if s.PortalEmailVerify {
		if !s.MailConfig().IsConfigured() {
			aghhttp.ErrorAndLog(
				ctx, l, r, w, http.StatusServiceUnavailable, "%s", ErrMailNotConfigured,
			)

			return
		}

		err = m.checkEmailCode(req.Email, req.Code)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

			return
		}

		verified = true
	}

	u, err := m.users.Register(&users.RegisterParams{
		Name:          req.Name,
		Password:      req.Password,
		Email:         email,
		EmailVerified: verified,
	})
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "portal account created", "uid", u.UID)

	// Signing up signs in, so that the user does not have to type the password
	// they just chose a second time.
	sess, err := m.Login(ctx, u.UID, req.Password, clientIP(r))
	if err != nil {
		// The account exists and works; only the automatic sign-in failed.
		l.ErrorContext(ctx, "signing in after registration", "err", err)

		aghhttp.WriteJSONResponseOK(ctx, l, w, r, &loginResponse{User: m.InfoForRequest(ctx, r, u)})

		return
	}

	m.setSessionCookie(w, r, sess)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &loginResponse{User: m.InfoForRequest(ctx, r, u)})
}

// checkEmailCode reports whether the code matches the address.
func (m *Manager) checkEmailCode(email, code string) (err error) {
	norm, err := users.NormalizeEmail(email)
	if err != nil || norm == "" {
		return ErrInvalidEmail
	}

	if !m.codes.check(norm, strings.TrimSpace(code), m.now()) {
		return ErrInvalidCode
	}

	return nil
}

// passwordRequest is the request of the POST /portal/api/password HTTP API.
type passwordRequest struct {
	// OldPassword is the password in use.
	OldPassword string `json:"old_password"`

	// NewPassword is the password to set.
	NewPassword string `json:"new_password"`

	// RememberToken is the remembered device making the change, if any.  It is
	// spared when the other devices are signed out: the visitor changing their
	// password should not lose their own sign-in, while every other device
	// must, because a token that outlived the password is a way back in.
	RememberToken string `json:"remember_token"`
}

// handlePassword is the handler for the POST /portal/api/password HTTP API.
func (m *Manager) handlePassword(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()
	l := m.logger

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	req := &passwordRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	// The current password is required, so that a stolen session alone cannot
	// lock the owner out of their own account.
	if !m.users.AuthenticatePortal(u.UID, req.OldPassword) {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusUnauthorized, "the current password is wrong")

		return
	}

	// Changing the password signs every other device out.  The one making the
	// change is spared, because it just proved it knows the old password.
	keep := ""
	if req.RememberToken != "" {
		keep = users.RememberHash(req.RememberToken)
	}

	err = m.users.SetPortalPasswordKeeping(u.UID, req.NewPassword, keep)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "portal password changed", "uid", u.UID)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
}

// emailRequest is the request of the POST /portal/api/email HTTP API.
type emailRequest struct {
	// Email is the address to bind.  An empty value unbinds the current one.
	Email string `json:"email"`

	// Code is the verification code for the address.
	Code string `json:"code"`
}

// handleEmail is the handler for the POST /portal/api/email HTTP API.
func (m *Manager) handleEmail(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()
	l := m.logger

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	req := &emailRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	// Unbinding is always allowed and needs no proof: it gives nothing away.
	if strings.TrimSpace(req.Email) == "" {
		err = m.users.SetEmail(u.UID, "", false)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

			return
		}

		aghhttp.WriteJSONResponseOK(ctx, l, w, r, &meResponse{User: m.InfoForRequest(ctx, r, u)})

		return
	}

	if !m.users.GetSettings().MailConfig().IsConfigured() {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusServiceUnavailable, "%s", ErrMailNotConfigured)

		return
	}

	err = m.checkEmailCode(req.Email, req.Code)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	norm, _ := users.NormalizeEmail(req.Email)

	err = m.users.SetEmail(u.UID, norm, true)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "portal e-mail bound", "uid", u.UID)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &meResponse{User: m.InfoForRequest(ctx, r, u)})
}

// mailTestRequest is the request of the POST /control/portal/mail/test HTTP API.
type mailTestRequest struct {
	// To is the address to send the test message to.
	To string `json:"to"`
}

// handleMailTest is the handler for the POST /control/portal/mail/test HTTP API.
//
// It exists because the mail settings are only exercised when somebody signs
// up, and a mistake there looks to the user like "the code never arrives" with
// nothing in the log that points at the cause.
func (m *Manager) handleMailTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &mailTestRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	to, err := users.NormalizeEmail(req.To)
	if err != nil || to == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", ErrInvalidEmail)

		return
	}

	conf := m.users.GetSettings().MailConfig()
	if !conf.IsConfigured() {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", ErrMailNotConfigured)

		return
	}

	body := "这是一封测试邮件。\n\n" +
		"如果你收到了它，说明 AGHub 门户的邮件服务器配置可用，用户注册和绑定邮箱时的验证码也能发出去。\n"

	err = users.SendMail(conf, to, "AGHub 邮件测试", body)
	if err != nil {
		l.ErrorContext(ctx, "sending a test message", "err", err)

		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadGateway, "sending: %s", err)

		return
	}

	l.InfoContext(ctx, "test message sent")

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
}
