// Package users implements the built-in user quota management layer.
//
// A user is an entity that owns one or more client identifiers (IP addresses,
// CIDR networks, or ClientIDs) and has a query quota and an expiration date.
// The DNS server consults the manager on every request and refuses the
// requests of users who have exhausted their quota or whose subscription has
// expired.
package users

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/mail"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
	"golang.org/x/crypto/bcrypt"
)

// Period is the quota accounting period of a user.
type Period string

// Supported accounting periods.
const (
	PeriodDay   Period = "day"
	PeriodMonth Period = "month"
	PeriodTotal Period = "total"
)

// Unlimited is the sentinel value for a field that imposes no limit.
const Unlimited int64 = -1

// Reasons reported for users that are not allowed to query.
const (
	// ReasonManual means the user was disabled by the administrator.
	ReasonManual = "manual"

	// ReasonQuota means the user has exhausted the request quota.
	ReasonQuota = "quota"

	// ReasonExpired means the user's subscription has expired.
	ReasonExpired = "expired"

	// ReasonUnmatched means the client does not belong to any user and the
	// manager is configured to deny unmatched clients.
	ReasonUnmatched = "unmatched"
)

// Status values reported for users.
const (
	StatusActive       = "active"
	StatusDisabled     = "disabled"
	StatusExpired      = "expired"
	StatusOverQuota    = "over_quota"
	StatusExpiringSoon = "expiring_soon"
)

// expiringSoonThreshold is the number of seconds before the expiration date at
// which a user is reported as expiring soon.
const expiringSoonThreshold = 3 * 24 * 60 * 60

// secsPerDay is the number of seconds in a day.
const secsPerDay = 24 * 60 * 60

// User is a managed DNS user with a query quota.  All fields are persisted.
//
// A *User must be treated as immutable once it is published in a snapshot;
// mutations create a new value and publish a new snapshot.
type User struct {
	// UID is the unique identifier of the user.  It is generated on creation
	// and never changes.
	UID string `json:"uid"`

	// Name is the human-readable name of the user.
	Name string `json:"name"`

	// Remark is an optional free-form note.
	Remark string `json:"remark,omitempty"`

	// IDs are the identifiers of the user, such as IP addresses, CIDR
	// networks, or ClientIDs.
	IDs []string `json:"ids"`

	// RequestLimit is the maximum number of requests allowed per Period.  It
	// is either non-negative or [Unlimited].
	RequestLimit int64 `json:"request_limit"`

	// Period is the quota accounting period.
	Period Period `json:"period"`

	// ExpiresAt is the Unix timestamp in seconds at which the user's
	// subscription expires.  Zero means that it never expires.
	ExpiresAt int64 `json:"expires_at"`

	// CreatedAt is the Unix timestamp in seconds of the user's creation.
	CreatedAt int64 `json:"created_at"`

	// Enabled is the administrator-controlled switch of the user.
	Enabled bool `json:"enabled"`

	// Email is the e-mail address of the user.  It is empty when the user
	// registered without one.  At most one user may hold a given address.
	Email string `json:"email,omitempty"`

	// EmailVerified is true when the user has proved that the address in
	// [User.Email] belongs to them.  It is only ever set together with an
	// address, and it is cleared when the address changes.
	EmailVerified bool `json:"email_verified,omitempty"`

	// Avatar is the avatar preset the user chose in the portal.  It is one
	// of the strings in [AvatarPresets] and is empty when the user has not
	// chosen one.  Only the preset itself is stored; there is no upload.
	Avatar string `json:"avatar"`
}

const (
	// minPortalPasswordLen is the minimum length of a portal password.
	minPortalPasswordLen = 8

	// maxPortalPasswordLen is the maximum length of a portal password.  It
	// keeps the bcrypt input within its limit and rejects accidental
	// pastes.
	maxPortalPasswordLen = 128
)

// historyDays is the number of days of per-user usage history that is kept.
const historyDays = 30

// hourSeconds is the length of one hourly request bucket.
const hourSeconds = 3600

// hoursWindow is the number of hourly buckets that are kept: the hour in
// progress plus the twenty-three before it.  The window is aligned to hour
// boundaries rather than to the second the request was made, which is precise
// enough for a leaderboard and keeps the request path down to a single atomic
// add.  It is also exactly what the day of the board needs: in the last hour of
// a day the oldest bucket that is still held is the midnight that started it.
const hoursWindow = 24

// rankLocation is the timezone the leaderboard's day is aligned to.
//
// It is fixed rather than taken from the host: the users of the service are in
// China, and a server that runs on UTC would otherwise roll the board over at
// eight in the morning local time.
var rankLocation = time.FixedZone("CST", 8*60*60)

// usage is the runtime usage state of a user.  It is persisted separately from
// the user definition.
type usage struct {
	requests    atomic.Int64
	total       atomic.Int64
	periodStart atomic.Int64
	lastSeen    atomic.Int64

	// blocked is the number of queries of the user that a filtering rule
	// rejected.
	blocked atomic.Int64

	// passed is the number of queries of the user that matched a filtering
	// rule but were allowed by the allow-list.
	passed atomic.Int64

	// dayCount is the number of requests counted in the day bucket that
	// dayStart points to.
	dayCount atomic.Int64

	// dayStart is the Unix timestamp of the local midnight of the day bucket
	// that dayCount belongs to.  Zero means that no bucket is open yet.
	dayStart atomic.Int64

	// hourCount is the number of requests counted in the hour bucket that
	// hourStart points to.
	hourCount atomic.Int64

	// hourStart is the Unix timestamp of the start of the hour bucket that
	// hourCount belongs to.  Zero means that no bucket is open yet.
	hourStart atomic.Int64

	// hours maps the Unix timestamp of an hour bucket to the number of
	// requests made in it, for the buckets inside the window above.  Like
	// history it is published as an immutable map so that readers never take
	// a lock; the background flusher is the only writer.
	hours atomic.Pointer[map[int64]int64]

	// hourMicros is the sum of the durations of the queries counted in the
	// hour bucket that hourStart points to, in microseconds, and
	// hourSamples is how many durations that sum is made of.  They are kept
	// apart from hourCount because the two count different populations:
	// hourCount counts the queries the account was allowed to make, while
	// the durations cover every query the statistics module recorded for
	// it, filtering included.
	hourMicros  atomic.Int64
	hourSamples atomic.Int64

	// hourBlocked and hourPassed are the filtering outcome of the queries
	// counted in the hour bucket that hourStart points to.  They exist
	// because the board has a today window: without them the today board
	// could only show the lifetime totals, and one card would then mix a
	// day of requests with a lifetime of filtering.
	hourBlocked atomic.Int64
	hourPassed  atomic.Int64

	// hoursTime maps the Unix timestamp of an hour bucket to the durations
	// counted in it, for the buckets inside the window above.  It is the
	// latency counterpart of hours and is published the same way.
	hoursTime atomic.Pointer[map[int64]timeBucket]

	// totalMicros and totalSamples are the lifetime sums behind the average
	// latency of the account.  They are plain counters rather than buckets
	// because the total figure is not windowed.
	totalMicros  atomic.Int64
	totalSamples atomic.Int64

	// checkinDay is the Unix timestamp of the local midnight of the day of
	// the last check-in.  Zero means that the account has never checked in.
	checkinDay atomic.Int64

	// streak is the number of consecutive days the account has checked in.
	// It restarts at one when a day is missed.
	streak atomic.Int64

	// tempBonus is the temporary request allowance granted by the check-in of
	// tempBonusDay.  It is added to the quota of that day only.
	tempBonus atomic.Int64

	// tempBonusDay is the Unix timestamp of the local midnight of the day
	// tempBonus is valid for.  Zero means that no allowance is active.
	tempBonusDay atomic.Int64

	// logUnlocked is whether the account has earned the query log by keeping
	// up a check-in streak.  It is never cleared once set.
	logUnlocked atomic.Bool

	// history maps a YYYY-MM-DD date to the number of requests made on that
	// day.  It is published atomically as an immutable map so that readers
	// never take a lock.  The background flusher is the only writer.
	history atomic.Pointer[map[string]int64]
}

