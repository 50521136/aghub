package users

import (
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testLogger returns a logger that discards everything.
func testLogger() (l *slog.Logger) {
	return slog.New(slog.DiscardHandler)
}

// newTestManager returns a manager with a state file inside a temporary
// directory and a controllable clock.
func newTestManager(t *testing.T) (m *Manager, now *time.Time) {
	t.Helper()

	clock := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	now = &clock

	m, err := New(&Config{
		Logger:   testLogger(),
		Path:     filepath.Join(t.TempDir(), "users.json"),
		Location: time.UTC,
	})
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	m.now = func() (t time.Time) { return *now }

	err = m.refreshPeriods()
	if err != nil {
		t.Fatalf("refreshing periods: %v", err)
	}

	return m, now
}

// refreshPeriods recomputes the cached period boundaries for the current
// clock.
func (m *Manager) refreshPeriods() (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.refreshPeriodsLocked(m.now())

	return nil
}

// iptr returns a pointer to v.
func iptr(v int64) (p *int64) {
	return &v
}

// addUser is a test helper that adds a user and fails the test on error.
func addUser(t *testing.T, m *Manager, p *AddParams) (u *User) {
	t.Helper()

	u, err := m.Add(p)
	if err != nil {
		t.Fatalf("adding user: %v", err)
	}

	return u
}

func TestManagerAddAndList(t *testing.T) {
	m, _ := newTestManager(t)

	u := addUser(t, m, &AddParams{
		Name:         "alice",
		IDs:          []string{"192.168.1.10"},
		RequestLimit: iptr(100),
		Period:       PeriodDay,
		ExpireDays:   30,
		Enabled:      true,
	})

	if u.UID == "" {
		t.Fatal("uid is empty")
	}

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 user, got %d", len(list))
	}

	got := list[0]
	if got.Name != "alice" {
		t.Errorf("expected name alice, got %q", got.Name)
	}

	if got.Status != StatusActive {
		t.Errorf("expected status %q, got %q", StatusActive, got.Status)
	}

	if got.RequestLimit != 100 {
		t.Errorf("expected limit 100, got %d", got.RequestLimit)
	}

	if got.RemainingSeconds != 30*secsPerDay {
		t.Errorf("expected 30 days remaining, got %d", got.RemainingSeconds)
	}
}

func TestManagerAddErrors(t *testing.T) {
	m, _ := newTestManager(t)

	testCases := []struct {
		name string
		p    *AddParams
	}{{
		name: "no_name",
		p:    &AddParams{IDs: []string{"192.168.1.1"}, Enabled: true},
	}, {
		name: "no_ids",
		p:    &AddParams{Name: "a", Enabled: true},
	}, {
		name: "bad_id",
		p:    &AddParams{Name: "a", IDs: []string{"not a valid id!"}, Enabled: true},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Add(tc.p)
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestManagerClashingIDs(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{Name: "a", IDs: []string{"10.0.0.1"}, Enabled: true})

	_, err := m.Add(&AddParams{Name: "b", IDs: []string{"10.0.0.1"}, Enabled: true})
	if err == nil {
		t.Fatal("expected a clash error")
	}
}

func TestManagerQuotaEnforcement(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "quota",
		IDs:          []string{"192.168.1.20"},
		RequestLimit: iptr(3),
		Period:       PeriodDay,
		Enabled:      true,
	})

	ip := netip.MustParseAddr("192.168.1.20")

	for i := range 3 {
		ok, reason := m.AllowQuery("", ip)
		if !ok {
			t.Fatalf("request %d: expected allow, got refuse with %q", i+1, reason)
		}
	}

	ok, reason := m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the 4th request to be refused")
	}

	if reason != ReasonQuota {
		t.Errorf("expected reason %q, got %q", ReasonQuota, reason)
	}

	// A different client must not be affected.
	other := netip.MustParseAddr("192.168.1.21")
	ok, _ = m.AllowQuery("", other)
	if !ok {
		t.Error("expected an unrelated client to be allowed")
	}
}

