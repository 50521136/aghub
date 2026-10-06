// Package portal implements the user-facing side of AGHub: the sign-in of the
// DNS users and the read-only view of their own identifiers, quota and query
// log.
//
// It is deliberately independent from the administrator interface.  The portal
// has its own session storage, its own cookie and its own rate limiter, and it
// is never authenticated through the administrator middleware: an
// administrator session must not be usable as a portal session, and the other
// way around.
package portal

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghuser"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/google/uuid"
)

const (
	// SessionCookieName is the name of the portal session cookie.  It differs
	// from the administrator cookie so that the two can never be confused.
	SessionCookieName = "aghub_portal"

	// sessionCookiePath scopes the cookie to the portal, so that the browser
	// does not attach it to the administrator requests.
	sessionCookiePath = "/portal"

	// DefaultSessionTTL is how long a portal session lasts.
	DefaultSessionTTL = 7 * 24 * time.Hour

	// DefaultLoginAttempts is how many failed sign-ins a single address may
	// make before it is locked out.
	DefaultLoginAttempts = 10

	// DefaultLoginWindow is the length of the lockout.
	DefaultLoginWindow = 15 * time.Minute

	// DefaultCodeRequests is how many verification codes one address may ask
	// for in [DefaultCodeWindow].
	DefaultCodeRequests = 5

	// DefaultCodeWindow is the window of [DefaultCodeRequests].  It is much
	// longer than the sign-in window, because every request sends a real
	// message to a real mailbox.
	DefaultCodeWindow = 30 * time.Minute

	// DefaultLogLimit is the number of log entries returned when the request
	// does not ask for a different number.
	DefaultLogLimit = 100

	// maxLogLimit is the largest number of log entries a single request may
	// ask for.
	maxLogLimit = 1000
)

// UserStore is the part of the user manager that the portal uses.
type UserStore interface {
	// FindByLogin returns the user matching the login, or nil.
	FindByLogin(login string) (u *users.User)

	// AuthenticatePortal reports whether the password is the portal password
	// of the user.
	AuthenticatePortal(uid, password string) (ok bool)

	// HasPortalPassword reports whether the user may sign in at all.
	HasPortalPassword(uid string) (ok bool)

	// Get returns the user by UID, or nil.
	Get(uid string) (u *users.User)

	// InfoOf returns the API representation of the user, or nil.
	InfoOf(uid string) (i *users.Info)

	// Domain returns the domain of the DoT and DoH endpoints, or an empty
	// string when none is configured.
	Domain() (domain string)

	// GetSettings returns the AGHub-wide settings.  The portal reads the
	// allowed origins from them on every request, so that the administrator
	// does not have to restart AGHub after changing them.
	GetSettings() (s *users.Settings)

	// EnsurePortalToken returns the portal deployment token, generating one
	// when there is none.  The package handler calls it, so that building a
	// package always produces a front-end that can actually talk to AGHub.
	EnsurePortalToken() (tok string, err error)

	// CheckPortalToken reports whether tok is the current portal deployment
	// token.  The front-end sends it on every call; it replaces the origin
	// allow-list, because a header works over http and https from any origin
	// while a cross-origin cookie does not.
	CheckPortalToken(tok string) (ok bool)

	// Summary returns the aggregate state of all accounts.  The public page
	// shows it to visitors who have not signed in, so it must not carry
	// anything about an individual account.
	Summary() (s *users.Summary)

	// Ranking returns the busiest accounts.  The leaderboard shows it to
	// everyone, signed in or not, so it must carry only what the board
	// displays: a name, an identifier and a count.
	// order picks the figure the board is sorted by.
	Ranking(limit int, order users.RankOrder) (r []users.RankEntry)

	// ListFeedback returns the messages left by portal users, newest first.
	// It is administrator-only: the portal itself never reads it back.
	ListFeedback(limit int) (r []*users.Feedback)

	// DeleteFeedback removes one message by its ID.
	DeleteFeedback(id string) (err error)

	// MarkFeedbackRead marks every message as seen.
	MarkFeedbackRead() (err error)

	// CountUnreadFeedback returns how many messages the administrator has
	// not opened yet.
	CountUnreadFeedback() (n int)

	// RegisterProbe issues a connectivity probe for the account and returns
	// the label the browser must resolve.
	RegisterProbe(uid string) (label string)

	// ProbeStatus returns the hit of a probe registered by the account, if
	// the query has arrived.
	ProbeStatus(uid, label string) (hit *users.ProbeHit, ok bool)

	// AddFeedback stores a message from a portal user.
	AddFeedback(f *users.Feedback) (saved *users.Feedback, err error)

	// Register creates an account for a portal user, generating the identifier
	// that the account and the DNS identity share.
	Register(p *users.RegisterParams) (u *users.User, err error)

	// SetEmail sets or clears the e-mail address of the user.
	SetEmail(uid, email string, verified bool) (err error)

	// SetPortalPassword sets the portal password of the user.
	SetPortalPassword(uid, password string) (err error)

	// SetAvatar sets the avatar preset of the user.  Only a value from
	// [users.AvatarPresets] is accepted.
	SetAvatar(uid, avatar string) (err error)

	// CheckinStatus returns the daily check-in state of the user, or nil when
	// there is no such user.
	CheckinStatus(uid string) (st *users.CheckinStatus)

	// Checkin performs the daily check-in of the user and returns what it
	// granted.  It is idempotent within a local day.
	Checkin(uid string) (res *users.CheckinResult, err error)

	// LogUnlocked reports whether the user has earned the query log by
	// keeping up a check-in streak.
	LogUnlocked(uid string) (ok bool)
}

