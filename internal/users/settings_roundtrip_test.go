package users

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fillSettings returns settings with every field set to a value that is not the
// zero value, so that a field dropped along the way is visible.
//
// It panics on a field kind it does not know, which is the point: a new field
// has to be handled here, and that is cheaper than discovering in production
// that the administrator's configuration was silently cleared.
func fillSettings(tb testing.TB) (s *Settings) {
	tb.Helper()

	s = &Settings{}
	v := reflect.ValueOf(s).Elem()
	typ := v.Type()

	for i := range v.NumField() {
		f := v.Field(i)
		name := typ.Field(i).Name

		switch f.Kind() {
		case reflect.String:
			f.SetString("value-" + name)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int, reflect.Int64:
			f.SetInt(42)
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.String {
				tb.Fatalf("unhandled slice element for %s", name)
			}

			f.Set(reflect.ValueOf([]string{"value-" + name}))
		default:
			tb.Fatalf("unhandled kind %s for %s", f.Kind(), name)
		}
	}

	return s
}

// TestSettingsRoundTrip makes sure that no field of the settings is dropped on
// the way through the manager or the state file.
//
// Four separate fields were lost this way, each by a function that rebuilt the
// struct field by field: the proxy setter, the loader, the settings API and the
// getter.  The failure is always silent, and the administrator only notices
// when a feature stops working.
func TestSettingsRoundTrip(t *testing.T) {
	m, _ := newTestManager(t)

	want := fillSettings(t)

	err := m.SetSettings(want)
	if err != nil {
		t.Fatalf("setting: %v", err)
	}

	assertSameSettings(t, "through the manager", want, m.GetSettings())

	// The state file is the other half of the trip.
	path := m.Path()
	if err = m.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	m2, err := New(&Config{Logger: testLogger(), Path: path, Location: time.UTC})
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer m2.Close()

	assertSameSettings(t, "through the state file", want, m2.GetSettings())
}

// assertSameSettings reports every field that differs.
func assertSameSettings(tb testing.TB, where string, want, got *Settings) {
	tb.Helper()

	w := reflect.ValueOf(want).Elem()
	g := reflect.ValueOf(got).Elem()
	typ := w.Type()

	for i := range w.NumField() {
		name := typ.Field(i).Name
		if !reflect.DeepEqual(w.Field(i).Interface(), g.Field(i).Interface()) {
			tb.Errorf("%s: field %s was dropped: want %v, got %v",
				where, name, w.Field(i).Interface(), g.Field(i).Interface())
		}
	}
}

// TestGetSettingsReturnsACopy makes sure that a caller cannot change the stored
// settings by writing through the pointer it was handed.
func TestGetSettingsReturnsACopy(t *testing.T) {
	m, _ := newTestManager(t)
	defer m.Close()

	err := m.SetSettings(&Settings{
		DenyUnmatched: true,
		PortalOrigins: []string{"https://portal.example.com"},
	})
	if err != nil {
		t.Fatalf("setting: %v", err)
	}

	got := m.GetSettings()
	got.DenyUnmatched = false
	got.PortalOrigins[0] = "https://evil.example.com"

	fresh := m.GetSettings()
	if !fresh.DenyUnmatched {
		t.Error("expected the stored flag to be unchanged")
	}
	if fresh.PortalOrigins[0] != "https://portal.example.com" {
		t.Errorf("expected the stored origin to be unchanged, got %q", fresh.PortalOrigins[0])
	}
}

// TestSettingsAPIHidesTheMailPassword makes sure that the mail password never
// reaches a client, on either of the endpoints that return the settings.
func TestSettingsAPIHidesTheMailPassword(t *testing.T) {
	const password = "hunter2-secret"

	m, _ := newTestManager(t)
	defer m.Close()

	err := m.SetSettings(&Settings{
		SMTPHost:     "smtp.example.com",
		SMTPUser:     "mailer",
		SMTPPassword: password,
		SMTPFrom:     "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("setting: %v", err)
	}

	reg := &recordingRegistrar{handlers: map[string]http.HandlerFunc{}}
	m.RegisterWebHandlers(reg)

	tests := []struct {
		method string
		path   string
		body   string
	}{{
		method: http.MethodGet,
		path:   "/control/users/list",
	}, {
		// The settings endpoint replaces the settings, so it answers with the
		// stored ones and is the other place a secret could escape.
		method: http.MethodPost,
		path:   "/control/users/settings",
		body:   `{"deny_unmatched":false}`,
	}}

	for _, tc := range tests {
		handler := reg.handlers[tc.path]
		if handler == nil {
			t.Fatalf("no handler registered for %s", tc.path)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		handler(rec, req)

		body := rec.Body.String()

		if strings.Contains(body, password) {
			t.Errorf("%s: the mail password is in the response", tc.path)
		}

		var decoded struct {
			Settings struct {
				SMTPPasswordSet bool `json:"smtp_password_set"`
			} `json:"settings"`
		}

		err = json.Unmarshal(rec.Body.Bytes(), &decoded)
		if err != nil {
			t.Fatalf("%s: decoding: %v (%s)", tc.path, err, rec.Body.String())
		}

		if !decoded.Settings.SMTPPasswordSet {
			t.Errorf("%s: expected the response to say that a password is set", tc.path)
		}
	}

	// The manager itself still has the real value, or nothing could be sent.
	if got := m.GetSettings().SMTPPassword; got != password {
		t.Errorf("expected the stored password, got %q", got)
	}
}

// recordingRegistrar captures the handlers so that they can be called directly.
type recordingRegistrar struct {
	handlers map[string]http.HandlerFunc
}

func (r *recordingRegistrar) Register(method, path string, h http.HandlerFunc) {
	r.handlers[path] = h
}
