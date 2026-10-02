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
	ok, reason := m.AllowQuery("nobody", ip)
	if !ok || reason != "" {
		t.Fatalf("expected unmatched client to be allowed, got ok=%v reason=%q", ok, reason)
	}

	// A known client is still governed by its own quota.
	ok, reason = m.AllowQuery("alice", ip)
	if !ok || reason != "" {
		t.Fatalf("expected alice to be allowed, got ok=%v reason=%q", ok, reason)
	}

	err := m.SetSettings(&Settings{DenyUnmatched: true})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	ok, reason = m.AllowQuery("nobody", ip)
	if ok || reason != ReasonUnmatched {
		t.Fatalf("expected unmatched client to be refused, got ok=%v reason=%q", ok, reason)
	}

	// The known client must be unaffected by the switch.
	ok, reason = m.AllowQuery("alice", ip)
	if !ok || reason != "" {
		t.Fatalf("expected alice to still be allowed, got ok=%v reason=%q", ok, reason)
	}

	// Turning it back off restores the permissive behaviour.
	err = m.SetSettings(&Settings{DenyUnmatched: false})
	if err != nil {
		t.Fatalf("setting settings: %v", err)
	}

	ok, reason = m.AllowQuery("nobody", ip)
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
		ok, reason := m.AllowQuery("nobody", ip)
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
