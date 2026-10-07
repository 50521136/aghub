package portal

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/aghuser"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
)

// Register registers the portal handlers with reg.  reg must not apply the
// administrator authentication middleware: the portal authenticates on its
// own.
func (m *Manager) Register(reg aghhttp.Registrar) {
	reg.Register(http.MethodPost, "/portal/api/login", m.handleLogin)
	reg.Register(http.MethodPost, "/portal/api/logout", m.handleLogout)
	reg.Register(http.MethodGet, "/portal/api/me", m.handleMe)
	reg.Register(http.MethodGet, "/portal/api/log", m.handleLog)

	// Sign-up and the account pages.  These are public because a user who
	// cannot sign in yet still has to be able to reach them.
	reg.Register(http.MethodGet, "/portal/api/config", m.handleConfig)
	reg.Register(http.MethodGet, "/portal/api/public", m.handlePublic)
	reg.Register(http.MethodGet, "/portal/api/ranking", m.handleRanking)

	// The wall is read by visitors who have no account yet and written by a
	// signed-in user.  The registrar keys its mux on the path alone, so the
	// two verbs share one handler.
	reg.Register(http.MethodGet, "/portal/api/feedback", m.handleFeedbackAny)
	reg.Register(http.MethodPost, "/portal/api/email/code", m.handleEmailCode)
	reg.Register(http.MethodPost, "/portal/api/register", m.handleRegister)

	// These need a session of their own.
	reg.Register(http.MethodPost, "/portal/api/password", m.handlePassword)
	reg.Register(http.MethodPost, "/portal/api/remember/exchange", m.handleRememberExchange)
	reg.Register(http.MethodPost, "/portal/api/remember/forget", m.handleRememberForget)
	reg.Register(http.MethodGet, "/portal/api/remember", m.handleRememberList)
	reg.Register(http.MethodPost, "/portal/api/remember/revoke", m.handleRememberRevoke)
	reg.Register(http.MethodPost, "/portal/api/email", m.handleEmail)

	// Reading the avatar presets is public -- the picker is shown before the
	// visitor has an account -- while setting one needs a session.  The
	// registrar keys its mux on the path alone, so both verbs have to go
	// through a single handler or the second registration panics.
	reg.Register(http.MethodGet, "/portal/api/avatar", m.handleAvatarAny)

	// The check-in is read and written on one path.  The registrar keys its
	// mux on the path alone, so the two verbs cannot be registered separately
	// and are dispatched by a single handler, exactly like the avatar above.
	reg.Register(http.MethodGet, "/portal/api/checkin", m.handleCheckinAny)

	// The bundled front-end is served on the same origin as the API.  A
	// deployment that hosts the front-end elsewhere simply ignores it.
	m.registerStatic(reg)
}

// RegisterAdmin registers the portal handlers that the administrator uses.
//
// They must be registered on the administrator registrar, not on the one
// [Manager.Register] uses, because they expose the deployment package and are
// not for the portal users.
func (m *Manager) RegisterAdmin(reg aghhttp.Registrar) {
	reg.Register(http.MethodGet, "/control/portal/package", m.handlePackage)
	reg.Register(http.MethodPost, "/control/portal/mail/test", m.handleMailTest)
	reg.Register(http.MethodPost, "/portal/api/probe", m.handleProbe)
	reg.Register(http.MethodGet, "/portal/api/probe/status", m.handleProbeStatus)
	reg.Register(http.MethodGet, "/control/portal/feedback", m.handleFeedbackList)
	reg.Register(http.MethodPost, "/control/portal/feedback/read", m.handleFeedbackRead)
	reg.Register(http.MethodPost, "/control/portal/feedback/update", m.handleFeedbackUpdate)
	reg.Register(http.MethodPost, "/control/portal/feedback/delete", m.handleFeedbackDelete)
}

// probeResponse is the response of POST /portal/api/probe.
type probeResponse struct {
	// Label is the first label of the name the browser must resolve.  The
	// caller appends its own domain to build the full name; nothing has to
	// answer it, because the query reaching the resolver is the whole point.
	Label string `json:"label"`

	// TTL is how many seconds the probe stays matchable.
	TTL int `json:"ttl"`
}