// usageState is the persistent representation of usage.
type usageState struct {
	Requests    int64 `json:"requests"`
	Total       int64 `json:"total_requests"`
	PeriodStart int64 `json:"period_start"`
	LastSeen    int64 `json:"last_seen"`

	// Blocked is the number of queries of the user that a filtering rule
	// rejected.
	Blocked int64 `json:"blocked,omitempty"`

	// Passed is the number of queries of the user that matched a filtering
	// rule but were allowed by the allow-list.
	Passed int64 `json:"passed,omitempty"`

	// DayStart is the Unix timestamp of the local midnight of the day bucket
	// that is currently open.
	DayStart int64 `json:"day_start,omitempty"`

	// DayCount is the number of requests counted in the open day bucket.  It
	// is persisted so that a restart does not lose the part of the day that
	// has already been counted.
	DayCount int64 `json:"day_count,omitempty"`

	// HourStart is the Unix timestamp of the start of the hour bucket that is
	// currently open.
	HourStart int64 `json:"hour_start,omitempty"`

	// HourCount is the number of requests counted in the open hour bucket.
	HourCount int64 `json:"hour_count,omitempty"`

	// Hours maps the Unix timestamp of an hour bucket to the number of
	// requests made in it, for the buckets inside the window the board reads.
	Hours map[int64]int64 `json:"hours,omitempty"`

	// HourMicros and HourSamples are the duration sums of the hour bucket
	// that is currently open.
	HourMicros  int64 `json:"hour_micros,omitempty"`
	HourSamples int64 `json:"hour_samples,omitempty"`

	// HourBlocked and HourPassed are the filtering outcome of the hour bucket
	// that is currently open.  They are persisted for the same reason the
	// request count of the open bucket is: a counter that only lives in
	// memory is lost on every restart, and the day's figures would then drop
	// the part of the day that has already been counted.
	HourBlocked int64 `json:"hour_blocked,omitempty"`
	HourPassed  int64 `json:"hour_passed,omitempty"`

	// HoursTime is the latency counterpart of Hours: it maps the Unix
	// timestamp of an hour bucket to the durations counted in it.
	HoursTime map[int64]timeBucket `json:"hours_time,omitempty"`

	// TotalMicros and TotalSamples are the lifetime duration sums of the
	// account, which the total board and the account page average.
	TotalMicros  int64 `json:"total_micros,omitempty"`
	TotalSamples int64 `json:"total_samples,omitempty"`

	// CheckinDay is the Unix timestamp of the local midnight of the day of
	// the last check-in.
	CheckinDay int64 `json:"checkin_day,omitempty"`

	// Streak is the number of consecutive check-in days.
	Streak int64 `json:"streak,omitempty"`

	// TempBonus is the temporary request allowance granted by the check-in of
	// TempBonusDay.
	TempBonus int64 `json:"temp_bonus,omitempty"`

	// TempBonusDay is the Unix timestamp of the local midnight of the day
	// TempBonus is valid for.
	TempBonusDay int64 `json:"temp_bonus_day,omitempty"`

	// LogUnlocked is whether the account has earned the query log.
	LogUnlocked bool `json:"log_unlocked,omitempty"`

	// History maps a YYYY-MM-DD date to the number of requests made on that
	// day.
	History map[string]int64 `json:"history,omitempty"`
}

// timeBucket is the sum of the query durations counted in one hour bucket and
// the number of queries it was summed over.  Both are kept because an average
// needs the two, and a bucket with no samples must be distinguishable from one
// whose samples all took zero time.
//
// It also carries the filtering outcome of the same hour.  They ride along in
// this bucket rather than in a second ring because the board reads all three
// figures for one window: keeping them together is what makes "today" mean the
// same thing for the request count, the average and the filtering counts.
type timeBucket struct {
	// Micros is the sum of the durations, in microseconds.
	Micros int64 `json:"micros"`

	// Samples is the number of durations summed into Micros.
	Samples int64 `json:"samples"`

	// Blocked is the number of queries a filtering rule rejected.
	Blocked int64 `json:"blocked,omitempty"`

	// Passed is the number of queries that matched a filtering rule but were
	// allowed by the allow-list.
	Passed int64 `json:"passed,omitempty"`
}

// add folds another bucket into b.
func (b timeBucket) add(other timeBucket) (sum timeBucket) {
	return timeBucket{
		Micros:  b.Micros + other.Micros,
		Samples: b.Samples + other.Samples,
		Blocked: b.Blocked + other.Blocked,
		Passed:  b.Passed + other.Passed,
	}
}

// average returns the mean duration of the bucket in milliseconds, and the
// number of samples behind it.  A bucket without samples has no average, so
// both results are zero.
func (b timeBucket) average() (ms float64, samples int64) {
	if b.Samples <= 0 {
		return 0, 0
	}

	return float64(b.Micros) / float64(b.Samples) / 1000, b.Samples
}

// netEntry is a CIDR network owned by a user.
type netEntry struct {
	prefix netip.Prefix
	entry  *entry
}

// entry pairs a user definition with its runtime usage.
type entry struct {
	user  *User
	usage *usage
}

// snapshot is an immutable view of the user index.  It is published atomically
// so that the DNS request path never takes a lock.
type snapshot struct {
	byID map[string]*entry
	nets []netEntry
	list []*entry
}

// Manager is the user quota manager.  It is safe for concurrent use.
type Manager struct {
	logger *slog.Logger

	// snap is the current immutable index.
	snap atomic.Pointer[snapshot]

	// settings is the manager-wide configuration.  It is replaced atomically so
	// that the request path reads it without a lock.
	settings atomic.Pointer[Settings]

	// mu protects mutations of the persisted state.
	mu sync.Mutex

	// defs holds the user definitions by UID.
	defs map[string]*User

	// usage holds the runtime usage by UID.
	usage map[string]*usage

	// portalPasswords holds the bcrypt hashes of the user portal passwords by
	// UID.  A user without an entry has no portal access.
	//
	// The hashes are deliberately kept out of [User] and [Info], because
	// [Info] embeds [User] and is returned by the administrator API, so a
	// field on the user would end up in every response.  Keeping them in a
	// separate map makes that impossible by construction.
	portalPasswords map[string]string

	// remember holds the remembered devices by the hash of their token.  Only
	// the hash is stored, so the state file is not a set of working
	// credentials.  mu guards it: it is touched when a visitor signs in or
	// comes back, never on the query path.
	remember map[string]*rememberToken

	// path is the path of the state file.
	path string

	// dirty is set when the state has unsaved changes.
	dirty atomic.Bool

	// probesMu protects probes.  It is separate from mu because probes are
	// touched on the query path, which must not contend with the lock that
	// guards persistence.
	probesMu sync.Mutex

	// probes holds the connectivity probes that have not expired yet.
	probes map[string]*probeEntry

	// periodStart caches the start of the current period by period type.
	periodStart map[Period]int64

	// loc is the time zone used to compute period boundaries.
	loc *time.Location

	// domainFunc returns the domain of the DoT/DoH endpoint.  It is optional
	// and is read on every request, so the host can be changed at runtime
	// without restarting the manager.
	domainFunc atomic.Pointer[func() string]

	// now returns the current time.  It is only replaced in tests.
	now func() time.Time

	// done is closed to stop the background goroutine.
	done chan struct{}

	wg sync.WaitGroup
}