// FilteringStatus is the state of the filter engine.
type FilteringStatus struct {
	// Rules is the number of rules in force.
	Rules uint64

	// Lists is the number of enabled blocking lists.
	Lists int

	// CustomRules is the number of custom rules.
	CustomRules int

	// Enabled is whether filtering is applied at all.
	Enabled bool
}

// FilteringSource reports the state of the filter engine.
//
// It is a separate interface because filtering is optional: without it the
// public page says nothing about rules rather than reporting zero, which would
// read as "nothing is blocked here".
type FilteringSource interface {
	// FilteringStatus returns the state of the filter engine.
	FilteringStatus() (s FilteringStatus)
}

// StatsSummary is the service-wide query counters of the last 24 hours.
type StatsSummary struct {
	// Queries is the number of DNS queries answered.
	Queries int64

	// Blocked is the number of queries rejected by filtering.
	Blocked int64

	// Passed is the number of queries that matched a filtering rule but
	// were allowed by the allow-list.
	Passed int64
}

// StatsSource reports the service-wide query counters of the last 24 hours.
//
// It is a separate interface because statistics are optional: without them the
// public page reports zeroes rather than an invented number.
type StatsSource interface {
	// Stats24h returns the counters.
	Stats24h() (s StatsSummary)
}

// LogSource provides the query log entries of a single user.
type LogSource interface {
	// Search returns the entries belonging to the given clients.
	Search(ctx context.Context, req *LogRequest) (resp *LogResponse, err error)
}

// LogRequest is a search over the log of one user.
type LogRequest struct {
	// ClientIDs are the client identifiers of the user.
	ClientIDs []string

	// ClientNets are the networks the user is identified by.
	ClientNets []netip.Prefix

	// Term is an optional term matched against the host, client and address.
	Term string

	// OlderThan, if not zero, returns only the entries older than it.  The
	// log view uses it as a cursor to load the next page.
	OlderThan time.Time

	// Limit is the maximum number of entries to return.
	Limit int
}

// LogResponse is the result of a [LogRequest].
type LogResponse struct {
	// Entries is the API representation of the entries, newest first.
	Entries map[string]any
}