// handleProbe implements POST /portal/api/probe.
//
// The portal cannot ask a browser which resolver it uses -- there is no such
// API -- so it asks the device to prove it instead: the browser resolves a
// random name, and the answer is read from the resolver side.  The name
// carries a token that only this page load knows, so a match cannot come from
// any other device of the account.
func (m *Manager) handleProbe(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	owner, ok := m.probeOwner(r)
	if !ok {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest,
			"a probe without a session needs the portal token and an owner")

		return
	}

	label := m.users.RegisterProbe(owner)
	if label == "" {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusInternalServerError,
			"issuing probe")

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &probeResponse{
		Label: label,
		TTL:   int(users.ProbeTTL.Seconds()),
	})
}

// probeStatusResponse is the response of GET /portal/api/probe/status.
type probeStatusResponse struct {
	// Seen is true when a query for the probe name reached the resolver.
	Seen bool `json:"seen"`

	// Hit describes the query that matched, when Seen is true.  Its address
	// is recorded for diagnostics only; nothing is decided by comparing it
	// with the visitor's, because every device behind one NAT shares it.
	Hit *users.ProbeHit `json:"hit,omitempty"`
}

// handleProbeStatus implements GET /portal/api/probe/status.
func (m *Manager) handleProbeStatus(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	owner, ok := m.probeOwner(r)
	if !ok {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest,
			"a probe without a session needs the portal token and an owner")

		return
	}

	label := r.URL.Query().Get("token")
	if label == "" {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "token is required")

		return
	}

	resp := &probeStatusResponse{}

	// The answer is the probe alone.  The token in the name is known only to
	// the page load that issued it, so a match is this device by construction
	// and no address comparison is needed -- which is just as well, since
	// every device behind one NAT shares an address.
	hit, found := m.users.ProbeStatus(owner, label)
	if found {
		resp.Seen = true
		resp.Hit = hit
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, resp)
}

// anonymousProbePrefix separates the probe keys of visitors who have not signed
// in from the account identifiers.  Without it, an anonymous caller could name
// somebody else's account and read that account's probe.
const anonymousProbePrefix = "anon:"

// probeOwner returns the key a probe is registered under, and false when the
// request may not have one.
//
// A signed-in request uses its account.  A request without a session uses the
// opaque owner the portal passes, and is only accepted with a valid portal
// token: the probe table is bounded by a three-minute expiry, and an endpoint
// that lets anyone add to it would be a way to keep it full.
func (m *Manager) probeOwner(r *http.Request) (owner string, ok bool) {
	if u := m.optionalUser(r); u != nil {
		return u.UID, true
	}

	if !m.users.CheckPortalToken(portalTokenOf(r)) {
		return "", false
	}

	anon := strings.TrimSpace(r.URL.Query().Get("owner"))
	if !isValidProbeOwner(anon) {
		return "", false
	}

	return anonymousProbePrefix + anon, true
}

// optionalUser returns the signed-in user, or nil when the request carries no
// usable session.  Unlike [Manager.requireUser] it writes nothing, so it can be
// used where being signed out is allowed.
func (m *Manager) optionalUser(r *http.Request) (u *users.User) {
	tok := m.sessionToken(r)
	if isZeroToken(tok) {
		return nil
	}

	return m.Authenticate(r.Context(), tok)
}

// isValidProbeOwner reports whether s may be used as the owner key of an
// anonymous probe.  The portal generates it, but it arrives over the network
// and ends up in a map key, so it is checked rather than trusted.
func isValidProbeOwner(s string) (ok bool) {
	const (
		minLen = 16
		maxLen = 64
	)

	if len(s) < minLen || len(s) > maxLen {
		return false
	}

	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_':
		default:
			return false
		}
	}

	return true
}