// Settings is the manager-wide configuration.  It is immutable once published,
// so the request path reads it without a lock.
type Settings struct {
	// DenyUnmatched is true when a client that does not belong to any user must
	// be refused instead of being let through unaccounted.  Enabling it turns
	// the user list itself into the gate, so removing a user immediately cuts
	// that client off.
	DenyUnmatched bool `json:"deny_unmatched"`

	// UpdateProxy is the GitHub acceleration prefix used by the online
	// updater, for example "https://gh-proxy.com".  It is empty when the
	// updater connects to GitHub directly.  It is not related to users; it
	// lives here because this is the AGHub-wide settings store.
	UpdateProxy string `json:"update_proxy,omitempty"`

	// PortalOrigins are the origins of the user portal front-ends that are
	// allowed to call the portal API from a browser, for example
	// "https://portal.example.com".  A full URL is accepted and reduced to
	// its origin.  When it is empty, only a front-end served by AGHub itself
	// can use the portal, which is the safest default.  It is not related to
	// users; it lives here because this is the AGHub-wide settings store.
	PortalOrigins []string `json:"portal_origins,omitempty"`

	// PortalToken is the deployment token of the portal front-end.  It is
	// generated once and baked into the deployment package; the front-end
	// sends it in the X-Portal-Token header on every call.
	//
	// It replaces the origin allow-list.  A token travels in a header rather
	// than a cookie, so there is nothing for the browser to refuse: no
	// SameSite, no Secure, no origin to register, and the same front-end works
	// over http and https.  It is not a credential for any account -- it only
	// says "this call comes from the portal deployment the administrator
	// built", and the portal users still sign in with their own passwords.
	PortalToken string `json:"portal_token,omitempty"`

	// PortalAPIBase is the address of the portal API as the browser reaches
	// it, for example "https://dns.example.com:3004".  It is baked into the
	// deployment package of the portal front-end, which is served from a
	// different origin.  When it is empty, the front-end talks to the origin
	// it was served from.
	PortalAPIBase string `json:"portal_api_base,omitempty"`

	// PortalOpen is true when anyone may create a portal account.  It is off
	// by default, so a deployment that is not watched cannot be signed up to.
	PortalOpen bool `json:"portal_open,omitempty"`

	// PortalEmailVerify is true when signing up requires proving ownership of
	// an e-mail address.  It has no effect while [Settings.PortalOpen] is
	// false.  It also requires the SMTP settings below to be filled in.
	PortalEmailVerify bool `json:"portal_email_verify,omitempty"`

	// PortalDefaultQuota is the request quota given to a new portal account.
	// Zero means unlimited.
	PortalDefaultQuota int64 `json:"portal_default_quota,omitempty"`

	// PortalDefaultDays is the number of days a new portal account stays
	// valid for.  Zero means that it never expires.
	PortalDefaultDays int64 `json:"portal_default_days,omitempty"`

	// PortalAnnouncement is a free-form message shown to portal users, for
	// example a maintenance notice.  Empty means that there is nothing to
	// show.
	PortalAnnouncement string `json:"portal_announcement,omitempty"`

	// SMTPHost is the host name of the mail server used to send the
	// verification codes of the portal.  Sending is disabled while it is
	// empty.
	SMTPHost string `json:"smtp_host,omitempty"`

	// SMTPPort is the port of the mail server.  Zero means 587.
	SMTPPort int `json:"smtp_port,omitempty"`

	// SMTPUser is the user name for the mail server.
	SMTPUser string `json:"smtp_user,omitempty"`

	// SMTPPassword is the password for the mail server.  It is stored in the
	// state file, which is only readable by the service account.
	//
	// It must never reach a client.  The API does not send the settings
	// themselves but a copy made by [settingsForAPI], in which this field is
	// replaced by a flag; an absent value in a request means "keep the stored
	// one".  A tag of "-" is not an option, because the same struct is what
	// gets written to disk.
	SMTPPassword string `json:"smtp_password,omitempty"`

	// SMTPFrom is the sender address of the verification messages.  Zero
	// value means [Settings.SMTPUser].
	SMTPFrom string `json:"smtp_from,omitempty"`

	// SMTPPlain is true when the connection must be made without STARTTLS.
	// It is only for a mail server on a trusted network.
	SMTPPlain bool `json:"smtp_plain,omitempty"`
}

// Config is the configuration of a Manager.
type Config struct {
	// Logger is used for logging the operation of the manager.  It must not be
	// nil.
	Logger *slog.Logger

	// Path is the path of the state file.  It must not be empty.
	Path string

	// Location is the time zone used to compute period boundaries.  If nil,
	// [time.Local] is used.
	Location *time.Location
}

// New creates a new manager and loads its state from the state file.  A
// missing state file is not an error.
func New(cfg *Config) (m *Manager, err error) {
	if cfg == nil || cfg.Logger == nil {
		return nil, fmt.Errorf("users: logger is nil")
	}

	if cfg.Path == "" {
		return nil, fmt.Errorf("users: state file path is empty")
	}

	loc := cfg.Location
	if loc == nil {
		loc = time.Local
	}

	m = &Manager{
		logger:          cfg.Logger.With(slogutil.KeyPrefix, "users"),
		defs:            map[string]*User{},
		usage:           map[string]*usage{},
		portalPasswords: map[string]string{},
		remember:        map[string]*rememberToken{},
		probes:          map[string]*probeEntry{},
		path:            cfg.Path,
		loc:             loc,
		periodStart:     map[Period]int64{},
		now:             time.Now,
		done:            make(chan struct{}),
	}

	m.settings.Store(&Settings{})

	err = m.load()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.refreshPeriodsLocked(m.now())
	m.publishLocked()
	m.mu.Unlock()

	return m, nil
}

// Start launches the background flusher.
func (m *Manager) Start() {
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				now := m.now()

				m.mu.Lock()
				m.refreshPeriodsLocked(now)
				m.flushHistoryLocked(now)
				m.flushHoursLocked(now)
				m.mu.Unlock()

				if m.dirty.Swap(false) {
					err := m.save()
					if err != nil {
						m.logger.Error("saving user state", "err", err)
					}
				}
			case <-m.done:
				return
			}
		}
	}()
}

// Close stops the background flusher and saves the state.
func (m *Manager) Close() (err error) {
	select {
	case <-m.done:
		// Already closed.
	default:
		close(m.done)
	}

	m.wg.Wait()

	m.dirty.Store(false)

	return m.save()
}

// NewUID returns a new random user identifier.
func NewUID() (uid string, err error) {
	b := make([]byte, 8)

	_, err = rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// MinPortalPasswordLen is the shortest password accepted for a portal account.
const MinPortalPasswordLen = 8

// maxEmailLen is the longest accepted e-mail address.
const maxEmailLen = 254

// NormalizeEmail validates an e-mail address and returns its canonical form.
// An empty input is not an error and yields an empty string.
func NormalizeEmail(email string) (norm string, err error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}

	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("users: invalid e-mail address")
	}

	norm = strings.ToLower(addr.Address)
	if len(norm) > maxEmailLen {
		return "", fmt.Errorf("users: e-mail address is too long")
	}

	return norm, nil
}

// RegisterParams are the parameters of [Manager.Register].
type RegisterParams struct {
	// Name is the optional display name.  It defaults to the identifier.
	Name string

	// Password is the password of the new account.
	Password string

	// Email is the optional e-mail address.
	Email string

	// EmailVerified is true when the caller has already proved that the
	// address belongs to the registrant.  An address that has not been proved
	// is still stored, so that the user can prove it later, but it does not
	// count as verified.
	EmailVerified bool
}

// Register creates an account for a portal user.  The identifier is generated
// here, is unique, and never changes; it is also what the user puts in front of
// the domain of the DoT and DoH endpoints, so the account and the DNS identity
// are the same thing by construction.
//
// The quota and the validity period come from the AGHub-wide settings, so the
// administrator controls what a new account costs without touching the code.
func (m *Manager) Register(p *RegisterParams) (u *User, err error) {
	if p == nil {
		return nil, fmt.Errorf("users: no parameters")
	}

	password := p.Password
	if len(password) < MinPortalPasswordLen {
		return nil, fmt.Errorf(
			"users: password must be at least %d characters",
			MinPortalPasswordLen,
		)
	}

	email, err := NormalizeEmail(p.Email)
	if err != nil {
		return nil, err
	}

	if email != "" && m.FindByEmail(email) != nil {
		return nil, fmt.Errorf("users: e-mail address is already registered")
	}

	uid, err := m.freeUID()
	if err != nil {
		return nil, err
	}

	s := m.GetSettings()

	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = uid
	}

	var limit *int64
	if s.PortalDefaultQuota > 0 {
		l := s.PortalDefaultQuota
		limit = &l
	}

	// The generated identifier is the only identifier of the account, which is
	// what makes the binding between the account and the DNS identity unique.
	u, err = m.Add(&AddParams{
		UID:          uid,
		Name:         name,
		IDs:          []string{uid},
		RequestLimit: limit,
		Period:       PeriodDay,
		ExpireDays:   s.PortalDefaultDays,
		Enabled:      true,
	})
	if err != nil {
		return nil, err
	}

	u.Email = email
	u.EmailVerified = email != "" && p.EmailVerified

	err = m.SetPortalPassword(uid, password)
	if err != nil {
		// The account would be unusable without a password, so it must not be
		// left behind half-created.
		_ = m.Remove(uid)

		return nil, err
	}

	m.mu.Lock()
	m.dirty.Store(true)
	m.mu.Unlock()

	return u, nil
}

// freeUID returns a generated identifier that no user holds.
func (m *Manager) freeUID() (uid string, err error) {
	for range 8 {
		uid, err = NewUID()
		if err != nil {
			return "", err
		}

		if m.Get(uid) == nil {
			return uid, nil
		}
	}

	return "", fmt.Errorf("users: could not generate a free identifier")
}

// FindByEmail returns the user holding the e-mail address, or nil.  The address
// is compared in its canonical form.
func (m *Manager) FindByEmail(email string) (u *User) {
	norm, err := NormalizeEmail(email)
	if err != nil || norm == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, def := range m.defs {
		if strings.EqualFold(def.Email, norm) {
			return def
		}
	}

	return nil
}

// SetEmail sets the e-mail address of the user.  An empty address clears it.
// The address must not belong to another user.
func (m *Manager) SetEmail(uid, email string, verified bool) (err error) {
	norm, err := NormalizeEmail(email)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	def, ok := m.defs[uid]
	if !ok {
		return fmt.Errorf("users: no user with identifier %q", uid)
	}

	if norm != "" {
		for otherUID, other := range m.defs {
			if otherUID != uid && strings.EqualFold(other.Email, norm) {
				return fmt.Errorf("users: e-mail address is already registered")
			}
		}
	}

	def.Email = norm
	// A new address is never verified on its own, even when the previous one
	// was: the proof belonged to the old address.
	def.EmailVerified = norm != "" && verified

	m.publishLocked()
	m.dirty.Store(true)

	return nil
}

