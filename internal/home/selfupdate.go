package home

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/selfupdate"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// restartDelay is the delay before the process is restarted after a successful
// update.  It gives the HTTP response the time to reach the client.
const restartDelay = 500 * time.Millisecond

// updateCtxTimeout is the timeout of the update procedure itself.
const updateCtxTimeout = 20 * time.Minute

// initSelfUpdate initializes the online update subsystem.  All arguments must
// not be nil.
func initSelfUpdate(ctx context.Context, baseLogger *slog.Logger) (err error) {
	updater, err := selfupdate.New(&selfupdate.Config{Logger: baseLogger})
	if err != nil {
		return err
	}

	globalContext.updater = updater

	baseLogger.InfoContext(
		ctx,
		"online update is enabled",
		"current_version", updater.CurrentVersion(),
		"repository", updater.Repo(),
	)

	return nil
}

// registerUpdateHandlers sets up the HTTP handlers of the online update.
func (web *webAPI) registerUpdateHandlers() {
	if globalContext.updater == nil {
		return
	}

	web.httpReg.Register(
		http.MethodGet,
		"/control/aghub/update/status",
		web.handleUpdateStatus,
	)
	web.httpReg.Register(
		http.MethodGet,
		"/control/aghub/update/check",
		web.handleUpdateCheck,
	)
	web.httpReg.Register(
		http.MethodPost,
		"/control/aghub/update/apply",
		web.handleUpdateApply,
	)
}

// updateStatusResp is the response of the GET /control/aghub/update/status HTTP
// API.
type updateStatusResp struct {
	// Status is the state of the update in progress.
	Status selfupdate.Status `json:"status"`

	// CurrentVersion is the version of the running application.
	CurrentVersion string `json:"current_version"`

	// Repository is the GitHub repository used for updates.
	Repository string `json:"repository"`

	// Enabled is true if the online update is configured.
	Enabled bool `json:"enabled"`
}

// newUpdateStatusResp returns the current update status.
func newUpdateStatusResp(u *selfupdate.Updater) (resp *updateStatusResp) {
	return &updateStatusResp{
		CurrentVersion: u.CurrentVersion(),
		Repository:     u.Repo(),
		Enabled:        u.Enabled(),
		Status:         u.Status(),
	}
}

// handleUpdateStatus is the handler for the GET /control/aghub/update/status
// HTTP API.
func (web *webAPI) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := web.logger

	updater := globalContext.updater
	if updater == nil {
		aghhttp.WriteJSONResponseOK(ctx, l, w, r, &updateStatusResp{})

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, newUpdateStatusResp(updater))
}

// handleUpdateCheck is the handler for the GET /control/aghub/update/check HTTP
// API.
func (web *webAPI) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := web.logger

	updater := globalContext.updater
	if updater == nil {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusServiceUnavailable,
			"online update is not initialized",
		)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, updater.Check(ctx))
}

// handleUpdateApply is the handler for the POST /control/aghub/update/apply
// HTTP API.  The update is performed in the background and the process is
// restarted when it succeeds, so the response is sent immediately.
func (web *webAPI) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := web.logger

	updater := globalContext.updater
	if updater == nil {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusServiceUnavailable,
			"online update is not initialized",
		)

		return
	}

	if !updater.Enabled() {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusBadRequest,
			"online update is not configured",
		)

		return
	}

	if updater.Status().Running {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusConflict,
			"an update is already in progress",
		)

		return
	}

	go applyUpdate(l, updater)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, newUpdateStatusResp(updater))
}

// applyUpdate performs the update and restarts the application.  It is meant to
// be run in a separate goroutine.
func applyUpdate(l *slog.Logger, updater *selfupdate.Updater) {
	// The request context is already canceled at this point, so a background
	// context is used instead.
	ctx, cancel := context.WithTimeout(context.Background(), updateCtxTimeout)
	defer cancel()

	execPath := updater.ExecPath()

	err := updater.Apply(ctx)
	if err != nil {
		l.ErrorContext(ctx, "applying update", slogutil.KeyError, err)

		return
	}

	// Give the client the time to receive the response before the process is
	// replaced.
	time.Sleep(restartDelay)

	err = selfupdate.Restart(l, execPath)
	if err != nil {
		l.ErrorContext(ctx, "restarting after update", slogutil.KeyError, err)
	}
}