// feedbackListResponse is the response of the GET /control/portal/feedback HTTP
// API.
type feedbackListResponse struct {
	// Items is the messages, newest first.
	Items []*users.Feedback `json:"items"`

	// Unread is how many of them have not been opened.
	Unread int `json:"unread"`

	// Open is how many of them the administrator has not closed yet.  The
	// page shows it as the work left, so it counts every message rather than
	// the ones of the page.
	Open int `json:"open"`
}

// handleFeedbackList is the handler for the GET /control/portal/feedback HTTP
// API.
//
// The portal accepts messages from users and, until this existed, put them
// somewhere the administrator had no way to read.  It is administrator-only:
// the messages carry contact details, so they are not part of the public API.
func (m *Manager) handleFeedbackList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "invalid limit")

			return
		}

		limit = n
	}

	items := m.users.ListFeedback(limit)
	if items == nil {
		items = []*users.Feedback{}
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &feedbackListResponse{
		Items:  items,
		Unread: m.users.CountUnreadFeedback(),
		Open:   m.users.CountOpenFeedback(),
	})
}

// feedbackUpdateRequest is the request of the
// POST /control/portal/feedback/update HTTP API.
//
// Every field is a pointer: an absent one means "leave it as it is", so that
// closing a message cannot wipe its reply and answering one cannot reopen it.
type feedbackUpdateRequest struct {
	// ID is the message to change.
	ID string `json:"id"`

	// Resolved closes the message or reopens it.
	Resolved *bool `json:"resolved"`

	// Private hides the message from the public wall or shows it again.
	Private *bool `json:"private"`

	// Reply replaces the answer to the author.  An empty reply clears it.
	Reply *string `json:"reply"`
}

// feedbackUpdateResponse is the response of the
// POST /control/portal/feedback/update HTTP API.
type feedbackUpdateResponse struct {
	// Item is the message as it is stored after the change.
	Item *users.Feedback `json:"item"`
}

// handleFeedbackUpdate is the handler for the
// POST /control/portal/feedback/update HTTP API.
//
// It is the only way the administrator answers a message, closes it or changes
// whether the portal shows it, and every one of those marks the message read:
// the administrator cannot act on a message without having seen it.
func (m *Manager) handleFeedbackUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &feedbackUpdateRequest{}

	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if req.ID == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "id is required")

		return
	}

	item, err := m.users.UpdateFeedback(req.ID, &users.FeedbackUpdate{
		Resolved: req.Resolved,
		Private:  req.Private,
		Reply:    req.Reply,
	})
	if err != nil {
		// An unknown identifier is the caller's mistake, not a failure of
		// the server: the page may have been open while the message was
		// deleted in another tab.
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusNotFound, "updating: %s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &feedbackUpdateResponse{Item: item})
}

// feedbackDeleteRequest is the request of the POST /control/portal/feedback/delete
// HTTP API.
type feedbackDeleteRequest struct {
	// ID is the message to remove.
	ID string `json:"id"`
}

// handleFeedbackDelete is the handler for the
// POST /control/portal/feedback/delete HTTP API.
func (m *Manager) handleFeedbackDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &feedbackDeleteRequest{}

	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if req.ID == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "id is required")

		return
	}

	err = m.users.DeleteFeedback(req.ID)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "deleting: %s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
}

// handleFeedbackRead is the handler for the POST /control/portal/feedback/read
// HTTP API.  It marks every message as seen, which is what opening the page
// means.
func (m *Manager) handleFeedbackRead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	err := m.users.MarkFeedbackRead()
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "marking read: %s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &okResponse{})
}

// handlePackage is the handler for the GET /control/portal/package HTTP API.
// It builds the deployment package of the front-end with the configured API
// address baked in.
func (m *Manager) handlePackage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	s := m.users.GetSettings()

	// The package is useless without a token, so make sure one exists before
	// building it.  The administrator never has to generate it by hand.
	tok, err := m.users.EnsurePortalToken()
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	b, err := Package(s.PortalAPIBase, tok)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	l.InfoContext(ctx, "portal package built", "size", len(b), "api_base", s.PortalAPIBase)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", PackageFileName),
	)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))

	_, err = w.Write(b)
	if err != nil {
		l.ErrorContext(ctx, "writing the portal package", "err", err)
	}
}

