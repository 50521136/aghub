package home

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/aghtls"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// userQuotasFileName is the name of the file with the user quota state.
const userQuotasFileName = "users.json"

// initUserQuotas initializes the user quota manager.  All arguments must not
// be nil.
func initUserQuotas(
	ctx context.Context,
	baseLogger *slog.Logger,
	httpReg aghhttp.Registrar,
	workDir string,
) (err error) {
	mgr, err := users.New(&users.Config{
		Logger: baseLogger,
		Path:   filepath.Join(workDir, dataDir, userQuotasFileName),
	})
	if err != nil {
		return fmt.Errorf("creating user quota manager: %w", err)
	}

	mgr.RegisterWebHandlers(httpReg)
	mgr.Start()

	globalContext.users = mgr

	baseLogger.InfoContext(
		ctx,
		"user quota management is enabled",
		"state_file", mgr.Path(),
	)

	return nil
}

// initUserQuotasDomain makes the user manager report the domain of the
// encrypted DNS endpoint.  A client identifier is only half of the host name a
// client has to use, so the UI needs the other half to show and copy it.  The
// TLS manager is created after the user manager, which is why this is a
// separate step.  tlsMgr must not be nil.
func initUserQuotasDomain(tlsMgr aghtls.Manager) {
	if globalContext.users == nil {
		return
	}

	globalContext.users.SetDomainFunc(func() (domain string) {
		conf := tlsMgr.ExtendedTLSConfig()
		if conf == nil {
			return ""
		}

		return strings.TrimPrefix(conf.ServerName, "*.")
	})
}

// closeUserQuotas stops the user quota manager and saves its state.
func closeUserQuotas(ctx context.Context, l *slog.Logger) {
	if globalContext.users == nil {
		return
	}

	err := globalContext.users.Close()
	if err != nil {
		l.ErrorContext(ctx, "closing user quotas", slogutil.KeyError, err)
	}

	globalContext.users = nil
}