// normalizePeriod returns a valid period, defaulting to [PeriodDay].
func normalizePeriod(p Period) (norm Period) {
	switch p {
	case PeriodDay, PeriodMonth, PeriodTotal:
		return p
	default:
		return PeriodDay
	}
}

// normalizeIDs cleans up and validates the identifiers of a user.
func normalizeIDs(ids []string) (norm []string, err error) {
	norm = make([]string, 0, len(ids))

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		err = validateID(id)
		if err != nil {
			return nil, err
		}

		if !slices.Contains(norm, id) {
			norm = append(norm, id)
		}
	}

	return norm, nil
}

// validateID validates a single identifier, which may be an IP address, a CIDR
// network, or an opaque ClientID.
func validateID(id string) (err error) {
	if _, err = netip.ParseAddr(id); err == nil {
		return nil
	}

	if _, err = netip.ParsePrefix(id); err == nil {
		return nil
	}

	return validateClientID(id)
}

// validateClientID validates an opaque ClientID, which is how DoH and DoT
// clients are identified.
//
// The rule is deliberately the same one AdGuard Home applies to ClientIDs of
// persistent clients and to the labels it slices out of a DoT/DoQ server name
// ([netutil.ValidateHostnameLabel]).  Anything this function accepts is
// something AdGuard Home can actually produce; anything looser would let an
// administrator save an identifier that can never match, and — for the SNI
// path — that makes the whole query fail with SERVFAIL rather than being
// refused.
func validateClientID(id string) (err error) {
	err = netutil.ValidateHostnameLabel(id)
	if err != nil {
		return fmt.Errorf("users: invalid client id: %w", err)
	}

	return nil
}

// publishLocked rebuilds the immutable index from the definitions.
//
// m.mu is expected to be held by the caller.
func (m *Manager) publishLocked() {
	snap := &snapshot{
		byID: make(map[string]*entry, len(m.defs)),
		list: make([]*entry, 0, len(m.defs)),
	}

	for uid, u := range m.defs {
		us := m.usage[uid]
		if us == nil {
			us = &usage{}
			m.usage[uid] = us
		}

		e := &entry{user: u, usage: us}
		snap.list = append(snap.list, e)

		for _, id := range u.IDs {
			prefix, perr := netip.ParsePrefix(id)
			if perr == nil {
				snap.nets = append(snap.nets, netEntry{prefix: prefix.Masked(), entry: e})

				continue
			}

			snap.byID[id] = e
		}
	}

	slices.SortFunc(snap.list, func(a, b *entry) (cmp int) {
		return strings.Compare(a.user.Name, b.user.Name)
	})

	// Longer prefixes are checked first so that the most specific network
	// wins.
	slices.SortStableFunc(snap.nets, func(a, b netEntry) (cmp int) {
		return b.prefix.Bits() - a.prefix.Bits()
	})

	m.snap.Store(snap)
}

// match returns the entry of the user owning the given identifiers, if any.
// It never takes a lock.
func (m *Manager) match(clientID string, ip netip.Addr) (e *entry) {
	snap := m.snap.Load()
	if snap == nil {
		return nil
	}

	if clientID != "" {
		e = snap.byID[clientID]
		if e != nil {
			return e
		}
	}

	if !ip.IsValid() {
		return nil
	}

	e = snap.byID[ip.WithZone("").String()]
	if e != nil {
		return e
	}

	for _, ne := range snap.nets {
		if ne.prefix.Contains(ip.WithZone("")) {
			return ne.entry
		}
	}

	return nil
}

// AllowQuery checks whether the query from the given client is allowed and, if
// so, counts it.  It returns the reason for the refusal, which is empty when
// the query is allowed.
//
// A client that belongs to no user is allowed and left uncounted unless
// [Settings.DenyUnmatched] is set, in which case the user list acts as the gate.
func (m *Manager) AllowQuery(
	clientID string,
	ip netip.Addr,
	qname string,
) (ok bool, reason string) {
	// The probe is recorded before the allow decision, because a device
	// pointed at us has proved what the portal asked it to prove even when
	// the answer is a refusal: an account over its quota is still connected.
	m.matchProbe(qname, clientID, ip)

	e := m.match(clientID, ip)
	if e == nil {
		if s := m.settings.Load(); s != nil && s.DenyUnmatched {
			return false, ReasonUnmatched
		}

		return true, ""
	}

	u := e.user

	if !u.Enabled {
		return false, ReasonManual
	}

	now := m.now().Unix()

	if u.ExpiresAt > 0 && now >= u.ExpiresAt {
		return false, ReasonExpired
	}

	if u.RequestLimit >= 0 {
		start := m.periodStartOf(u.Period)
		if e.usage.periodStart.Load() != start {
			e.usage.periodStart.Store(start)
			e.usage.requests.Store(0)
		}

		limit := u.RequestLimit
		if limit > 0 {
			// The check-in allowance is valid for one local day.  For the
			// common per-day quota that day is the period itself, so the
			// boundary that was already fetched above is reused.  A longer
			// period does not reset daily, but the allowance still expires
			// with the day, so its own boundary is looked up.
			today := start
			if u.Period != PeriodDay {
				today = m.periodStartOf(PeriodDay)
			}

			limit += e.usage.tempBonusOn(today)
		}

		if e.usage.requests.Load() >= limit {
			return false, ReasonQuota
		}
	}

	e.usage.requests.Add(1)
	e.usage.total.Add(1)
	e.usage.dayCount.Add(1)
	e.usage.hourCount.Add(1)
	e.usage.lastSeen.Store(now)

	m.dirty.Store(true)

	return true, ""
}

// RecordResult counts the filtering outcome of a query for the user that owns
// clientID.  blocked is true when a filtering rule rejected the query, and
// passed is true when a rule matched but the query was allowed by the
// allow-list, which is the count that shows how much a user's own rules would
// have caught if the allow-list had not saved it.
//
// It is called on the DNS query path, so it looks the user up in the immutable
// snapshot without taking a lock, exactly like [Manager.AllowQuery].  A client
// that belongs to no user is dropped rather than turned into a new one: the
// manager must never grow from ordinary traffic.
func (m *Manager) RecordResult(clientID string, blocked, passed bool) {
	if clientID == "" || (!blocked && !passed) {
		return
	}

	e := m.matchByClientID(clientID)
	if e == nil {
		return
	}

	if blocked {
		e.usage.blocked.Add(1)
		e.usage.hourBlocked.Add(1)
	}

	if passed {
		e.usage.passed.Add(1)
		e.usage.hourPassed.Add(1)
	}

	m.dirty.Store(true)
}

// ObserveLatency counts the duration of one resolved query for the user that
// owns the client.
//
// It is called on the DNS query path, once the response is known, so it looks
// the user up in the immutable snapshot without taking a lock, exactly like
// [Manager.AllowQuery].  A client that belongs to no user is dropped: the
// manager must never grow from ordinary traffic, and the service-wide average
// the portal shows for those queries comes from the statistics module.
func (m *Manager) ObserveLatency(clientID string, ip netip.Addr, d time.Duration) {
	if d < 0 {
		return
	}

	e := m.match(clientID, ip)
	if e == nil {
		return
	}

	e.usage.recordLatency(d)

	m.dirty.Store(true)
}

// matchByClientID returns the entry of the user owning the given client
// identifier, if any.  It never takes a lock.
func (m *Manager) matchByClientID(clientID string) (e *entry) {
	snap := m.snap.Load()
	if snap == nil {
		return nil
	}

	return snap.byID[clientID]
}

// periodStartOf returns the cached start of the current period.
func (m *Manager) periodStartOf(p Period) (start int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.periodStartOfLocked(p)
}

// periodStartOfLocked returns the cached start of the current period.
//
// m.mu is expected to be held by the caller.
func (m *Manager) periodStartOfLocked(p Period) (start int64) {
	start, ok := m.periodStart[p]
	if !ok {
		start = periodStart(m.now(), m.loc, p)
		m.periodStart[p] = start
	}

	return start
}

// refreshPeriodsLocked recomputes the cached period boundaries.
//
// m.mu is expected to be held by the caller.
func (m *Manager) refreshPeriodsLocked(now time.Time) {
	for _, p := range []Period{PeriodDay, PeriodMonth, PeriodTotal} {
		m.periodStart[p] = periodStart(now, m.loc, p)
	}
}

