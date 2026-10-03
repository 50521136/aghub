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
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/netutil"
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
}

// historyDays is the number of days of per-user usage history that is kept.
const historyDays = 30

// usage is the runtime usage state of a user.  It is persisted separately from
// the user definition.
type usage struct {
	requests    atomic.Int64
	total       atomic.Int64
	periodStart atomic.Int64
	lastSeen    atomic.Int64

	// dayCount is the number of requests counted in the day bucket that
	// dayStart points to.
	dayCount atomic.Int64

	// dayStart is the Unix timestamp of the local midnight of the day bucket
	// that dayCount belongs to.  Zero means that no bucket is open yet.
	dayStart atomic.Int64

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

	// DayStart is the Unix timestamp of the local midnight of the day bucket
	// that is currently open.
	DayStart int64 `json:"day_start,omitempty"`

	// DayCount is the number of requests counted in the open day bucket.  It
	// is persisted so that a restart does not lose the part of the day that
	// has already been counted.
	DayCount int64 `json:"day_count,omitempty"`

	// History maps a YYYY-MM-DD date to the number of requests made on that
	// day.
	History map[string]int64 `json:"history,omitempty"`
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

	// path is the path of the state file.
	path string

	// dirty is set when the state has unsaved changes.
	dirty atomic.Bool

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
		logger:      cfg.Logger.With(slogutil.KeyPrefix, "users"),
		defs:        map[string]*User{},
		usage:       map[string]*usage{},
		path:        cfg.Path,
		loc:         loc,
		periodStart: map[Period]int64{},
		now:         time.Now,
		done:        make(chan struct{}),
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
func (m *Manager) AllowQuery(clientID string, ip netip.Addr) (ok bool, reason string) {
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

		if e.usage.requests.Load() >= u.RequestLimit {
			return false, ReasonQuota
		}
	}

	e.usage.requests.Add(1)
	e.usage.total.Add(1)
	e.usage.dayCount.Add(1)
	e.usage.lastSeen.Store(now)

	m.dirty.Store(true)

	return true, ""
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
func (e *usage) historyOf() (points []UsagePoint) {
	h := e.history.Load()
	if h == nil {
		return nil
	}

	points = make([]UsagePoint, 0, len(*h))
	for date, requests := range *h {
		points = append(points, UsagePoint{Date: date, Requests: requests})
	}

	slices.SortFunc(points, func(a, b UsagePoint) (cmp int) {
		return strings.Compare(a.Date, b.Date)
	})

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
}

// info builds the API representation of an entry.
func (m *Manager) info(e *entry) (i *Info) {
	u := e.user
	now := m.now().Unix()

	i = &Info{
		User:          u,
		Requests:      e.usage.requests.Load(),
		TotalRequests: e.usage.total.Load(),
		PeriodStart:   e.usage.periodStart.Load(),
		LastSeen:      e.usage.lastSeen.Load(),
		NextReset:     nextPeriodStart(m.now(), m.loc, u.Period),
		History:       e.usage.historyOf(),
	}

	if u.ExpiresAt > 0 {
		i.RemainingSeconds = max(0, u.ExpiresAt-now)
	} else {
		i.RemainingSeconds = Unlimited
	}

	if u.RequestLimit >= 0 {
		i.RemainingRequests = max(0, u.RequestLimit-i.Requests)
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
	case u.RequestLimit >= 0 && i.Requests >= u.RequestLimit:
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
}

// Summary returns the aggregate state of all users.
func (m *Manager) Summary() (s *Summary) {
	s = &Summary{}

	for _, i := range m.List() {
		s.Total++
		s.Requests += i.Requests
		s.TotalRequests += i.TotalRequests

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

	return &Settings{
		DenyUnmatched: cur.DenyUnmatched,
		UpdateProxy:   cur.UpdateProxy,
	}
}

// SetSettings replaces the manager-wide settings.
func (m *Manager) SetSettings(s *Settings) (err error) {
	if s == nil {
		return fmt.Errorf("users: settings are nil")
	}

	m.settings.Store(&Settings{
		DenyUnmatched: s.DenyUnmatched,
		UpdateProxy:   s.UpdateProxy,
	})
	m.dirty.Store(true)

	m.logger.Info("user settings updated", "deny_unmatched", s.DenyUnmatched)

	return nil
}

// SetUpdateProxy stores the online-update acceleration prefix, keeping the
// other settings as they are.
func (m *Manager) SetUpdateProxy(prefix string) (err error) {
	cur := m.GetSettings()

	m.settings.Store(&Settings{
		DenyUnmatched: cur.DenyUnmatched,
		UpdateProxy:   prefix,
	})
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

	uid, err := NewUID()
	if err != nil {
		return nil, err
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