// allowedOrigin returns the value for the CORS headers when the request comes
// from an allowed origin, or an empty string.
//
// The portal front-end may be hosted on a different origin than the API, so
// the browser sends the request cross-site.  Only the origins from the settings
// are echoed back, and the wildcard is never used, because the responses carry
// the session cookie.
func (m *Manager) allowedOrigin(r *http.Request) (origin string) {
	origin = r.Header.Get("Origin")
	if origin == "" {
		return ""
	}

	// A preflight cannot carry a custom header, so the deployment token cannot
	// be checked here.  Answering it reveals nothing: the request it precedes
	// still has to present a valid token, or it is refused without any data.
	if r.Method == http.MethodOptions {
		return origin
	}

	// The deployment token is the modern way in.  It is baked into the package
	// the administrator builds, so a front-end served from anywhere works
	// without an origin to register.
	if m.users.CheckPortalToken(portalTokenOf(r)) {
		return origin
	}

	// The origin allow-list is kept for deployments configured before the
	// token existed.
	for _, allowed := range normalizeOrigins(m.users.GetSettings().PortalOrigins) {
		if allowed == origin {
			return origin
		}
	}

	return ""
}

// portalTokenOf returns the deployment token of the request, taken from the
// X-Portal-Token header or the token query parameter.
//
// The header is the normal way.  The query parameter exists for a front-end
// that cannot set headers -- a plain <img> or a redirect -- and because the
// token is not a credential for any account, putting it in a URL costs
// nothing beyond the log line.
func portalTokenOf(r *http.Request) (tok string) {
	if tok = strings.TrimSpace(r.Header.Get("X-Portal-Token")); tok != "" {
		return tok
	}

	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// normalizeOrigins reduces the configured origins to their scheme and host and
// drops the ones that are not usable.
func normalizeOrigins(raw []string) (origins []string) {
	for _, o := range raw {
		if o = strings.TrimSpace(o); o == "" {
			continue
		}

		if origin := OriginFromURL(o); origin != "" {
			origins = append(origins, origin)

			continue
		}

		if strings.Contains(o, "://") {
			// A URL without a host cannot be an origin.
			continue
		}

		// Allow the bare "host" and "host:port" forms as well.
		origins = append(origins, o)
	}

	return origins
}

// handleCORS writes the CORS headers when needed and returns true if the
// request was a preflight that has been answered.
func (m *Manager) handleCORS(w http.ResponseWriter, r *http.Request) (answered bool) {
	origin := m.allowedOrigin(r)
	if origin == "" {
		return false
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Portal-Token")
	h.Set("Access-Control-Max-Age", "600")
	h.Add("Vary", "Origin")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)

		return true
	}

	return false
}

// crossSite reports whether the request comes from another origin.
func (m *Manager) crossSite(r *http.Request) (ok bool) {
	return m.allowedOrigin(r) != ""
}

// isHTTPS reports whether the request reached the server over TLS, possibly
// through a reverse proxy that terminated it.
func isHTTPS(r *http.Request) (ok bool) {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setSessionCookie writes the session cookie.
//
// When the front-end is on another origin the cookie has to be sent
// cross-site, which browsers only allow for SameSite=None, and SameSite=None
// is only accepted together with Secure.  A cross-origin deployment therefore
// requires HTTPS on the API as well.
func (m *Manager) setSessionCookie(w http.ResponseWriter, r *http.Request, s *Session) {
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    hex.EncodeToString(s.Token[:]),
		Path:     sessionCookiePath,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}

	if s.Expire.After(time.Time{}) {
		c.Expires = s.Expire
		c.MaxAge = int(time.Until(s.Expire).Seconds())
	}

	if m.crossSite(r) {
		c.SameSite = http.SameSiteNoneMode
		c.Secure = true
	}

	http.SetCookie(w, c)
}