// flushHistoryLocked moves the request counters of the users into their daily
// usage history and drops the history that is out of the retention window.
//
// The day buckets are rotated here rather than in the DNS request path so that
// the latter stays free of locks.  The trade-off is that up to one flush
// interval worth of requests made right after local midnight is attributed to
// the previous day, which is acceptable for a usage report.
//
// m.mu is expected to be held by the caller.
func (m *Manager) flushHistoryLocked(now time.Time) {
	today := periodStart(now, m.loc, PeriodDay)

	for _, e := range m.usage {
		start := e.dayStart.Load()
		if start == today {
			continue
		}

		if start != 0 {
			requests := e.dayCount.Swap(0)

			// A user who has never made a request gets no history at all.
			// Once there is one, idle days are recorded as zeros so that the
			// gaps are visible in the report.
			if requests > 0 || e.history.Load() != nil {
				date := time.Unix(start, 0).In(m.loc).Format(time.DateOnly)
				e.recordHistory(date, requests)
			}
		}

		// When no bucket is open yet, which happens between the start of the
		// process and the first flush, adopt the current day without touching
		// the counter: the requests that were already counted happened within
		// this flush interval, so they belong to this bucket.
		e.dayStart.Store(today)

		// The rotation changed the state, and no DNS request has to arrive for
		// it to need saving: an idle user still gets a bucket written for the
		// day that ended.  Without this the entry stays in memory only and is
		// lost if the process does not shut down cleanly.
		m.dirty.Store(true)
	}
}

// flushHoursLocked rotates the hourly request buckets of the users and drops
// the ones that left the window the board reads.
//
// Like the day buckets, the rotation happens here rather than on the DNS
// request path, so that path stays free of locks.  A request that lands in the
// half minute after the hour turns is attributed to the hour that just ended,
// which is invisible on a figure that covers a whole day.
//
// m.mu is expected to be held by the caller.
func (m *Manager) flushHoursLocked(now time.Time) {
	hour := hourStartOf(now)

	for _, e := range m.usage {
		start := e.hourStart.Load()
		if start == hour {
			continue
		}

		if start != 0 {
			e.recordHour(start, e.hourCount.Swap(0))
			e.recordHourTime(start, timeBucket{
				Micros:  e.hourMicros.Swap(0),
				Samples: e.hourSamples.Swap(0),
				Blocked: e.hourBlocked.Swap(0),
				Passed:  e.hourPassed.Swap(0),
			})
		}

		// As with the day bucket, adopting the current hour without
		// touching the counter keeps the requests counted since the last
		// flush, which were made within this hour.
		e.hourStart.Store(hour)

		// The rotation alone makes the entry worth saving: the hour that
		// ended has to reach the disk even if no request follows.
		m.dirty.Store(true)
	}
}

// hourStartOf returns the Unix timestamp of the start of the hour containing t.
func hourStartOf(t time.Time) (start int64) {
	return t.Unix() - t.Unix()%hourSeconds
}

// recordHour adds one closed hour to the ring and drops the hours that are now
// outside the window.
func (e *usage) recordHour(start, requests int64) {
	next := map[int64]int64{start: requests}

	if prev := e.hours.Load(); prev != nil {
		for h, r := range *prev {
			if _, ok := next[h]; !ok {
				next[h] = r
			}
		}
	}

	cutoff := start - (hoursWindow-1)*hourSeconds
	for h := range next {
		if h < cutoff {
			delete(next, h)
		}
	}

	e.hours.Store(&next)
}

// requestsToday returns the number of requests the user made since midnight in
// [rankLocation], the hour in progress included.
//
// The figure is summed from the hourly buckets rather than read off the day
// counter, because that counter follows the timezone of the host while the
// board has to roll over at Beijing midnight whatever the host is set to.
func (e *usage) requestsToday(now time.Time) (n int64) {
	start := dayStartOf(now, rankLocation)

	if h := e.hours.Load(); h != nil {
		for s, r := range *h {
			if s >= start {
				n += r
			}
		}
	}

	// The bucket in progress is counted live, so the figure moves as requests
	// come in instead of only on the next rotation.  A bucket that was never
	// opened counts as well: between the start of the process and the first
	// rotation there is no hour to compare against, and the requests already
	// counted were made within the hour that is running.
	//
	// A bucket that belongs to an earlier day is left out: those requests are
	// already in the ring, and counting them twice would be worse than the
	// half minute of attribution the rotation costs.
	if s := e.hourStart.Load(); s == 0 || s >= start {
		n += e.hourCount.Load()
	}

	return n
}

// recordHourTime adds one closed hour of durations to the ring and drops the
// buckets that are now outside the window.
func (e *usage) recordHourTime(start int64, b timeBucket) {
	next := map[int64]timeBucket{start: b}

	if prev := e.hoursTime.Load(); prev != nil {
		for h, v := range *prev {
			if _, ok := next[h]; !ok {
				next[h] = v
			}
		}
	}

	cutoff := start - (hoursWindow-1)*hourSeconds
	for h := range next {
		if h < cutoff {
			delete(next, h)
		}
	}

	e.hoursTime.Store(&next)
}

// recordLatency adds the duration of one query to the open hour bucket and to
// the lifetime sums.
//
// It is called on the DNS query path, after the response, so it only touches
// atomics.
func (e *usage) recordLatency(d time.Duration) {
	if d < 0 {
		return
	}

	micros := d.Microseconds()

	e.hourMicros.Add(micros)
	e.hourSamples.Add(1)
	e.totalMicros.Add(micros)
	e.totalSamples.Add(1)
}

// latencyToday returns the mean query duration of the account since midnight
// in [rankLocation], in milliseconds, and the number of queries behind it.
//
// It mirrors [usage.requestsToday] and therefore answers for the same window
// the board counts in, so the two figures on one card describe one period.
func (e *usage) latencyToday(now time.Time) (ms float64, samples int64) {
	return e.todayBucket(now).average()
}

// todayBucket returns the sum of the hour buckets that fall inside the day in
// progress in [rankLocation], the hour in progress included.
//
// Every per-day figure the board shows is read off this one sum.  Deriving them
// together is what keeps a card self-consistent: the request count, the average
// latency and the filtering counts then all describe the same period, instead
// of three of them following the board's window and the fourth reporting a
// lifetime total.
func (e *usage) todayBucket(now time.Time) (sum timeBucket) {
	start := dayStartOf(now, rankLocation)

	if h := e.hoursTime.Load(); h != nil {
		for s, b := range *h {
			if s >= start {
				sum = sum.add(b)
			}
		}
	}

	// The hour in progress has not been rotated into the ring yet, so it is
	// added separately.  hourStart is 0 right after a start, before the first
	// flush has opened a bucket; that hour is the current one either way.
	if s := e.hourStart.Load(); s == 0 || s >= start {
		sum = sum.add(timeBucket{
			Micros:  e.hourMicros.Load(),
			Samples: e.hourSamples.Load(),
			Blocked: e.hourBlocked.Load(),
			Passed:  e.hourPassed.Load(),
		})
	}

	return sum
}

// latencyTotal returns the mean query duration of the account over its whole
// life, in milliseconds, and the number of queries behind it.
func (e *usage) latencyTotal() (ms float64, samples int64) {
	return timeBucket{
		Micros:  e.totalMicros.Load(),
		Samples: e.totalSamples.Load(),
	}.average()
}

// dayStartOf returns the Unix timestamp of midnight of the day that contains t
// in loc.
func dayStartOf(t time.Time, loc *time.Location) (start int64) {
	y, mo, d := t.In(loc).Date()

	return time.Date(y, mo, d, 0, 0, 0, 0, loc).Unix()
}

// recordHistory adds a day of usage to the history, publishing a new immutable
// map so that concurrent readers do not need a lock.
func (e *usage) recordHistory(date string, requests int64) {
	next := map[string]int64{date: requests}

	if prev := e.history.Load(); prev != nil {
		for d, r := range *prev {
			if _, ok := next[d]; !ok {
				next[d] = r
			}
		}
	}

	if len(next) > historyDays {
		dates := make([]string, 0, len(next))
		for d := range next {
			dates = append(dates, d)
		}

		// The keys are YYYY-MM-DD dates, so sorting them lexicographically
		// also sorts them chronologically.
		slices.Sort(dates)

		for _, d := range dates[:len(dates)-historyDays] {
			delete(next, d)
		}
	}

	e.history.Store(&next)
}

