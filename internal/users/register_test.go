package users

import (
	"strings"
	"testing"
	"time"
)

func TestRegister(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	if err := m.SetSettings(&Settings{PortalDefaultQuota: 50, PortalDefaultDays: 7}); err != nil {
		t.Fatalf("setting up: %v", err)
	}

	u, err := m.Register(&RegisterParams{
		Name:          "Alice",
		Password:      "localpass123",
		Email:         "Alice@Example.COM",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	// The identifier is the account and the DNS identity at the same time.
	if u.UID == "" {
		t.Fatal("expected a generated identifier")
	}
	if len(u.IDs) != 1 || u.IDs[0] != u.UID {
		t.Errorf("expected the identifier to be the only ID, got %q", u.IDs)
	}

	// A user with a name of its own keeps it.
	if u.Name != "Alice" {
		t.Errorf("expected name %q, got %q", "Alice", u.Name)
	}

	// The address is stored in its canonical form.
	if u.Email != "alice@example.com" {
		t.Errorf("expected the lowercased address, got %q", u.Email)
	}
	if !u.EmailVerified {
		t.Error("expected the address to be verified")
	}

	// The defaults from the settings apply, so the administrator controls what
	// a sign-up costs without touching the code.
	if u.RequestLimit != 50 {
		t.Errorf("expected the default quota 50, got %d", u.RequestLimit)
	}
	if u.ExpiresAt == 0 {
		t.Error("expected the default validity to be applied")
	}

	if !u.Enabled {
		t.Error("expected a new account to be enabled")
	}

	if !m.HasPortalPassword(u.UID) {
		t.Error("expected the account to be able to sign in")
	}
	if !m.AuthenticatePortal(u.UID, "localpass123") {
		t.Error("expected the password to work")
	}
}

func TestRegisterDefaults(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	// Without any portal defaults the account is unlimited and never expires,
	// which is what a zero means everywhere else in the settings.
	u, err := m.Register(&RegisterParams{Password: "localpass123"})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if u.RequestLimit != Unlimited {
		t.Errorf("expected an unlimited quota, got %d", u.RequestLimit)
	}
	if u.ExpiresAt != 0 {
		t.Errorf("expected no expiration, got %d", u.ExpiresAt)
	}

	// Without a name the identifier stands in for one, so the account is never
	// nameless in the list.
	if u.Name != u.UID {
		t.Errorf("expected the name to default to the identifier, got %q", u.Name)
	}

	// Without an address nothing is verified.
	if u.Email != "" || u.EmailVerified {
		t.Errorf("expected no address, got %q verified=%v", u.Email, u.EmailVerified)
	}
}

func TestRegisterRejects(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	_, err := m.Register(&RegisterParams{Password: "short"})
	if err == nil {
		t.Error("expected a short password to be rejected")
	}

	_, err = m.Register(&RegisterParams{Password: "localpass123", Email: "not an address"})
	if err == nil {
		t.Error("expected a malformed address to be rejected")
	}

	_, err = m.Register(&RegisterParams{Password: "localpass123", Email: "a@example.com"})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	// Two accounts must not share an address, or a password reset would be
	// ambiguous.
	_, err = m.Register(&RegisterParams{Password: "localpass123", Email: "A@Example.com"})
	if err == nil {
		t.Error("expected a duplicate address to be rejected")
	}
}

func TestRegisterGeneratesDistinctIdentifiers(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	seen := map[string]bool{}
	for range 20 {
		u, err := m.Register(&RegisterParams{Password: "localpass123"})
		if err != nil {
			t.Fatalf("registering: %v", err)
		}

		if seen[u.UID] {
			t.Fatalf("identifier %q was handed out twice", u.UID)
		}
		seen[u.UID] = true

		// The identifier goes in front of the domain, so it has to be a valid
		// host name label.  Register enforces that by making it the user's
		// only ID, which goes through the same validation as any other.
		if err = validateID(u.UID); err != nil {
			t.Fatalf("identifier %q is not usable as a subdomain: %v", u.UID, err)
		}
	}
}

func TestRegisterSurvivesReload(t *testing.T) {
	m, _ := newTestManager(t)

	u, err := m.Register(&RegisterParams{
		Password:      "localpass123",
		Email:         "alice@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if err = m.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	m2, err := New(&Config{Logger: testLogger(), Path: m.Path(), Location: time.UTC})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer m2.Close()

	got := m2.Get(u.UID)
	if got == nil {
		t.Fatal("expected the account to survive a restart")
	}
	if got.Email != "alice@example.com" || !got.EmailVerified {
		t.Errorf("expected the address to survive, got %q verified=%v", got.Email, got.EmailVerified)
	}
	if !m2.AuthenticatePortal(u.UID, "localpass123") {
		t.Error("expected the password to survive a restart")
	}
	if m2.FindByEmail("ALICE@example.com") == nil {
		t.Error("expected the address to be findable after a restart")
	}
}

func TestSetEmail(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	a, err := m.Register(&RegisterParams{Password: "localpass123"})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	_, err = m.Register(&RegisterParams{Password: "localpass123", Email: "b@example.com"})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	// Binding an address proves it, because the caller had to check a code.
	if err = m.SetEmail(a.UID, "A@Example.com", true); err != nil {
		t.Fatalf("setting the address: %v", err)
	}

	if got := m.Get(a.UID).Email; got != "a@example.com" {
		t.Errorf("expected the canonical address, got %q", got)
	}
	if !m.Get(a.UID).EmailVerified {
		t.Error("expected the address to be verified")
	}

	// The address of another account is not available.
	if err = m.SetEmail(a.UID, "b@example.com", true); err == nil {
		t.Error("expected an address in use to be refused")
	}

	// Changing the address without proof clears the flag: the proof belonged
	// to the old address.
	if err = m.SetEmail(a.UID, "c@example.com", false); err != nil {
		t.Fatalf("setting the address: %v", err)
	}
	if m.Get(a.UID).EmailVerified {
		t.Error("expected an unproven address not to be verified")
	}

	// Clearing it removes both the address and the flag.
	if err = m.SetEmail(a.UID, "", false); err != nil {
		t.Fatalf("clearing the address: %v", err)
	}
	if got := m.Get(a.UID).Email; got != "" {
		t.Errorf("expected no address, got %q", got)
	}
	if m.Get(a.UID).EmailVerified {
		t.Error("expected no address to be verified")
	}

	if m.FindByEmail("a@example.com") != nil {
		t.Error("expected the old address to be free again")
	}
	if m.FindByEmail("b@example.com") == nil {
		t.Error("expected the other account to keep its address")
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{{
		name: "empty",
		in:   "  ",
		want: "",
	}, {
		name: "plain",
		in:   "Alice@Example.COM",
		want: "alice@example.com",
	}, {
		name: "display name",
		in:   "Alice <alice@example.com>",
		want: "alice@example.com",
	}, {
		name:    "no at sign",
		in:      "alice.example.com",
		wantErr: true,
	}, {
		name:    "no domain",
		in:      "alice@",
		wantErr: true,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeEmail(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.in, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestRegisterEmailIsNotVerifiedWithoutProof(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	// A registration that did not go through the code check must not end up
	// with a verified address, or the flag would mean nothing.
	u, err := m.Register(&RegisterParams{
		Password:      "localpass123",
		Email:         "alice@example.com",
		EmailVerified: false,
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if u.EmailVerified {
		t.Error("expected an unproven address not to be verified")
	}
	if u.Email != "alice@example.com" {
		t.Errorf("expected the address to be kept, got %q", u.Email)
	}
}

func TestRegisterLeavesNothingBehindWhenThePasswordFails(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	// A password that cannot be stored must not leave a half-created account,
	// or the user list fills up with entries nobody can sign in to.
	// bcrypt refuses anything longer than 72 bytes.
	long := strings.Repeat("a", 73)

	_, err := m.Register(&RegisterParams{Password: long})
	if err == nil {
		t.Fatal("expected an over-long password to be rejected")
	}

	if n := len(m.List()); n != 0 {
		t.Errorf("expected no users to be left behind, got %d", n)
	}
}

// TestFindByLoginByEmail checks that a user can sign in with the address they
// signed up with, in any casing, and that a name shared by two users is still
// refused while an address is not.
func TestFindByLoginByEmail(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	if err := m.SetSettings(&Settings{}); err != nil {
		t.Fatalf("setting up: %v", err)
	}

	eve, err := m.Register(&RegisterParams{
		Name:     "Eve",
		Password: "localpass123",
		Email:    "eve@example.com",
	})
	if err != nil {
		t.Fatalf("registering Eve: %v", err)
	}

	bob, err := m.Register(&RegisterParams{
		Name:     "Bob",
		Password: "localpass123",
		Email:    "bob@example.com",
	})
	if err != nil {
		t.Fatalf("registering Bob: %v", err)
	}

	// A second account with the same name, which makes the name ambiguous.
	_, err = m.Add(&AddParams{Name: "Eve", IDs: []string{"eveid2"}})
	if err != nil {
		t.Fatalf("adding the second Eve: %v", err)
	}

	testCases := []struct {
		name  string
		login string
		want  string
	}{{
		name:  "by_uid",
		login: eve.UID,
		want:  eve.UID,
	}, {
		name:  "by_id",
		login: eve.UID,
		want:  eve.UID,
	}, {
		name:  "by_email",
		login: "eve@example.com",
		want:  eve.UID,
	}, {
		name:  "by_email_uppercase",
		login: "EVE@Example.COM",
		want:  eve.UID,
	}, {
		name:  "by_email_padded",
		login: "  eve@example.com  ",
		want:  eve.UID,
	}, {
		name:  "the_other_account",
		login: "bob@example.com",
		want:  bob.UID,
	}, {
		name:  "ambiguous_name_refused",
		login: "Eve",
		want:  "",
	}, {
		name:  "unknown",
		login: "nobody@example.com",
		want:  "",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := m.FindByLogin(tc.login)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("expected no user, got %q", got.UID)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected %q, got no user", tc.want)
			}

			if got.UID != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got.UID)
			}
		})
	}
}