// clearSessionCookie expires the session cookie.
func (m *Manager) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     sessionCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}

	if m.crossSite(r) {
		c.SameSite = http.SameSiteNoneMode
		c.Secure = true
	}

	http.SetCookie(w, c)
}

// sessionToken returns the token of the request, or the zero token when the
// request carries no usable one.
func (m *Manager) sessionToken(r *http.Request) (tok aghuser.SessionToken) {
	raw := bearerToken(r)
	if raw == "" {
		// Fall back to the cookie, which is what a page served by AGHub
		// itself uses.
		c, err := r.Cookie(SessionCookieName)
		if err != nil || c.Value == "" {
			return tok
		}

		raw = c.Value
	}

	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != aghuser.SessionTokenLength {
		return tok
	}

	copy(tok[:], decoded)

	return tok
}

// bearerToken returns the hex session token from the Authorization header, or
// an empty string.
func bearerToken(r *http.Request) (tok string) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return ""
	}

	const prefix = "Bearer "
	if len(raw) > len(prefix) && strings.EqualFold(raw[:len(prefix)], prefix) {
		return strings.TrimSpace(raw[len(prefix):])
	}

	return ""
}

// isZeroToken reports whether tok is the zero token.
func isZeroToken(tok aghuser.SessionToken) (ok bool) {
	return tok == aghuser.SessionToken{}
}

// clientIP returns the address of the client for the rate limiter.
func clientIP(r *http.Request) (ip netip.Addr) {
	host, err := netutil.SplitHost(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}

	return addr.Unmap()
}

// loginRequest is the body of the sign-in request.
type loginRequest struct {
	// Login is the identifier or the name of the user.
	Login string `json:"login"`

	// Password is the portal password.
	Password string `json:"password"`

	// Remember asks for a remembered sign-in, so that the next visit does not
	// need the password again.
	Remember bool `json:"remember"`
}

// loginResponse is the body of a successful sign-in.
type loginResponse struct {
	// User is the account of the signed-in user.
	User *Info `json:"user"`

	// Token is the session token, hex encoded.  A front-end that is served
	// from another origin keeps it and sends it back in the Authorization
	// header; the cookie is set as well, so the same API works for a page
	// served by AGHub itself.
	//
	// A token rather than only a cookie, because a cross-origin cookie has to
	// be SameSite=None, which browsers accept only with Secure, which a plain
	// HTTP response cannot set.  A header carries no such baggage: it works
	// over http and https, from any origin, with no cookie flags to get right.
	Token string `json:"token"`

	// RememberToken is the long-lived token for the browser, returned only
	// when the request asked to be remembered.  The browser hands it back to
	// POST /portal/api/remember/exchange to get a new session without a
	// password.
	RememberToken string `json:"remember_token,omitempty"`

	// RememberExpire is when RememberToken stops working, as a Unix timestamp.
	RememberExpire int64 `json:"remember_expire,omitempty"`
}

// handleLogin implements POST /portal/api/login.
func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	l := m.logger

	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	req := &loginRequest{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	s, err := m.Login(ctx, req.Login, req.Password, clientIP(r))
	switch {
	case errors.Is(err, ErrTooManyAttempts):
		w.Header().Set("Retry-After", strconv.Itoa(int(m.limiter.window.Seconds())))
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusTooManyRequests, "%s", err)
	case errors.Is(err, ErrInvalidLogin):
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusUnauthorized, "%s", err)
	case err != nil:
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)
	default:
		m.setSessionCookie(w, r, s)

		resp := &loginResponse{
			User:  m.InfoForRequest(ctx, r, s.User),
			Token: hex.EncodeToString(s.Token[:]),
		}
		if req.Remember {
			m.issueRemember(ctx, r, s.User, resp)
		}

		aghhttp.WriteJSONResponseOK(ctx, l, w, r, resp)
	}
}

// handleLogout implements POST /portal/api/logout.
func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	tok := m.sessionToken(r)
	if !isZeroToken(tok) {
		err := m.Logout(ctx, tok)
		if err != nil {
			m.logger.ErrorContext(ctx, "logging out", slogutil.KeyError, err)
		}
	}

	m.clearSessionCookie(w, r)
	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &struct{}{})
}

