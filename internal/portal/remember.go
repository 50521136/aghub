package portal

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// A remembered device is how a visitor stops being asked to sign in: the
// browser keeps a long-lived token, the portal trades it for a session the
// first time a page needs one, and the visitor never sees the sign-in form.
// It covers the moments that used to end a session — the browser was closed,
// the session was collected while they were away, the panel restarted.
//
// Three properties keep this from being a password in a cookie:
//
//   - Only the hash of the token is stored, so a copy of the state file is not
//     a set of working credentials.
//   - A token is single use.  Every exchange replaces it, so a token copied
//     from a browser stops working the moment the real browser comes back —
//     and the reuse is noticed, which revokes the device.
//   - The devices are listed and can be revoked, which is what turns "my
//     laptop was stolen" into something the owner can act on.

// rememberRequest is the body of the endpoints that take a remembered token.
type rememberRequest struct {
	// Token is the long-lived token from the browser.
	Token string `json:"token"`
}

// rememberRevokeRequest is the body of the revoke endpoint.
type rememberRevokeRequest struct {
	// ID is the device to revoke, as returned by the list.
	ID string `json:"id"`

	// Keep is the device to spare, given as the id from the list.  It is what
	// makes "sign the other devices out" leave the asking one alone.
	Keep string `json:"keep"`

	// All asks to revoke every device of the account except Keep.
	All bool `json:"all"`
}

// rememberListResponse is the response of the list endpoint.
type rememberListResponse struct {
	// Devices are the remembered devices of the signed-in account, newest
	// first.
	Devices []*users.RememberInfo `json:"devices"`
}

// handleRememberExchange implements POST /portal/api/remember/exchange.
//
// The caller proves it holds a remembered token and gets back a session plus
// the token that replaces the one it presented.
func (m *Manager) handleRememberExchange(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()
	l := m.logger

	req := &rememberRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if !looksLikeRememberToken(req.Token) {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusUnauthorized,
			"the remembered sign-in is not valid any more")

		return
	}

	ip, ua := m.requestVisitor(r)

	uid, tok, info, err := m.users.ExchangeRemember(req.Token, ua, ip.String())
	switch {
	case errors.Is(err, users.ErrRememberUnknown):
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusUnauthorized,
			"the remembered sign-in is not valid any more")

		return
	case err != nil:
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	// The same rule as [Manager.Authenticate]: the account has to exist and
	// still have portal access.  A disabled account is deliberately not
	// refused here — signing in is how its owner finds out that it is
	// disabled, and the password path does not refuse it either.
	u := m.users.Get(uid)
	if u == nil || !m.users.HasPortalPassword(uid) {
		// The account is gone or its portal access was taken away.  The token
		// has to go with it, or every page load asks again.
		_ = m.users.ForgetRememberAll(uid, "")

		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusUnauthorized, "the account cannot sign in")

		return
	}

	sess, err := m.sessions.New(ctx, SessionUser(uid))
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError,
			"creating the session: %s", err)

		return
	}

	m.setSessionCookie(w, r, &Session{User: u, Token: sess.Token, Expire: sess.Expire})
	l.InfoContext(ctx, "portal resumed a remembered device", "uid", uid, "ip", ip.String())

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &loginResponse{
		User:           m.InfoForRequest(ctx, r, u),
		Token:          hex.EncodeToString(sess.Token[:]),
		RememberToken:  tok,
		RememberExpire: info.Expire,
	})
}

// handleRememberForget implements POST /portal/api/remember/forget.  It is
// what signing out uses: knowing the token is already enough to use it, so
// requiring a session as well would only leave tokens behind when the session
// is what has just been thrown away.
func (m *Manager) handleRememberForget(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	req := &rememberRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	err = m.users.ForgetRemember(req.Token)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &okResponse{})
}

// handleRememberList implements GET /portal/api/remember.
func (m *Manager) handleRememberList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	devices := m.users.Remembered(u.UID)
	if devices == nil {
		devices = []*users.RememberInfo{}
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &rememberListResponse{Devices: devices})
}

// handleRememberRevoke implements POST /portal/api/remember/revoke.  It either
// revokes one device by id or every device of the account but one.
func (m *Manager) handleRememberRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	req := &rememberRevokeRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	switch {
	case req.All:
		n := m.users.ForgetRememberAll(u.UID, req.Keep)
		l.InfoContext(ctx, "portal signed the remembered devices out", "uid", u.UID, "devices", n)
	case req.ID != "":
		err = m.users.ForgetRememberID(u.UID, req.ID)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)

			return
		}

		l.InfoContext(ctx, "portal revoked a remembered device", "uid", u.UID)
	default:
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "nothing to revoke")

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
}

// issueRemember records a remembered device for a user that has just signed in
// and fills the response in.
//
// A failure here is logged and swallowed: the visitor is signed in either way,
// and refusing the sign-in because the convenience token could not be stored
// would be a worse answer than the one they came for.
func (m *Manager) issueRemember(
	ctx context.Context,
	r *http.Request,
	u *users.User,
	resp *loginResponse,
) {
	ip, ua := m.requestVisitor(r)

	tok, info, err := m.users.IssueRemember(u.UID, ua, ip.String())
	if err != nil {
		m.logger.ErrorContext(ctx, "issuing a remembered sign-in", slogutil.KeyError, err)

		return
	}

	resp.RememberToken = tok
	resp.RememberExpire = info.Expire
}

// looksLikeRememberToken reports whether tok can be one of ours.  The check
// costs nothing and keeps a malformed request from reaching the store.
func looksLikeRememberToken(tok string) (ok bool) {
	if len(tok) != 2*rememberTokenBytes {
		return false
	}

	_, err := hex.DecodeString(tok)

	return err == nil
}

// rememberTokenBytes is the length of the random part of a remembered token,
// mirroring the store.  It is duplicated here because the portal only needs to
// recognise the shape, not to create one.
const rememberTokenBytes = 32