// historyOf returns the usage history of the user, oldest first.
//
// The current day is appended when the user has been counted at least once.
// The rotation only moves the days that have passed into the history, so
// without it a user would see a chart that does not contain the very day they
// are looking at.
func (e *usage) historyOf(loc *time.Location, now time.Time) (points []UsagePoint) {
	h := e.history.Load()
	if h != nil {
		points = make([]UsagePoint, 0, len(*h)+1)
		for date, requests := range *h {
			points = append(points, UsagePoint{Date: date, Requests: requests})
		}

		slices.SortFunc(points, func(a, b UsagePoint) (cmp int) {
			return strings.Compare(a.Date, b.Date)
		})
	}

	// A user who has never made a request gets no history at all, so the
	// current day is only added once there is one.  dayStart alone is not
	// enough to tell: the rotation opens a bucket for every user, active or
	// not.
	//
	// The bucket may not be open yet, which is the case for a user created
	// after the last rotation.  The requests they just made are already
	// counted, so fall back to the current day rather than showing them an
	// empty chart.
	start := e.dayStart.Load()
	if len(points) > 0 || e.dayCount.Load() > 0 {
		day := now.In(loc)
		if start != 0 {
			day = time.Unix(start, 0).In(loc)
		}

		today := day.Format(time.DateOnly)
		if len(points) == 0 || points[len(points)-1].Date != today {
			points = append(points, UsagePoint{Date: today, Requests: e.dayCount.Load()})
		}
	}

	if len(points) > historyDays {
		points = points[len(points)-historyDays:]
	}

	return points
}

// periodStart returns the Unix timestamp of the start of the period containing
// t.
func periodStart(t time.Time, loc *time.Location, p Period) (start int64) {
	t = t.In(loc)

	switch p {
	case PeriodMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).Unix()
	case PeriodTotal:
		return 0
	default:
		y, mo, d := t.Date()

		return time.Date(y, mo, d, 0, 0, 0, 0, loc).Unix()
	}
}

// nextPeriodStart returns the Unix timestamp at which the period containing t
// ends, or zero when the period never ends.  The boundaries are the same
// midnight-aligned ones that [periodStart] uses, so the moment a user is
// created has no influence on when the counter is reset.
func nextPeriodStart(t time.Time, loc *time.Location, p Period) (next int64) {
	t = t.In(loc)

	switch p {
	case PeriodMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0).Unix()
	case PeriodTotal:
		return 0
	default:
		y, mo, d := t.Date()

		return time.Date(y, mo, d, 0, 0, 0, 0, loc).AddDate(0, 0, 1).Unix()
	}
}

// UsagePoint is the number of requests a user made on a single day.
type UsagePoint struct {
	// Date is the local date in YYYY-MM-DD format.
	Date string `json:"date"`

	// Requests is the number of requests made on that day.
	Requests int64 `json:"requests"`
}

// Info is the state of a user as reported to the API.
type Info struct {
	// User is the user definition.
	*User

	// Requests is the number of requests in the current period.
	Requests int64 `json:"requests"`

	// TotalRequests is the number of requests since creation.
	TotalRequests int64 `json:"total_requests"`

	// RequestsToday is the number of requests of the day in progress, counted
	// from midnight in [rankLocation].
	RequestsToday int64 `json:"requests_today"`

	// AvgLatencyTodayMS is the mean duration of the queries of the day in
	// progress, in milliseconds, and LatencyTodaySamples is how many queries
	// that average was computed from.  The average is zero when there are no
	// samples, which is why the count is reported next to it: a page must be
	// able to tell "no data" from "instant".
	AvgLatencyTodayMS   float64 `json:"avg_latency_today_ms"`
	LatencyTodaySamples int64   `json:"latency_today_samples"`

	// AvgLatencyMS and LatencySamples are the same figures over the whole
	// life of the account.
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
	LatencySamples int64   `json:"latency_samples"`

	// Blocked is the number of queries that a filtering rule rejected.
	Blocked int64 `json:"blocked"`

	// Passed is the number of queries that matched a filtering rule but were
	// allowed by the allow-list.
	Passed int64 `json:"passed"`

	// BlockedToday and PassedToday are the same two figures over the day in
	// progress, counted from midnight in [rankLocation].  They exist for the
	// same reason the latency has a today variant: a page that reports a day
	// of requests must not put a lifetime of filtering next to it.
	BlockedToday int64 `json:"blocked_today"`
	PassedToday  int64 `json:"passed_today"`

	// PeriodStart is the Unix timestamp of the start of the current period.
	PeriodStart int64 `json:"period_start"`

	// LastSeen is the Unix timestamp of the last allowed request.
	LastSeen int64 `json:"last_seen"`

	// Status is the computed status of the user.
	Status string `json:"status"`

	// DisabledReason is the reason the user is not allowed to query, if any.
	DisabledReason string `json:"disabled_reason,omitempty"`

	// RemainingSeconds is the number of seconds until the expiration date.  It
	// is [Unlimited] when the user never expires.
	RemainingSeconds int64 `json:"remaining_seconds"`

	// RemainingRequests is the number of requests left in the current period.
	// It is [Unlimited] when the user has no quota.
	RemainingRequests int64 `json:"remaining_requests"`

	// NextReset is the Unix timestamp at which the current accounting period
	// ends and the request counter goes back to zero.  It is zero for a user
	// whose quota is counted over the whole lifetime.
	NextReset int64 `json:"next_reset"`

	// History is the per-day request count of the user, oldest first.  It is
	// limited to the most recent [historyDays] days.
	History []UsagePoint `json:"history,omitempty"`

	// HasPortalPassword is true when the user can sign in to the user portal.
	// The password itself is never exposed.
	HasPortalPassword bool `json:"has_portal_password"`
}

// info builds the API representation of an entry.
func (m *Manager) info(e *entry) (i *Info) {
	u := e.user
	now := m.now().Unix()

	// The temporary check-in allowance is part of the quota of the day it was
	// granted for, so the remaining count and the over-quota status must use
	// the same effective limit as [Manager.AllowQuery] does.
	limit := u.RequestLimit
	if limit > 0 {
		limit += e.usage.tempBonusOn(periodStart(m.now(), m.loc, PeriodDay))
	}

	i = &Info{
		User:              u,
		Requests:          e.usage.requests.Load(),
		TotalRequests:     e.usage.total.Load(),
		RequestsToday:     e.usage.requestsToday(m.now()),
		Blocked:           e.usage.blocked.Load(),
		Passed:            e.usage.passed.Load(),
		PeriodStart:       e.usage.periodStart.Load(),
		LastSeen:          e.usage.lastSeen.Load(),
		NextReset:         nextPeriodStart(m.now(), m.loc, u.Period),
		History:           e.usage.historyOf(m.loc, m.now()),
		HasPortalPassword: m.HasPortalPassword(u.UID),
	}

	// One pass over the hour ring yields every per-day figure, so all four of
	// them describe the same window and a card cannot mix periods.
	today := e.usage.todayBucket(m.now())
	i.AvgLatencyTodayMS, i.LatencyTodaySamples = today.average()
	i.BlockedToday, i.PassedToday = today.Blocked, today.Passed

	i.AvgLatencyMS, i.LatencySamples = e.usage.latencyTotal()

	if u.ExpiresAt > 0 {
		i.RemainingSeconds = max(0, u.ExpiresAt-now)
	} else {
		i.RemainingSeconds = Unlimited
	}

	if u.RequestLimit >= 0 {
		i.RemainingRequests = max(0, limit-i.Requests)
	} else {
		i.RemainingRequests = Unlimited
	}

	switch {
	case !u.Enabled:
		i.Status = StatusDisabled
		i.DisabledReason = ReasonManual
	case u.ExpiresAt > 0 && now >= u.ExpiresAt:
		i.Status = StatusExpired
		i.DisabledReason = ReasonExpired
	case u.RequestLimit >= 0 && i.Requests >= limit:
		i.Status = StatusOverQuota
		i.DisabledReason = ReasonQuota
	case u.ExpiresAt > 0 && i.RemainingSeconds <= expiringSoonThreshold:
		i.Status = StatusExpiringSoon
	default:
		i.Status = StatusActive
	}

	return i
}

// List returns the state of all users.
func (m *Manager) List() (infos []*Info) {
	snap := m.snap.Load()
	if snap == nil {
		return nil
	}

	infos = make([]*Info, 0, len(snap.list))
	for _, e := range snap.list {
		infos = append(infos, m.info(e))
	}

	return infos
}

// Summary is the aggregate state of all users.
type Summary struct {
	// Total is the total number of users.
	Total int `json:"total"`

	// Active is the number of users that may query.
	Active int `json:"active"`

	// Disabled is the number of users disabled by the administrator.
	Disabled int `json:"disabled"`

	// Expired is the number of users whose subscription has expired.
	Expired int `json:"expired"`

	// OverQuota is the number of users that have exhausted their quota.
	OverQuota int `json:"over_quota"`

	// ExpiringSoon is the number of users expiring within three days.
	ExpiringSoon int `json:"expiring_soon"`

	// Requests is the total number of requests in the current period.
	Requests int64 `json:"requests"`

	// TotalRequests is the total number of requests since creation.
	TotalRequests int64 `json:"total_requests"`

	// Blocked is the total number of queries that a filtering rule rejected
	// over all users.
	Blocked int64 `json:"blocked"`

	// Passed is the total number of queries that matched a filtering rule
	// but were allowed by the allow-list over all users.
	Passed int64 `json:"passed"`
}