// requireUser authenticates the request and writes the error response when it
// fails.
func (m *Manager) requireUser(w http.ResponseWriter, r *http.Request) (u *users.User, ok bool) {
	ctx := r.Context()

	tok := m.sessionToken(r)
	if isZeroToken(tok) {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusUnauthorized, "no session")

		return nil, false
	}

	u = m.Authenticate(ctx, tok)
	if u == nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusUnauthorized, "invalid session")

		return nil, false
	}

	return u, true
}

// meResponse is the body of the account endpoint.
type meResponse struct {
	// User is the account of the signed-in user.
	User *Info `json:"user"`

	// Checkin is the daily check-in state of the account.  It is a sibling of
	// User rather than part of it so that it mirrors the standalone
	// GET /portal/api/checkin, which returns the same object at the top level.
	Checkin *users.CheckinStatus `json:"checkin"`
}

// handleMe implements GET /portal/api/me.
func (m *Manager) handleMe(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	info := m.InfoForRequest(ctx, r, u)
	if info == nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusNotFound, "no such user")

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &meResponse{
		User:    info,
		Checkin: m.users.CheckinStatus(u.UID),
	})
}

// handlePublic implements GET /portal/api/public.
//
// It answers without a session: this is what the page shows before anybody
// signs in, and it is the only portal handler that does so on purpose.
func (m *Manager) handlePublic(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, m.PublicStats())
}

// handleRanking implements GET /portal/api/ranking.
//
// The board is public, like the statistics page: it is what makes the portal
// worth opening when a visitor has nothing to configure.
func (m *Manager) handleRanking(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}

	order := users.ParseRankOrder(r.URL.Query().Get("order"))

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &rankingResponse{
		Entries: m.users.Ranking(limit, order),
		Order:   order,
		Updated: time.Now().Unix(),
	})
}

// rankingResponse is the response of GET /portal/api/ranking.
type rankingResponse struct {
	// Entries are the rows of the board, best first.
	Entries []users.RankEntry `json:"entries"`

	// Order is the figure the board was sorted by, which is what the caller
	// asked for or the default when it asked for nothing usable.
	Order users.RankOrder `json:"order"`

	// Updated is the Unix timestamp of the response.
	Updated int64 `json:"updated"`
}

// avatarPresetsResponse is the response of GET /portal/api/avatar.
type avatarPresetsResponse struct {
	// Presets is the list of avatars a user may choose.  The front-end
	// renders them as they are; the emoji themselves are the keys, so there
	// is no second table to keep in step.
	Presets []string `json:"presets"`
}

// handleAvatarPresets implements GET /portal/api/avatar.
//
// It answers without a session: the avatar picker is shown to visitors who
// have not signed in yet.  The list comes from the user module, which is the
// single source of truth for both the front-end and the validation of the
// value that is posted back.
// handleAvatarAny serves the avatar path for both verbs.
//
// [Manager.Register] hands the method to the middleware wrapper, not to the
// mux, so two registrations on one path collide.  Dispatching here keeps the
// presets readable without a session while setting one still requires one.
func (m *Manager) handleAvatarAny(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		m.handleAvatar(w, r)

		return
	}

	m.handleAvatarPresets(w, r)
}

func (m *Manager) handleAvatarPresets(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &avatarPresetsResponse{
		Presets: users.AvatarPresets(),
	})
}

// avatarRequest is the body of POST /portal/api/avatar.
type avatarRequest struct {
	// Avatar is one of the presets from GET /portal/api/avatar.
	Avatar string `json:"avatar"`
}

// avatarResponse is the response of POST /portal/api/avatar.
type avatarResponse struct {
	// OK is true when the avatar was stored.
	OK bool `json:"ok"`

	// Avatar is the avatar that was stored.
	Avatar string `json:"avatar"`
}