// Config is the configuration of a [Manager].
type Config struct {
	// Logger is used for logging the operation of the portal.  It must not be
	// nil.
	Logger *slog.Logger

	// Users is the user manager.  It must not be nil.
	Users UserStore

	// Sessions stores the portal sessions.  It must not be nil and must use a
	// database file of its own.  It must not be shared with the administrator
	// sessions.
	Sessions aghuser.SessionStorage

	// Log is the query log.  It must not be nil.
	Log LogSource

	// Filtering reports the state of the filter engine.  It may be nil, and
	// then the public page does not mention the rules at all.
	Filtering FilteringSource

	// Stats reports the service-wide counters of the last 24 hours.  It may
	// be nil, and then the public page reports zeroes for them.
	Stats StatsSource

	// SessionTTL is how long a session lasts.  Zero means
	// [DefaultSessionTTL].
	SessionTTL time.Duration

	// LoginAttempts is how many failed sign-ins an address may make before it
	// is locked out.  Zero means [DefaultLoginAttempts].
	LoginAttempts int

	// LoginWindow is the length of the lockout.  Zero means
	// [DefaultLoginWindow].
	LoginWindow time.Duration

	// CodeRequests is how many verification codes an address may ask for in
	// [Config.CodeWindow].  Zero means [DefaultCodeRequests].
	CodeRequests int

	// CodeWindow is the window of [Config.CodeRequests].  Zero means
	// [DefaultCodeWindow].
	CodeWindow time.Duration

	// Now returns the current time.  It is only replaced in tests.
	Now func() time.Time
}

// Manager is the user portal.
type Manager struct {
	logger    *slog.Logger
	users     UserStore
	sessions  aghuser.SessionStorage
	log       LogSource
	filtering FilteringSource
	stats     StatsSource
	limiter   *loginLimiter
	now       func() time.Time
	ttl       time.Duration

	// codes holds the pending e-mail verifications.  They are deliberately in
	// memory: a code that does not survive a restart is safer, and losing one
	// only costs the user another request.
	codes *codeStore

	// codeLimiter bounds how many messages one address can make the server
	// send, so that the portal cannot be used as a mail relay.
	codeLimiter *loginLimiter
}

// New creates a new portal manager.
func New(conf *Config) (m *Manager, err error) {
	switch {
	case conf == nil:
		return nil, fmt.Errorf("portal: no config")
	case conf.Logger == nil:
		return nil, fmt.Errorf("portal: logger is nil")
	case conf.Users == nil:
		return nil, fmt.Errorf("portal: user store is nil")
	case conf.Sessions == nil:
		return nil, fmt.Errorf("portal: session storage is nil")
	case conf.Log == nil:
		return nil, fmt.Errorf("portal: log source is nil")
	}

	ttl := conf.SessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}

	attempts := conf.LoginAttempts
	if attempts <= 0 {
		attempts = DefaultLoginAttempts
	}

	window := conf.LoginWindow
	if window <= 0 {
		window = DefaultLoginWindow
	}

	codeRequests := conf.CodeRequests
	if codeRequests <= 0 {
		codeRequests = DefaultCodeRequests
	}

	codeWindow := conf.CodeWindow
	if codeWindow <= 0 {
		codeWindow = DefaultCodeWindow
	}

	now := conf.Now
	if now == nil {
		now = time.Now
	}

	return &Manager{
		logger:      conf.Logger.With(slogutil.KeyPrefix, "portal"),
		users:       conf.Users,
		sessions:    conf.Sessions,
		log:         conf.Log,
		filtering:   conf.Filtering,
		stats:       conf.Stats,
		limiter:     newLoginLimiter(attempts, window, now),
		now:         now,
		ttl:         ttl,
		codes:       &codeStore{m: map[string]*emailCode{}},
		codeLimiter: newLoginLimiter(codeRequests, codeWindow, now),
	}, nil
}

// ErrInvalidLogin is returned when the login or the password is wrong.  It is
// deliberately the same for both, so that the response does not reveal whether
// the login exists.
var ErrInvalidLogin = fmt.Errorf("portal: invalid login or password")

// ErrTooManyAttempts is returned when an address made too many failed sign-in
// attempts.
var ErrTooManyAttempts = fmt.Errorf("portal: too many failed sign-in attempts")

// Session is a signed-in portal session.
type Session struct {
	// User is the user the session belongs to.
	User *users.User

	// Token is the session token.  It is what the cookie carries.
	Token aghuser.SessionToken

	// Expire is when the session stops being valid.
	Expire time.Time
}