func TestManagerQuotaPeriodReset(t *testing.T) {
	m, now := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "daily",
		IDs:          []string{"192.168.1.30"},
		RequestLimit: iptr(1),
		Period:       PeriodDay,
		Enabled:      true,
	})

	ip := netip.MustParseAddr("192.168.1.30")

	ok, _ := m.AllowQuery("", ip)
	if !ok {
		t.Fatal("expected the first request to be allowed")
	}

	ok, reason := m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the second request to be refused")
	}

	if reason != ReasonQuota {
		t.Fatalf("expected reason %q, got %q", ReasonQuota, reason)
	}

	// Move to the next day and refresh the period boundaries.
	*now = now.Add(24 * time.Hour)
	if err := m.refreshPeriods(); err != nil {
		t.Fatalf("refreshing periods: %v", err)
	}

	ok, reason = m.AllowQuery("", ip)
	if !ok {
		t.Fatalf("expected the request to be allowed after the reset, got %q", reason)
	}

	if got := m.List()[0].Requests; got != 1 {
		t.Errorf("expected 1 request in the new period, got %d", got)
	}
}

func TestManagerTotalPeriodNeverResets(t *testing.T) {
	m, now := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "total",
		IDs:          []string{"192.168.1.31"},
		RequestLimit: iptr(1),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	ip := netip.MustParseAddr("192.168.1.31")

	ok, _ := m.AllowQuery("", ip)
	if !ok {
		t.Fatal("expected the first request to be allowed")
	}

	*now = now.Add(48 * time.Hour)
	if err := m.refreshPeriods(); err != nil {
		t.Fatalf("refreshing periods: %v", err)
	}

	ok, reason := m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the second request to be refused")
	}

	if reason != ReasonQuota {
		t.Errorf("expected reason %q, got %q", ReasonQuota, reason)
	}
}

func TestManagerExpiry(t *testing.T) {
	m, now := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:       "expiring",
		IDs:        []string{"192.168.1.40"},
		Period:     PeriodDay,
		ExpireDays: 1,
		Enabled:    true,
	})

	ip := netip.MustParseAddr("192.168.1.40")

	ok, _ := m.AllowQuery("", ip)
	if !ok {
		t.Fatal("expected the request to be allowed before expiry")
	}

	*now = now.Add(25 * time.Hour)

	ok, reason := m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the request to be refused after expiry")
	}

	if reason != ReasonExpired {
		t.Errorf("expected reason %q, got %q", ReasonExpired, reason)
	}

	if got := m.List()[0].Status; got != StatusExpired {
		t.Errorf("expected status %q, got %q", StatusExpired, got)
	}
}

func TestManagerManualDisable(t *testing.T) {
	m, _ := newTestManager(t)

	u := addUser(t, m, &AddParams{
		Name:    "off",
		IDs:     []string{"192.168.1.50"},
		Period:  PeriodDay,
		Enabled: false,
	})

	ip := netip.MustParseAddr("192.168.1.50")

	ok, reason := m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the request to be refused")
	}

	if reason != ReasonManual {
		t.Errorf("expected reason %q, got %q", ReasonManual, reason)
	}

	if n := m.SetEnabled([]string{u.UID}, true); n != 1 {
		t.Fatalf("expected 1 updated user, got %d", n)
	}

	ok, reason = m.AllowQuery("", ip)
	if !ok {
		t.Fatalf("expected the request to be allowed after enabling, got %q", reason)
	}
}

func TestManagerCIDRMatching(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "subnet",
		IDs:          []string{"10.20.0.0/16"},
		RequestLimit: iptr(1),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	ok, _ := m.AllowQuery("", netip.MustParseAddr("10.20.5.5"))
	if !ok {
		t.Fatal("expected an address in the subnet to be matched")
	}

	ok, _ = m.AllowQuery("", netip.MustParseAddr("10.20.5.6"))
	if ok {
		t.Fatal("expected the quota to be shared by the whole subnet")
	}

	ok, _ = m.AllowQuery("", netip.MustParseAddr("10.21.5.5"))
	if !ok {
		t.Error("expected an address outside the subnet to be allowed")
	}
}

func TestManagerClientIDMatching(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "doh",
		IDs:          []string{"my-doh-client"},
		RequestLimit: iptr(1),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	ok, _ := m.AllowQuery("my-doh-client", netip.Addr{})
	if !ok {
		t.Fatal("expected the ClientID to be matched")
	}

	ok, _ = m.AllowQuery("my-doh-client", netip.Addr{})
	if ok {
		t.Fatal("expected the quota to be exhausted")
	}

	ok, _ = m.AllowQuery("other-client", netip.Addr{})
	if !ok {
		t.Error("expected an unrelated ClientID to be allowed")
	}
}