// handleAvatar implements POST /portal/api/avatar.
func (m *Manager) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	req := &avatarRequest{}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	// The check is repeated in the user module as well, so that a caller
	// that skips this handler cannot store something the picker never
	// offered.
	if !users.IsValidAvatar(req.Avatar) {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "unknown avatar")

		return
	}

	err = m.users.SetAvatar(u.UID, req.Avatar)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &avatarResponse{
		OK:     true,
		Avatar: req.Avatar,
	})
}

// handleCheckinAny serves /portal/api/checkin for both verbs.
//
// The registrar keys its mux on the path alone, so reading the state and
// recording a check-in have to share a handler; the method picks the one.
func (m *Manager) handleCheckinAny(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		m.handleCheckin(w, r)

		return
	}

	m.handleCheckinStatus(w, r)
}

// handleCheckinStatus implements GET /portal/api/checkin.
func (m *Manager) handleCheckinStatus(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	st := m.users.CheckinStatus(u.UID)
	if st == nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusNotFound, "no such user")

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, st)
}

// checkinResponse is the response of POST /portal/api/checkin.
type checkinResponse struct {
	// OK is true whenever the check-in was recorded, including a repeat on
	// the same day, so that a retry is not mistaken for a failure.
	OK bool `json:"ok"`

	// Streak is the number of consecutive check-in days after the call.
	Streak int64 `json:"streak"`

	// TempBonus is the temporary allowance granted by the call.  It is zero
	// when the account had already checked in or has no finite quota.
	TempBonus int64 `json:"temp_bonus"`

	// PermanentBonus is the permanent quota increase granted by the call.  It
	// is zero when no milestone was reached.
	PermanentBonus int64 `json:"permanent_bonus"`

	// Milestone is the streak length that granted
	// [checkinResponse.PermanentBonus], or zero when none was reached.
	Milestone int64 `json:"milestone"`
}

// handleCheckin implements POST /portal/api/checkin.
func (m *Manager) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	res, err := m.users.Checkin(u.UID)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &checkinResponse{
		OK:             true,
		Streak:         res.Streak,
		TempBonus:      res.TempBonus,
		PermanentBonus: res.PermanentBonus,
		Milestone:      res.Milestone,
	})
}

// handleFeedback implements POST /portal/api/feedback.
func (m *Manager) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	body := &struct {
		// Content is the message itself.
		Content string `json:"content"`

		// Contact is an optional way to reach the sender.
		Contact string `json:"contact"`

		// Public is whether the author allows the message on the public
		// wall.  An absent field means yes: that is what the form offers,
		// and a caller that does not know about the flag should not end up
		// with a private message by accident.
		Public *bool `json:"public"`
	}{}

	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(body)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest,
			"decoding feedback: %s", err)

		return
	}

	saved, err := m.users.AddFeedback(&users.Feedback{
		UID:     u.UID,
		Name:    u.Name,
		Contact: body.Contact,
		Content: body.Content,
		Private: body.Public != nil && !*body.Public,
	})
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest,
			"storing feedback: %s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &feedbackResponse{
		ID:        saved.ID,
		CreatedAt: saved.CreatedAt,
	})
}

// handleFeedbackAny serves /portal/api/feedback for both verbs.
//
// The registrar keys its mux on the path alone, so reading the wall and leaving
// a message have to share a handler; the method picks the one.
func (m *Manager) handleFeedbackAny(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		m.handleFeedback(w, r)

		return
	}

	m.handleFeedbackWall(w, r)
}