// Summary returns the aggregate state of all users.
func (m *Manager) Summary() (s *Summary) {
	s = &Summary{}

	for _, i := range m.List() {
		s.Total++
		s.Requests += i.Requests
		s.TotalRequests += i.TotalRequests
		s.Blocked += i.Blocked
		s.Passed += i.Passed

		switch i.Status {
		case StatusDisabled:
			s.Disabled++
		case StatusExpired:
			s.Expired++
		case StatusOverQuota:
			s.OverQuota++
		case StatusExpiringSoon:
			s.ExpiringSoon++
			s.Active++
		default:
			s.Active++
		}
	}

	return s
}

// Path returns the path of the state file.
func (m *Manager) Path() (p string) {
	return m.path
}

// GetSettings returns a copy of the manager-wide settings.
func (m *Manager) GetSettings() (s *Settings) {
	cur := m.settings.Load()
	if cur == nil {
		return &Settings{}
	}

	// Return a copy so that a caller cannot change the stored settings by
	// writing through the pointer it was handed.
	cp := *cur
	cp.PortalOrigins = slices.Clone(cur.PortalOrigins)

	return &cp
}

// SetSettings replaces the manager-wide settings.
func (m *Manager) SetSettings(s *Settings) (err error) {
	if s == nil {
		return fmt.Errorf("users: settings are nil")
	}

	if w := SettingsWarning(s); w != "" {
		m.logger.Info("user settings look wrong", "warning", w)
	}

	// Store the whole struct.  Rebuilding it field by field silently drops
	// every field that is not named here, which is how the portal settings
	// were lost on every save.
	stored := *s
	stored.PortalOrigins = slices.Clone(s.PortalOrigins)
	m.settings.Store(&stored)

	m.dirty.Store(true)

	m.logger.Info(
		"user settings updated",
		"deny_unmatched", s.DenyUnmatched,
		"portal_origins", len(s.PortalOrigins),
	)

	return nil
}

// isPlainHTTP reports whether raw is an http:// URL.
// PortalTokenLength is the length of the portal deployment token in hex
// characters.
const PortalTokenLength = 32

// EnsurePortalToken returns the portal deployment token, generating and storing
// one when there is none yet.
func (m *Manager) EnsurePortalToken() (tok string, err error) {
	if cur := m.GetSettings(); cur.PortalToken != "" {
		return cur.PortalToken, nil
	}

	raw := make([]byte, PortalTokenLength/2)
	_, err = rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("users: generating the portal token: %w", err)
	}

	tok = hex.EncodeToString(raw)

	s := *m.GetSettings()
	s.PortalToken = tok
	m.settings.Store(&s)
	m.dirty.Store(true)

	m.logger.Info("portal deployment token generated")

	return tok, nil
}

// RotatePortalToken replaces the portal deployment token with a new one.  The
// previously downloaded front-end stops working, which is the point: it is how
// a leaked deployment package is cut off.
func (m *Manager) RotatePortalToken() (tok string, err error) {
	raw := make([]byte, PortalTokenLength/2)
	_, err = rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("users: generating the portal token: %w", err)
	}

	tok = hex.EncodeToString(raw)

	s := *m.GetSettings()
	s.PortalToken = tok
	m.settings.Store(&s)
	m.dirty.Store(true)

	m.logger.Info("portal deployment token rotated")

	return tok, nil
}

// CheckPortalToken reports whether tok is the current portal deployment token.
func (m *Manager) CheckPortalToken(tok string) (ok bool) {
	if tok == "" {
		return false
	}

	cur := m.GetSettings().PortalToken

	return cur != "" && subtle.ConstantTimeCompare([]byte(cur), []byte(tok)) == 1
}

// SettingsWarning describes a settings combination that is almost certainly a
// deployment mistake, or returns an empty string when there is nothing to say.
//
// It is a warning and not an error on purpose.  A plain HTTP API address next
// to an HTTPS origin does break the browser-direct deployment: the request is
// blocked as mixed content, and the cross-origin session cookie is dropped
// because SameSite=None requires Secure.  But it is harmless when the browser
// talks to a same-origin back-end instead -- the PHP back-end that ships in
// the portal package, or an nginx proxy -- because then the browser never uses
// that address at all.  Refusing to store the settings made the portal
// impossible to configure for exactly those deployments, which are the ones
// that cannot put a certificate on the AGHub host.
func SettingsWarning(s *Settings) (w string) {
	if s == nil || !isPlainHTTP(s.PortalAPIBase) || !hasHTTPSOrigin(s.PortalOrigins) {
		return ""
	}

	return "portal_api_base is plain HTTP while portal_origins has an HTTPS " +
		"origin. If the browser calls the API directly this cannot work: the " +
		"request is blocked as mixed content and the session cookie is dropped. " +
		"It is harmless when the browser talks to a same-origin back-end " +
		"instead - leave portal_api_base empty for that, or point it at a PHP " +
		"or nginx layer in front of AGHub. To let the browser call AGHub " +
		"directly, serve the API over HTTPS with --web-tls-cert."
}

func isPlainHTTP(raw string) (ok bool) {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "http://")
}

// hasHTTPSOrigin reports whether any of the origins is served over HTTPS.
func hasHTTPSOrigin(origins []string) (ok bool) {
	for _, o := range origins {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(o)), "https://") {
			return true
		}
	}

	return false
}

// SetUpdateProxy stores the online-update acceleration prefix, keeping the
// other settings as they are.
func (m *Manager) SetUpdateProxy(prefix string) (err error) {
	cur := m.GetSettings()

	// Store the whole settings, not just the two fields this call is about:
	// a fresh struct would silently drop the portal origins and the portal
	// API address.
	cur.UpdateProxy = prefix
	m.settings.Store(cur)
	m.dirty.Store(true)

	m.logger.Info("online update proxy updated", "proxy", prefix)

	return nil
}

// SetDomainFunc sets the function that returns the domain of the DoT/DoH
// endpoint.  The domain is reported by the API so that the UI can build the
// full host name of a client identifier.  It must be called before the manager
// starts serving requests.
func (m *Manager) SetDomainFunc(f func() (domain string)) {
	if f == nil {
		m.domainFunc.Store(nil)

		return
	}

	m.domainFunc.Store(&f)
}

// Domain returns the domain of the DoT/DoH endpoint, or an empty string when
// none is configured.
func (m *Manager) Domain() (domain string) {
	f := m.domainFunc.Load()
	if f == nil {
		return ""
	}

	return (*f)()
}

// Get returns the user with the given UID, if any.
func (m *Manager) Get(uid string) (u *User) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.defs[uid]
}

// AddParams are the parameters of a new user.
type AddParams struct {
	// UID is the identifier of the user.  When it is empty, one is generated.
	// It is only set by callers that must know the identifier in advance,
	// such as the portal sign-up.
	UID string

	// Name is the human-readable name of the user.
	Name string

	// Remark is an optional free-form note.
	Remark string

	// IDs are the identifiers of the user.
	IDs []string

	// RequestLimit is the request quota per period.  Nil means unlimited.
	RequestLimit *int64

	// Period is the quota accounting period.
	Period Period

	// ExpireDays is the number of days until the subscription expires.  Zero
	// or negative means that it never expires.
	ExpireDays int64

	// Enabled is the initial administrator-controlled switch.
	Enabled bool
}

// Add creates a new user and returns it.
func (m *Manager) Add(p *AddParams) (u *User, err error) {
	if p == nil {
		return nil, fmt.Errorf("users: no parameters")
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, fmt.Errorf("users: name is empty")
	}

	ids, err := normalizeIDs(p.IDs)
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("users: no valid identifiers")
	}

	uid := strings.TrimSpace(p.UID)
	if uid == "" {
		uid, err = NewUID()
		if err != nil {
			return nil, err
		}
	}

	now := m.now()

	limit := Unlimited
	if p.RequestLimit != nil {
		limit = max(Unlimited, *p.RequestLimit)
	}

	u = &User{
		UID:          uid,
		Name:         name,
		Remark:       strings.TrimSpace(p.Remark),
		IDs:          ids,
		RequestLimit: limit,
		Period:       normalizePeriod(p.Period),
		CreatedAt:    now.Unix(),
		Enabled:      p.Enabled,
	}

	if p.ExpireDays > 0 {
		u.ExpiresAt = now.AddDate(0, 0, int(p.ExpireDays)).Unix()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	clash, owner := m.clashLocked(uid, ids)
	if clash != "" {
		return nil, fmt.Errorf("users: identifier %q is already used by user %q", clash, owner)
	}

	m.defs[uid] = u
	m.usage[uid] = &usage{}
	m.publishLocked()
	m.dirty.Store(true)

	return u, nil
}