func TestManagerReset(t *testing.T) {
	m, _ := newTestManager(t)

	u := addUser(t, m, &AddParams{
		Name:         "reset",
		IDs:          []string{"192.168.1.60"},
		RequestLimit: iptr(1),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	ip := netip.MustParseAddr("192.168.1.60")

	ok, _ := m.AllowQuery("", ip)
	if !ok {
		t.Fatal("expected the first request to be allowed")
	}

	ok, _ = m.AllowQuery("", ip)
	if ok {
		t.Fatal("expected the second request to be refused")
	}

	if n := m.Reset([]string{u.UID}); n != 1 {
		t.Fatalf("expected 1 reset user, got %d", n)
	}

	ok, reason := m.AllowQuery("", ip)
	if !ok {
		t.Fatalf("expected the request to be allowed after the reset, got %q", reason)
	}

	if got := m.List()[0].TotalRequests; got != 2 {
		t.Errorf("expected 2 total requests, got %d", got)
	}
}

func TestManagerUpdate(t *testing.T) {
	m, _ := newTestManager(t)

	u := addUser(t, m, &AddParams{
		Name:         "update",
		IDs:          []string{"192.168.1.70"},
		RequestLimit: iptr(10),
		Period:       PeriodDay,
		ExpireDays:   1,
		Enabled:      true,
	})

	name := "updated"
	extend := int64(30)
	limit := int64(Unlimited)

	_, err := m.Update(u.UID, &UpdateParams{
		Name:         &name,
		ExtendDays:   &extend,
		RequestLimit: &limit,
	})
	if err != nil {
		t.Fatalf("updating user: %v", err)
	}

	got := m.List()[0]
	if got.Name != "updated" {
		t.Errorf("expected name %q, got %q", "updated", got.Name)
	}

	if got.RequestLimit != Unlimited {
		t.Errorf("expected unlimited, got %d", got.RequestLimit)
	}

	// The expiration date is extended from the current one because it is in
	// the future: 1 day left + 30 days.
	if got.RemainingSeconds != 31*secsPerDay {
		t.Errorf("expected 31 days remaining, got %d", got.RemainingSeconds)
	}
}

func TestManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	clock := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	m, err := New(&Config{Logger: testLogger(), Path: path, Location: time.UTC})
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	m.now = func() (t time.Time) { return clock }

	u := addUser(t, m, &AddParams{
		Name:         "persisted",
		Remark:       "note",
		IDs:          []string{"192.168.1.80", "10.0.0.0/8"},
		RequestLimit: iptr(5),
		Period:       PeriodMonth,
		ExpireDays:   10,
		Enabled:      true,
	})

	ok, _ := m.AllowQuery("", netip.MustParseAddr("192.168.1.80"))
	if !ok {
		t.Fatal("expected the request to be allowed")
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("closing manager: %v", err)
	}

	if _, err = os.Stat(path); err != nil {
		t.Fatalf("state file was not written: %v", err)
	}

	reloaded, err := New(&Config{Logger: testLogger(), Path: path, Location: time.UTC})
	if err != nil {
		t.Fatalf("reloading manager: %v", err)
	}

	reloaded.now = func() (t time.Time) { return clock }

	list := reloaded.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 user after reload, got %d", len(list))
	}

	got := list[0]
	if got.UID != u.UID {
		t.Errorf("expected uid %q, got %q", u.UID, got.UID)
	}

	if got.Name != "persisted" || got.Remark != "note" {
		t.Errorf("unexpected name or remark: %q, %q", got.Name, got.Remark)
	}

	if got.RequestLimit != 5 || got.Period != PeriodMonth {
		t.Errorf("unexpected limit or period: %d, %q", got.RequestLimit, got.Period)
	}

	if got.Requests != 1 {
		t.Errorf("expected 1 request after reload, got %d", got.Requests)
	}

	if got.TotalRequests != 1 {
		t.Errorf("expected 1 total request after reload, got %d", got.TotalRequests)
	}
}

func TestManagerRemove(t *testing.T) {
	m, _ := newTestManager(t)

	u := addUser(t, m, &AddParams{Name: "gone", IDs: []string{"192.168.1.90"}, Enabled: true})

	err := m.Remove(u.UID)
	if err != nil {
		t.Fatalf("removing user: %v", err)
	}

	if len(m.List()) != 0 {
		t.Fatal("expected no users after removal")
	}

	ip := netip.MustParseAddr("192.168.1.90")
	ok, _ := m.AllowQuery("", ip)
	if !ok {
		t.Error("expected an unknown client to be allowed")
	}
}