// feedbackView is one message as the portal shows it.
//
// It is built from [users.Feedback] rather than being the stored struct, so
// that what reaches a browser is decided here: the contact details of the
// author and the account the message belongs to never leave AGHub, and the page
// of one visitor carries nothing about another.
type feedbackView struct {
	// ID is the identifier of the message.
	ID string `json:"id"`

	// Name is the display name of the author.
	Name string `json:"name"`

	// Content is the message itself.
	Content string `json:"content"`

	// CreatedAt is the Unix timestamp of the message.
	CreatedAt int64 `json:"created_at"`

	// Public is whether the message is shown to everybody.
	Public bool `json:"public"`

	// Resolved is whether the administrator has closed it.
	Resolved bool `json:"resolved"`

	// ResolvedAt is the Unix timestamp of the moment it was closed, zero
	// while it is open.
	ResolvedAt int64 `json:"resolved_at"`

	// Reply is the answer of the administrator, empty until there is one.
	Reply string `json:"reply"`

	// RepliedAt is the Unix timestamp of the last change of the reply.
	RepliedAt int64 `json:"replied_at"`

	// Mine is whether the message was written by the visitor asking for it.
	Mine bool `json:"mine"`
}

// feedbackWallResponse is the response of GET /portal/api/feedback.
type feedbackWallResponse struct {
	// Items are the messages, newest first.
	Items []feedbackView `json:"items"`

	// LoggedIn is whether the caller has a session.  A visitor who has none
	// gets the public messages only, and the page says so.
	LoggedIn bool `json:"logged_in"`
}

// handleFeedbackWall implements GET /portal/api/feedback.
//
// The wall is public on purpose: a visitor who has not signed in still reads
// what other people reported, which is what makes the page worth opening.  A
// signed-in visitor also gets their own messages whatever their flag, so that a
// private report and the answer to it are not lost to its author.
func (m *Manager) handleFeedbackWall(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}

	u := m.optionalUser(r)

	var uid string
	if u != nil {
		uid = u.UID
	}

	items := m.users.ListFeedbackFor(uid, limit)
	views := make([]feedbackView, 0, len(items))

	for _, f := range items {
		views = append(views, feedbackViewOf(f, uid))
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &feedbackWallResponse{
		Items:    views,
		LoggedIn: u != nil,
	})
}

// feedbackViewOf builds what the portal shows of one message.
func feedbackViewOf(f *users.Feedback, uid string) (v feedbackView) {
	return feedbackView{
		ID:         f.ID,
		Name:       f.Name,
		Content:    f.Content,
		CreatedAt:  f.CreatedAt,
		Public:     f.IsPublic(),
		Resolved:   f.Resolved,
		ResolvedAt: f.ResolvedAt,
		Reply:      f.Reply,
		RepliedAt:  f.RepliedAt,
		Mine:       uid != "" && f.UID == uid,
	}
}

// feedbackResponse is the response of POST /portal/api/feedback.
type feedbackResponse struct {
	// ID is the identifier of the stored message.
	ID string `json:"id"`

	// CreatedAt is the Unix timestamp of the message.
	CreatedAt int64 `json:"created_at"`
}

// handleLog implements GET /portal/api/log.
//
// The handler only ever searches the entries of the signed-in user, and the
// client filter is built from the identifiers of that user, so a user cannot
// ask for the entries of another one.
func (m *Manager) handleLog(w http.ResponseWriter, r *http.Request) {
	if m.handleCORS(w, r) {
		return
	}

	ctx := r.Context()

	u, ok := m.requireUser(w, r)
	if !ok {
		return
	}

	// The log is what a check-in streak buys.  An account that has not earned
	// it yet is told so rather than shown an empty list, which would read as
	// a page that is broken.
	if !m.users.LogUnlocked(u.UID) {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusForbidden,
			"the query log opens after %d consecutive check-in days", users.LogUnlockStreak)

		return
	}

	q := r.URL.Query()
	req := &LogRequest{Term: strings.TrimSpace(q.Get("term"))}

	if raw := q.Get("older_than"); raw != "" {
		olderThan, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "invalid older_than")

			return
		}

		req.OlderThan = olderThan
	}

	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "invalid limit")

			return
		}

		req.Limit = limit
	}

	resp, err := m.SearchLog(ctx, u, req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, resp.Entries)
}

// OriginFromURL returns the origin of a URL, for example
// "https://portal.example.com".  It returns an empty string when the URL has
// no scheme or no host.
func OriginFromURL(raw string) (origin string) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}

	return u.Scheme + "://" + u.Host
}
