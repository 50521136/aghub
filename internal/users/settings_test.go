package users

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerDenyUnmatched(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "alice",
		IDs:          []string{"alice", "192.168.1.10"},
		RequestLimit: iptr(10),
		Period:       PeriodDay,
		Enabled:      true,
	})

	ip := netip.MustParseAddr("10.0.0.1")

	// By default an unknown client is allowed and not counted.
	ok, reason := m.AllowQuery("nobody", ip, "")
	if !ok || reason != "" {
		t.Fatalf("expected unmatched client to be allowed, got ok=%v reason=%q", ok, reason)
	}

	// A known client is still governed by its own quota.
	ok, reason = m.AllowQuery("alice", ip, "")
	if !ok || reason != "" {
		t.Fatalf("expected alice to be allowed, got ok=%v reason=%q", ok, reason)
	}

	err := m.SetSettings(&Settings{DenyUnmatched: true})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	ok, reason = m.AllowQuery("nobody", ip, "")
	if ok || reason != ReasonUnmatched {
		t.Fatalf("expected unmatched client to be refused, got ok=%v reason=%q", ok, reason)
	}

	// The known client must be unaffected by the switch.
	ok, reason = m.AllowQuery("alice", ip, "")
	if !ok || reason != "" {
		t.Fatalf("expected alice to still be allowed, got ok=%v reason=%q", ok, reason)
	}

	// Turning it back off restores the permissive behaviour.
	err = m.SetSettings(&Settings{DenyUnmatched: false})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	ok, reason = m.AllowQuery("nobody", ip, "")
	if !ok || reason != "" {
		t.Fatalf("expected unmatched client to be allowed again, got ok=%v reason=%q", ok, reason)
	}
}

func TestManagerDenyUnmatchedCountsNothing(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "alice",
		IDs:          []string{"alice"},
		RequestLimit: iptr(5),
		Period:       PeriodTotal,
		Enabled:      true,
	})

	err := m.SetSettings(&Settings{DenyUnmatched: true})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	ip := netip.MustParseAddr("10.0.0.1")

	for range 3 {
		ok, reason := m.AllowQuery("nobody", ip, "")
		if ok || reason != ReasonUnmatched {
			t.Fatalf("expected refusal, got ok=%v reason=%q", ok, reason)
		}
	}

	for _, i := range m.List() {
		if i.Requests != 0 {
			t.Fatalf("refused queries must not be counted, %q has %d", i.Name, i.Requests)
		}
	}
}

func TestManagerSettingsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	newMgr := func() (m *Manager) {
		t.Helper()

		var err error
		m, err = New(&Config{Logger: testLogger(), Path: path})
		if err != nil {
			t.Fatalf("creating manager: %v", err)
		}

		return m
	}

	m := newMgr()

	if s := m.GetSettings(); s.DenyUnmatched {
		t.Fatal("expected deny_unmatched to default to false")
	}

	err := m.SetSettings(&Settings{DenyUnmatched: true})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("closing manager: %v", err)
	}

	reopened := newMgr()
	if s := reopened.GetSettings(); !s.DenyUnmatched {
		t.Fatal("expected deny_unmatched to survive a restart")
	}
}

func TestValidateID(t *testing.T) {
	valid := []string{
		"192.168.1.10",
		"10.0.0.0/24",
		"a",
		"alice",
		"lisi-doh",
		"user123",
		"a1-b2-c3",
	}

	for _, id := range valid {
		if err := validateID(id); err != nil {
			t.Errorf("expected %q to be valid, got %v", id, err)
		}
	}

	// These are the shapes that make AdGuard Home fail the whole query with
	// SERVFAIL when it slices the ClientID out of a DoT/DoQ server name, so
	// they must not be storable.
	invalid := []string{
		"",
		"a_b",
		"ab-",
		"-ab",
		"a.b",
		"a@b",
		"a:b",
		"a/b",
		"a b",
		strings.Repeat("a", 64),
	}

	for _, id := range invalid {
		if err := validateID(id); err == nil {
			t.Errorf("expected %q to be invalid", id)
		}
	}
}

func TestSetUpdateProxyKeepsTheOtherSettings(t *testing.T) {
	m, _ := newTestManager(t)

	err := m.SetSettings(&Settings{
		DenyUnmatched: true,
		PortalOrigins: []string{"https://portal.example.com"},
		PortalAPIBase: "https://dns.example.com:3004",
	})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	err = m.SetUpdateProxy("https://gh-proxy.com")
	if err != nil {
		t.Fatalf("setting update proxy: %v", err)
	}

	// Changing the update proxy must not touch the rest of the settings.
	s := m.GetSettings()
	if s.UpdateProxy != "https://gh-proxy.com" {
		t.Errorf("expected the new proxy, got %q", s.UpdateProxy)
	}
	if !s.DenyUnmatched {
		t.Error("expected deny_unmatched to be kept")
	}
	if len(s.PortalOrigins) != 1 || s.PortalOrigins[0] != "https://portal.example.com" {
		t.Errorf("expected the portal origins to be kept, got %q", s.PortalOrigins)
	}
	if s.PortalAPIBase != "https://dns.example.com:3004" {
		t.Errorf("expected the portal API address to be kept, got %q", s.PortalAPIBase)
	}
}

func TestPortalAPIBasePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	newMgr := func() (m *Manager) {
		t.Helper()

		var err error
		m, err = New(&Config{Logger: testLogger(), Path: path})
		if err != nil {
			t.Fatalf("creating manager: %v", err)
		}

		return m
	}

	m := newMgr()

	err := m.SetSettings(&Settings{PortalAPIBase: "https://dns.example.com:3004"})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("closing manager: %v", err)
	}

	reopened := newMgr()
	if got := reopened.GetSettings().PortalAPIBase; got != "https://dns.example.com:3004" {
		t.Errorf("expected the portal API address to survive a restart, got %q", got)
	}
}

func TestSettingsReqKeepsAbsentFields(t *testing.T) {
	cur := &Settings{
		DenyUnmatched: false,
		PortalOrigins: []string{"https://portal.example.com"},
		PortalAPIBase: "https://dns.example.com:3004",
	}

	// A caller that only flips the gate must not clear the portal settings.
	on := true
	got := (&settingsReq{DenyUnmatched: &on}).applyTo(cur)

	if !got.DenyUnmatched {
		t.Error("expected deny_unmatched to be applied")
	}
	if len(got.PortalOrigins) != 1 || got.PortalOrigins[0] != "https://portal.example.com" {
		t.Errorf("expected the portal origins to be kept, got %q", got.PortalOrigins)
	}
	if got.PortalAPIBase != "https://dns.example.com:3004" {
		t.Errorf("expected the portal API address to be kept, got %q", got.PortalAPIBase)
	}

	// An explicitly empty value is a value, not an absent field.
	empty := ""
	got = (&settingsReq{PortalAPIBase: &empty}).applyTo(cur)
	if got.PortalAPIBase != "" {
		t.Errorf("expected the portal API address to be cleared, got %q", got.PortalAPIBase)
	}
}

// TestSetSettingsWarnsAboutHTTPSOriginWithPlainAPI checks the one combination
// of portal settings that cannot work when the browser calls the API directly.
//
// It is stored anyway, with a warning.  The same combination is harmless when
// the browser talks to a same-origin back-end -- the PHP back-end in the
// portal package, or an nginx proxy -- because then the browser never uses
// portal_api_base at all, and those are exactly the deployments that cannot
// put a certificate on the AGHub host.  Refusing the save made the portal
// impossible to configure for them.
func TestSetSettingsWarnsAboutHTTPSOriginWithPlainAPI(t *testing.T) {
	m, _ := newTestManager(t)

	want := &Settings{
		PortalAPIBase: "http://36.133.104.222:3000",
		PortalOrigins: []string{"https://adguardhome.lv10.ren"},
	}

	err := m.SetSettings(want)
	if err != nil {
		t.Fatalf("the combination has to be stored, not refused: %s", err)
	}

	// The settings really landed.
	got := m.GetSettings()
	if got.PortalAPIBase != want.PortalAPIBase {
		t.Fatalf("api base not stored: %q", got.PortalAPIBase)
	}
	if len(got.PortalOrigins) != 1 || got.PortalOrigins[0] != want.PortalOrigins[0] {
		t.Fatalf("origins not stored: %v", got.PortalOrigins)
	}

	// And the warning names the setting, so the administrator can act on it.
	w := SettingsWarning(got)
	if !strings.Contains(w, "portal_api_base") {
		t.Fatalf("warning should name the setting, got %q", w)
	}
	if !strings.Contains(w, "same-origin") {
		t.Fatalf("warning should mention the same-origin route, got %q", w)
	}

	// It has to point at the same-origin back-end too, not only at TLS: that
	// is the route most deployments actually take.
	if !strings.Contains(w, "PHP") {
		t.Fatalf("warning should mention the PHP back-end, got %q", w)
	}
}

// TestSetSettingsAllowsTheWorkingCombinations makes sure the check above only
// rejects the combination that is actually broken.
func TestSetSettingsAllowsTheWorkingCombinations(t *testing.T) {
	testCases := []struct {
		name    string
		apiBase string
		origins []string
	}{{
		name:    "https api with https origin",
		apiBase: "https://api.example.com",
		origins: []string{"https://portal.example.com"},
	}, {
		name:    "plain api with plain origin",
		apiBase: "http://192.0.2.1:3000",
		origins: []string{"http://portal.example.com"},
	}, {
		name:    "plain api without origins",
		apiBase: "http://192.0.2.1:3000",
	}, {
		name:    "no api base at all",
		origins: []string{"https://portal.example.com"},
	}, {
		name:    "same origin, empty api base",
		apiBase: "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestManager(t)

			err := m.SetSettings(&Settings{
				PortalAPIBase: tc.apiBase,
				PortalOrigins: tc.origins,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