func TestManagerImportUsers(t *testing.T) {
	m, _ := newTestManager(t)

	n, err := m.ImportUsers([]*User{{
		Name:         "imported",
		IDs:          []string{"172.16.0.1"},
		RequestLimit: 42,
		Period:       PeriodMonth,
		ExpiresAt:    m.now().Add(5 * 24 * time.Hour).Unix(),
		CreatedAt:    m.now().Unix(),
		Enabled:      true,
	}})
	if err != nil {
		t.Fatalf("importing users: %v", err)
	}

	if n != 1 {
		t.Fatalf("expected 1 imported user, got %d", n)
	}

	got := m.List()[0]
	if got.Name != "imported" || got.RequestLimit != 42 {
		t.Errorf("unexpected user: %+v", got.User)
	}

	// A second import must replace the previous users.
	n, err = m.ImportUsers([]*User{{
		Name:    "other",
		IDs:     []string{"172.16.0.2"},
		Period:  PeriodDay,
		Enabled: true,
	}})
	if err != nil {
		t.Fatalf("importing users: %v", err)
	}

	if n != 1 || len(m.List()) != 1 {
		t.Fatalf("expected the import to replace users, got %d", len(m.List()))
	}

	if m.List()[0].Name != "other" {
		t.Errorf("expected the user to be replaced, got %q", m.List()[0].Name)
	}
}

func TestManagerImportUsersDuplicatedIDs(t *testing.T) {
	m, _ := newTestManager(t)

	_, err := m.ImportUsers([]*User{{
		Name: "a",
		IDs:  []string{"172.16.0.1"},
	}, {
		Name: "b",
		IDs:  []string{"172.16.0.1"},
	}})
	if err == nil {
		t.Fatal("expected a duplicated identifier error")
	}
}

func TestManagerSummary(t *testing.T) {
	m, now := newTestManager(t)

	addUser(t, m, &AddParams{Name: "a", IDs: []string{"10.1.0.1"}, Enabled: true})
	addUser(t, m, &AddParams{Name: "b", IDs: []string{"10.1.0.2"}, Enabled: false})
	addUser(t, m, &AddParams{
		Name:       "c",
		IDs:        []string{"10.1.0.3"},
		ExpireDays: 1,
		Enabled:    true,
	})

	*now = now.Add(48 * time.Hour)

	s := m.Summary()
	if s.Total != 3 {
		t.Fatalf("expected 3 users, got %d", s.Total)
	}

	if s.Active != 1 {
		t.Errorf("expected 1 active user, got %d", s.Active)
	}

	if s.Disabled != 1 {
		t.Errorf("expected 1 disabled user, got %d", s.Disabled)
	}

	if s.Expired != 1 {
		t.Errorf("expected 1 expired user, got %d", s.Expired)
	}
}

func TestPeriodStart(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	at := time.Date(2026, 10, 2, 23, 30, 0, 0, loc)

	testCases := []struct {
		name   string
		want   time.Time
		period Period
	}{{
		name:   "day",
		period: PeriodDay,
		want:   time.Date(2026, 10, 2, 0, 0, 0, 0, loc),
	}, {
		name:   "month",
		period: PeriodMonth,
		want:   time.Date(2026, 10, 1, 0, 0, 0, 0, loc),
	}, {
		name:   "total",
		period: PeriodTotal,
		want:   time.Unix(0, 0),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := periodStart(at, loc, tc.period)
			if got != tc.want.Unix() {
				t.Errorf("expected %d, got %d", tc.want.Unix(), got)
			}
		})
	}
}

func TestNormalizePeriod(t *testing.T) {
	testCases := []struct {
		in   Period
		want Period
	}{
		{in: PeriodDay, want: PeriodDay},
		{in: PeriodMonth, want: PeriodMonth},
		{in: PeriodTotal, want: PeriodTotal},
		{in: "", want: PeriodDay},
		{in: "year", want: PeriodDay},
	}

	for _, tc := range testCases {
		if got := normalizePeriod(tc.in); got != tc.want {
			t.Errorf("normalizePeriod(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
