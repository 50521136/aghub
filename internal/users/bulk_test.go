package users

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// formatBulkErrors renders bulk errors readably, since the struct is otherwise
// printed as a pointer.
func formatBulkErrors(errs []*BulkError) (s string) {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("line %d %q: %s", e.Line, e.Text, e.Message))
	}

	return strings.Join(parts, "; ")
}

func TestParseBulk(t *testing.T) {
	text := `
# a comment line
a
b,张三
c,李四,20000,month
d,王五,50000,day,30
e,,unlimited,total,0

f,赵六,-1,每月,365
`

	entries, errs, skipped := ParseBulk(text)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %s", formatBulkErrors(errs))
	}

	// Four lines carry no entry: the leading blank, the comment, the blank
	// before "f" and the trailing newline.
	if skipped != 4 {
		t.Fatalf("expected 4 skipped lines, got %d", skipped)
	}

	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(entries))
	}

	check := func(i int, id, name string, limit int64, period Period, days int64) {
		t.Helper()

		e := entries[i]
		if e.ID != id || e.Name != name || e.Limit != limit || e.Period != period || e.ExpireDays != days {
			t.Errorf(
				"entry %d: got id=%q name=%q limit=%d period=%q days=%d",
				i, e.ID, e.Name, e.Limit, e.Period, e.ExpireDays,
			)
		}
	}

	// A bare identifier becomes its own display name and is unlimited.
	check(0, "a", "a", Unlimited, PeriodTotal, 0)
	check(1, "b", "张三", Unlimited, PeriodTotal, 0)
	check(2, "c", "李四", 20000, PeriodMonth, 0)
	check(3, "d", "王五", 50000, PeriodDay, 30)

	// An empty name field falls back to the identifier.
	check(4, "e", "e", Unlimited, PeriodTotal, 0)

	// Chinese period names and the unlimited keyword are accepted.
	check(5, "f", "赵六", Unlimited, PeriodMonth, 365)
}

func TestParseBulkErrors(t *testing.T) {
	text := `
a_b
good,ok
c,ok,notanumber
d,ok,100,fortnight
e,ok,100,day,-5
f,ok,100,day,0,extra
`

	_, errs, _ := ParseBulk(text)
	if len(errs) != 5 {
		t.Fatalf("expected 5 errors, got %d: %v", len(errs), errs)
	}

	lines := map[int]bool{}
	for _, e := range errs {
		lines[e.Line] = true
	}

	// Lines 2, 4, 5, 6, 7 (1-indexed) are the bad ones; line 3 is valid.
	for _, n := range []int{2, 4, 5, 6, 7} {
		if !lines[n] {
			t.Errorf("expected an error on line %d, got %v", n, errs)
		}
	}

	if lines[3] {
		t.Errorf("line 3 should have parsed, got %v", errs)
	}
}

func TestBulkAddCreatesUsers(t *testing.T) {
	m, _ := newTestManager(t)

	res := m.BulkAdd("a\nb,张三,20000,month,30\nc\n", true)
	if len(res.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", res.Errors)
	}

	if len(res.Users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(res.Users))
	}

	// The result must follow the order of the input, not the random order of
	// a map iteration.
	names := make([]string, 0, len(res.Users))
	for _, u := range res.Users {
		names = append(names, u.IDs[0])
	}

	if !slices.Equal(names, []string{"a", "b", "c"}) {
		t.Errorf("expected the users in input order, got %v", names)
	}

	// Every created user must be resolvable by its identifier straight away.
	ip := netip.MustParseAddr("10.0.0.1")

	for _, id := range []string{"a", "b", "c"} {
		ok, reason := m.AllowQuery(id, ip, "")
		if !ok {
			t.Errorf("expected %q to be allowed, reason=%q", id, reason)
		}
	}

	b := m.Get(res.Users[1].UID)
	if b == nil {
		t.Fatal("expected user b to exist")
	}

	if b.Name != "张三" || b.RequestLimit != 20000 || b.Period != PeriodMonth {
		t.Errorf("unexpected user b: %+v", b)
	}

	if b.ExpiresAt == 0 {
		t.Error("expected user b to have an expiration date")
	}
}

func TestBulkAddIsAllOrNothing(t *testing.T) {
	m, _ := newTestManager(t)

	res := m.BulkAdd("a\nb_bad\nc\n", true)
	if len(res.Errors) == 0 {
		t.Fatal("expected errors")
	}

	if len(res.Users) != 0 {
		t.Fatalf("expected nothing to be created, got %d users", len(res.Users))
	}

	if n := len(m.List()); n != 0 {
		t.Fatalf("expected the user list to stay empty, got %d", n)
	}
}

func TestBulkAddRejectsClashes(t *testing.T) {
	m, _ := newTestManager(t)

	addUser(t, m, &AddParams{
		Name:         "existing",
		IDs:          []string{"a"},
		RequestLimit: iptr(1),
		Enabled:      true,
	})

	res := m.BulkAdd("b\na\n", true)
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %v", res.Errors)
	}

	if len(res.Users) != 0 {
		t.Fatalf("expected nothing to be created, got %d", len(res.Users))
	}

	// The pre-existing user must be untouched.
	if n := len(m.List()); n != 1 {
		t.Fatalf("expected 1 user, got %d", n)
	}
}

func TestBulkAddRejectsDuplicatesWithinPayload(t *testing.T) {
	m, _ := newTestManager(t)

	res := m.BulkAdd("a\nb\na\n", true)
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %v", res.Errors)
	}

	if len(m.List()) != 0 {
		t.Fatal("expected nothing to be created")
	}
}

func TestBulkAddEmptyPayload(t *testing.T) {
	m, _ := newTestManager(t)

	for _, text := range []string{"", "\n\n", "# only a comment\n"} {
		res := m.BulkAdd(text, true)
		if len(res.Errors) == 0 {
			t.Errorf("expected an error for payload %q", text)
		}
	}
}

func TestBulkAddPersistence(t *testing.T) {
	m, _ := newTestManager(t)

	res := m.BulkAdd("a\nb\n", true)
	if len(res.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", res.Errors)
	}

	err := m.save()
	if err != nil {
		t.Fatalf("saving: %v", err)
	}

	reopened, err := New(&Config{Logger: testLogger(), Path: m.Path()})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}

	if n := len(reopened.List()); n != 2 {
		t.Fatalf("expected 2 users after reload, got %d", n)
	}
}