// UpdateParams are the parameters of an update.  Nil fields are left
// unchanged.
type UpdateParams struct {
	// Name is the new name of the user.
	Name *string

	// Remark is the new note of the user.
	Remark *string

	// IDs are the new identifiers of the user.
	IDs *[]string

	// RequestLimit is the new request quota.
	RequestLimit *int64

	// Period is the new accounting period.
	Period *Period

	// ExpireDays sets the number of days until the subscription expires,
	// counted from now.  It is ignored when ExtendDays is set.
	ExpireDays *int64

	// ExtendDays adds the given number of days to the expiration date.  The
	// expiration date is extended from the current one when it is in the
	// future and from now otherwise.
	ExtendDays *int64

	// Enabled is the new administrator-controlled switch.
	Enabled *bool
}

// Update modifies an existing user.
func (m *Manager) Update(uid string, p *UpdateParams) (u *User, err error) {
	if p == nil {
		return nil, fmt.Errorf("users: no parameters")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prev, ok := m.defs[uid]
	if !ok {
		return nil, fmt.Errorf("users: no user with uid %q", uid)
	}

	next := *prev
	next.IDs = slices.Clone(prev.IDs)

	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return nil, fmt.Errorf("users: name is empty")
		}

		next.Name = name
	}

	if p.Remark != nil {
		next.Remark = strings.TrimSpace(*p.Remark)
	}

	if p.IDs != nil {
		var ids []string
		ids, err = normalizeIDs(*p.IDs)
		if err != nil {
			return nil, err
		}

		if len(ids) == 0 {
			return nil, fmt.Errorf("users: no valid identifiers")
		}

		clash, owner := m.clashLocked(uid, ids)
		if clash != "" {
			return nil, fmt.Errorf("users: identifier %q is already used by user %q", clash, owner)
		}

		next.IDs = ids
	}

	if p.RequestLimit != nil {
		next.RequestLimit = max(Unlimited, *p.RequestLimit)
	}

	if p.Period != nil {
		next.Period = normalizePeriod(*p.Period)
	}

	now := m.now()

	switch {
	case p.ExtendDays != nil:
		base := next.ExpiresAt
		if base < now.Unix() {
			base = now.Unix()
		}

		next.ExpiresAt = base + *p.ExtendDays*secsPerDay
		if next.ExpiresAt < now.Unix() {
			next.ExpiresAt = 0
		}
	case p.ExpireDays != nil:
		if *p.ExpireDays > 0 {
			next.ExpiresAt = now.AddDate(0, 0, int(*p.ExpireDays)).Unix()
		} else {
			next.ExpiresAt = 0
		}
	}

	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}

	m.defs[uid] = &next
	m.publishLocked()
	m.dirty.Store(true)

	return &next, nil
}

// Remove deletes the user with the given UID.
func (m *Manager) Remove(uid string) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.defs[uid]; !ok {
		return fmt.Errorf("users: no user with uid %q", uid)
	}

	delete(m.defs, uid)
	delete(m.usage, uid)
	delete(m.portalPasswords, uid)
	m.forgetRememberLocked(uid, "")
	m.publishLocked()
	m.dirty.Store(true)

	return nil
}

// Reset resets the request counter of the given users for the current period.
func (m *Manager) Reset(uids []string) (n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, uid := range uids {
		u, ok := m.defs[uid]
		if !ok {
			continue
		}

		us := m.usage[uid]
		if us == nil {
			continue
		}

		us.requests.Store(0)
		us.periodStart.Store(m.periodStartOfLocked(normalizePeriod(u.Period)))
		n++
	}

	if n > 0 {
		m.dirty.Store(true)
	}

	return n
}

// SetEnabled enables or disables the given users.
func (m *Manager) SetEnabled(uids []string, enabled bool) (n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, uid := range uids {
		u, ok := m.defs[uid]
		if !ok || u.Enabled == enabled {
			continue
		}

		next := *u
		next.IDs = slices.Clone(u.IDs)
		next.Enabled = enabled
		m.defs[uid] = &next
		n++
	}

	if n > 0 {
		m.publishLocked()
		m.dirty.Store(true)
	}

	return n
}

// SetPortalPassword sets the password the user signs in to the user portal
// with.  It returns an error if the password does not meet the length limits.
func (m *Manager) SetPortalPassword(uid, password string) (err error) {
	return m.setPortalPassword(uid, password, "")
}

// SetPortalPasswordKeeping is [Manager.SetPortalPassword] for the visitor
// changing their own password: the device they are on stays signed in, every
// other one is signed out.  It exists because a token that outlived the
// password is a way back into the account, and because signing the visitor out
// of the page they are typing on is not a security feature.
func (m *Manager) SetPortalPasswordKeeping(uid, password, keepHash string) (err error) {
	return m.setPortalPassword(uid, password, keepHash)
}

// setPortalPassword is the shared body of the two exported setters.  keepHash
// is the hash of the one remembered device to spare, if any.
func (m *Manager) setPortalPassword(uid, password, keepHash string) (err error) {
	if len(password) < minPortalPasswordLen {
		return fmt.Errorf("users: password is shorter than %d characters", minPortalPasswordLen)
	}

	if len(password) > maxPortalPasswordLen {
		return fmt.Errorf("users: password is longer than %d characters", maxPortalPasswordLen)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("users: hashing the password: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.defs[uid]; !ok {
		return fmt.Errorf("users: no user with uid %q", uid)
	}

	m.portalPasswords[uid] = string(hash)

	// A remembered device that outlived the password is a way back into the
	// account, so the password and the devices it was set against change
	// together.
	m.forgetRememberLocked(uid, keepHash)

	m.dirty.Store(true)

	return nil
}

// ClearPortalPassword revokes the portal access of the user.
func (m *Manager) ClearPortalPassword(uid string) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.defs[uid]; !ok {
		return fmt.Errorf("users: no user with uid %q", uid)
	}

	delete(m.portalPasswords, uid)
	m.forgetRememberLocked(uid, "")
	m.dirty.Store(true)

	return nil
}

// HasPortalPassword returns true if the user can sign in to the user portal.
func (m *Manager) HasPortalPassword(uid string) (ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok = m.portalPasswords[uid]

	return ok
}

// FindByLogin returns the user matching the given portal login, or nil.  The
// login is matched against the identifiers first, since they are unique, then
// against the e-mail address, which is unique as well, and finally against the
// name, which is only used when it is unambiguous.
//
// The comparison is case-insensitive, because the identifiers are host name
// labels and the users type them by hand.
func (m *Manager) FindByLogin(login string) (u *User) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var byName []*User

	for _, def := range m.defs {
		for _, id := range def.IDs {
			if strings.ToLower(id) == login {
				return def
			}
		}

		// The e-mail address is stored normalised, so a direct comparison is
		// enough.  It is matched before the name because it is unique while
		// the name is not.
		if def.Email != "" && def.Email == login {
			return def
		}

		if strings.ToLower(def.Name) == login {
			byName = append(byName, def)
		}
	}

	if len(byName) == 1 {
		return byName[0]
	}

	// Either no user at all or several sharing the name.  Both are refused,
	// because signing in as the wrong user would leak their query log.
	return nil
}

// AuthenticatePortal returns true if password is the portal password of the
// user with the given UID.  The comparison always runs, even for a user
// without a password, so that the response time does not reveal whether the
// login exists.
func (m *Manager) AuthenticatePortal(uid, password string) (ok bool) {
	m.mu.Lock()
	hash, hasPassword := m.portalPasswords[uid]
	m.mu.Unlock()

	if !hasPassword {
		// An invalid bcrypt hash, so the comparison below fails after doing
		// the same amount of work.
		hash = dummyPasswordHash
	}

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyPasswordHash is a syntactically valid bcrypt hash used to keep the
// duration of a failed login constant.  Its value is irrelevant.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// clashLocked returns the identifier that is already used by another user as
// well as the name of that user, or empty strings.  m.mu is expected to be
// held by the caller.
func (m *Manager) clashLocked(uid string, ids []string) (clash, owner string) {
	for otherUID, other := range m.defs {
		if otherUID == uid {
			continue
		}

		for _, id := range ids {
			if slices.Contains(other.IDs, id) {
				return id, other.Name
			}
		}
	}

	return "", ""
}
