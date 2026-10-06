package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterNoDuplicatePaths checks that the portal registers each path once.
//
// The registrar keys its mux on the path and only hands the method to the
// middleware wrapper, so two registrations on one path make the mux panic at
// startup.  That is a crash on every start rather than a failing request, and
// nothing else catches it: it compiles, and the unit tests build their own
// registrar.  Registering on a real mux here turns it into a test failure.
func TestRegisterNoDuplicatePaths(t *testing.T) {
	m, _ := newTestPortal(t, &testUserStore{})

	mux := http.NewServeMux()
	reg := aghhttp.NewPlainRegistrar(mux)

	require.NotPanics(t, func() {
		m.Register(reg)
	})

	// The paths that serve both verbs through one handler, because the mux
	// cannot tell them apart, must still reach something for each verb.
	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   int
	}{{
		name:   "presets",
		method: http.MethodGet,
		path:   "/portal/api/avatar",
		want:   http.StatusOK,
	}, {
		name:   "set without session",
		method: http.MethodPost,
		path:   "/portal/api/avatar",
		want:   http.StatusUnauthorized,
	}, {
		name:   "checkin status without session",
		method: http.MethodGet,
		path:   "/portal/api/checkin",
		want:   http.StatusUnauthorized,
	}, {
		name:   "checkin without session",
		method: http.MethodPost,
		path:   "/portal/api/checkin",
		want:   http.StatusUnauthorized,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, r)

			assert.Equal(t, tc.want, w.Code)
		})
	}
}
