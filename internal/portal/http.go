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
	reg.Register(http.MethodPost, "/portal/api/email/code", m.handleEmailCode)
	reg.Register(http.MethodPost, "/portal/api/register", m.handleRegister)

	// These need a session of their own.
	reg.Register(http.MethodPost, "/portal/api/password", m.handlePassword)
	reg.Register(http.MethodPost, "/portal/api/email", m.handleEmail)

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
}

// handlePackage is the handler for the GET /control/portal/package HTTP API.
// It builds the deployment package of the front-end with the configured API
// address baked in.
func (m *Manager) handlePackage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	s := m.users.GetSettings()

	b, err := Package(s.PortalAPIBase, s.PortalOrigins)
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

	for _, allowed := range normalizeOrigins(m.users.GetSettings().PortalOrigins) {
		if allowed == origin {
			return origin
		}
	}

	return ""
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
	h.Set("Access-Control-Allow-Headers", "Content-Type")
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
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return tok
	}

	raw, err := hex.DecodeString(c.Value)
	if err != nil || len(raw) != aghuser.SessionTokenLength {
		return tok
	}

	copy(tok[:], raw)

	return tok
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
}

// loginResponse is the body of a successful sign-in.
type loginResponse struct {
	// User is the account of the signed-in user.
	User *Info `json:"user"`
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
		aghhttp.WriteJSONResponseOK(ctx, l, w, r, &loginResponse{User: m.InfoForRequest(ctx, r, s.User)})
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

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &meResponse{User: info})
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
