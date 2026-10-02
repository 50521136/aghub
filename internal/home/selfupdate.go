package home

import (
	"context"
	"encoding/json"
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

	// The acceleration proxy is stored in the AGHub-wide settings, which the
	// users manager owns.
	if users := globalContext.users; users != nil {
		proxy := users.GetSettings().UpdateProxy
		if proxy != "" {
			updater.SetProxy(proxy)
		}
	}

	globalContext.updater = updater

	baseLogger.InfoContext(
		ctx,
		"online update is enabled",
		"current_version", updater.CurrentVersion(),
		"repository", updater.Repo(),
		"proxy", updater.Proxy(),
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
	web.httpReg.Register(
		http.MethodGet,
		"/control/aghub/update/proxies",
		web.handleUpdateProxies,
	)
	web.httpReg.Register(
		http.MethodPost,
		"/control/aghub/update/proxy",
		web.handleUpdateSetProxy,
	)
	web.httpReg.Register(
		http.MethodPost,
		"/control/aghub/update/test-proxies",
		web.handleUpdateTestProxies,
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

// updateProxiesResp is the response of the GET /control/aghub/update/proxies
// HTTP API.
type updateProxiesResp struct {
	// Proxies is the built-in list of GitHub acceleration proxies.
	Proxies []selfupdate.ProxyNode `json:"proxies"`

	// Current is the prefix the updater is using now.  It differs from
	// Stored when the environment overrides the setting.
	Current string `json:"current"`

	// Stored is the prefix saved in the settings.
	Stored string `json:"stored"`
}

// handleUpdateProxies is the handler for the GET /control/aghub/update/proxies
// HTTP API.
func (web *webAPI) handleUpdateProxies(w http.ResponseWriter, r *http.Request) {
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

	resp := &updateProxiesResp{
		Proxies: selfupdate.DefaultProxies(),
		Current: updater.Proxy(),
	}

	if users := globalContext.users; users != nil {
		resp.Stored = users.GetSettings().UpdateProxy
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, resp)
}

// updateSetProxyReq is the request of the POST /control/aghub/update/proxy HTTP
// API.
type updateSetProxyReq struct {
	// Proxy is the acceleration prefix.  An empty string disables the
	// acceleration.
	Proxy string `json:"proxy"`
}

// handleUpdateSetProxy is the handler for the POST /control/aghub/update/proxy
// HTTP API.
func (web *webAPI) handleUpdateSetProxy(w http.ResponseWriter, r *http.Request) {
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

	users := globalContext.users
	if users == nil {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusServiceUnavailable,
			"settings storage is not initialized",
		)

		return
	}

	req := &updateSetProxyReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	proxy := selfupdate.NormalizeProxy(req.Proxy)

	err = selfupdate.ValidateProxy(proxy)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	err = users.SetUpdateProxy(proxy)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "saving: %s", err)

		return
	}

	updater.SetProxy(proxy)

	l.InfoContext(ctx, "online update proxy set", "proxy", proxy)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &updateProxiesResp{
		Proxies: selfupdate.DefaultProxies(),
		Current: updater.Proxy(),
		Stored:  proxy,
	})
}

// updateTestProxiesReq is the request of the POST
// /control/aghub/update/test-proxies HTTP API.
type updateTestProxiesReq struct {
	// Proxies are the prefixes to test.  An empty list means the built-in
	// list, which is large enough that clients are expected to send it in
	// batches.
	Proxies []string `json:"proxies"`
}

// updateTestProxiesResp is the response of the POST
// /control/aghub/update/test-proxies HTTP API.
type updateTestProxiesResp struct {
	// Results are the test results, in the order of the request.
	Results []selfupdate.ProxyTest `json:"results"`
}

// maxTestProxies is the number of proxies a single test request may cover.
// Testing is done in parallel, so a larger batch only makes the response
// slower to arrive.
const maxTestProxies = 40

// handleUpdateTestProxies is the handler for the POST
// /control/aghub/update/test-proxies HTTP API.  It tests each candidate
// against the GitHub API and a release asset, so that the caller can pick one
// that works from this network.
func (web *webAPI) handleUpdateTestProxies(w http.ResponseWriter, r *http.Request) {
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

	req := &updateTestProxiesReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if len(req.Proxies) > maxTestProxies {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusBadRequest,
			"too many proxies: %d, at most %d are allowed",
			len(req.Proxies),
			maxTestProxies,
		)

		return
	}

	for _, p := range req.Proxies {
		err = selfupdate.ValidateProxy(p)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

			return
		}
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &updateTestProxiesResp{
		Results: updater.TestProxies(ctx, req.Proxies),
	})
}