// Login verifies the credentials and opens a session.  ip is the address of
// the client and is only used for rate limiting.
func (m *Manager) Login(ctx context.Context, login, password string, ip netip.Addr) (s *Session, err error) {
	if !m.limiter.allow(ip) {
		return nil, ErrTooManyAttempts
	}

	u := m.users.FindByLogin(login)
	if u == nil {
		// Run the comparison anyway, so that the response time does not
		// reveal whether the login exists.
		_ = m.users.AuthenticatePortal("", password)
		m.limiter.fail(ip)
		m.logger.InfoContext(ctx, "portal login failed", "login", login, "ip", ip.String())

		return nil, ErrInvalidLogin
	}

	if !m.users.AuthenticatePortal(u.UID, password) {
		m.limiter.fail(ip)
		m.logger.InfoContext(ctx, "portal login failed", "uid", u.UID, "ip", ip.String())

		return nil, ErrInvalidLogin
	}

	m.limiter.succeed(ip)

	sess, err := m.sessions.New(ctx, SessionUser(u.UID))
	if err != nil {
		return nil, fmt.Errorf("portal: creating the session: %w", err)
	}

	m.logger.InfoContext(ctx, "portal login succeeded", "uid", u.UID, "ip", ip.String())

	return &Session{User: u, Token: sess.Token, Expire: sess.Expire}, nil
}

// Logout closes the session with the given token.  An unknown token is not an
// error.
func (m *Manager) Logout(ctx context.Context, token aghuser.SessionToken) (err error) {
	err = m.sessions.DeleteByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("portal: deleting the session: %w", err)
	}

	return nil
}

// Authenticate returns the user of the session with the given token, or nil
// when the session is unknown, expired, revoked or belongs to a user that is
// gone.
func (m *Manager) Authenticate(ctx context.Context, token aghuser.SessionToken) (u *users.User) {
	sess, err := m.sessions.FindByToken(ctx, token)
	if err != nil {
		m.logger.ErrorContext(ctx, "looking up the session", slogutil.KeyError, err)

		return nil
	}

	if sess == nil {
		return nil
	}

	uid := string(sess.UserLogin)
	u = m.users.Get(uid)
	if u == nil {
		return nil
	}

	// A user whose portal access was revoked must lose the session at once,
	// not when it expires.
	if !m.users.HasPortalPassword(uid) {
		return nil
	}

	return u
}

// Info returns the portal representation of a user.
func (m *Manager) Info(u *users.User) (i *Info) {
	info := m.users.InfoOf(u.UID)
	if info == nil {
		return nil
	}

	domain := m.users.Domain()

	return &Info{
		Info:   info,
		Domain: domain,
		Hosts:  hostsOf(u.IDs, domain),
	}
}

// SearchLog returns the log entries of the user.
func (m *Manager) SearchLog(ctx context.Context, u *users.User, req *LogRequest) (resp *LogResponse, err error) {
	if req == nil {
		req = &LogRequest{}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLogLimit
	}

	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	ids, nets := splitIDs(u.IDs)
	if len(ids) == 0 && len(nets) == 0 {
		// A user identified by nothing cannot own any log entry.
		return &LogResponse{Entries: map[string]any{"data": []any{}, "oldest": ""}}, nil
	}

	return m.log.Search(ctx, &LogRequest{
		ClientIDs:  ids,
		ClientNets: nets,
		Term:       req.Term,
		OlderThan:  req.OlderThan,
		Limit:      limit,
	})
}

// CookiePath returns the path the session cookie is scoped to.
func CookiePath() (path string) {
	return sessionCookiePath
}

// TTL returns the lifetime of a new session.
func (m *Manager) TTL() (d time.Duration) {
	return m.ttl
}

// SessionUser returns the session-storage user that stands for the quota user
// with the given UID.
//
// The portal identifies its users by UID, so the login of the session-storage
// user is the UID itself.  The session storage only ever looks the login back
// up, so the identifier merely has to be deterministic, which is what keeps a
// session valid across a restart.
func SessionUser(uid string) (u *aghuser.User) {
	return &aghuser.User{
		ID:       aghuser.UserID(uuid.NewSHA1(uuid.NameSpaceOID, []byte(uid))),
		Login:    aghuser.Login(uid),
		Password: aghuser.NewDefaultPassword(""),
	}
}
