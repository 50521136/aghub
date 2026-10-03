package home

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/aghuser"
	"github.com/AdguardTeam/AdGuardHome/internal/portal"
	"github.com/AdguardTeam/AdGuardHome/internal/querylog"
	"github.com/AdguardTeam/AdGuardHome/internal/users"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/timeutil"
)

// portalSessionsFileName is the name of the file with the user portal
// sessions.
//
// It is deliberately not the file the administrator sessions live in.  The two
// interfaces must not share sessions: an administrator session has to be
// useless in the portal and the other way around.
const portalSessionsFileName = "portal_sessions.db"

// initPortal initializes the user portal and registers its handlers with reg.
// reg must not apply the administrator authentication middleware.
//
// It must be called after the user quota manager and the query log are
// created, because the portal serves the users and reads their log entries.
func initPortal(
	ctx context.Context,
	baseLogger *slog.Logger,
	reg aghhttp.Registrar,
	workDir string,
) (err error) {
	usersMgr := globalContext.users
	if usersMgr == nil {
		return fmt.Errorf("portal: no user quota manager")
	}

	log := globalContext.queryLog
	if log == nil {
		return fmt.Errorf("portal: no query log")
	}

	sessions, err := newPortalSessions(ctx, baseLogger, usersMgr, workDir)
	if err != nil {
		return fmt.Errorf("portal: creating the session storage: %w", err)
	}

	mgr, err := portal.New(&portal.Config{
		Logger:   baseLogger,
		Users:    usersMgr,
		Sessions: sessions,
		Log:      &portalLogSource{log: log},
	})
	if err != nil {
		return fmt.Errorf("portal: creating the manager: %w", err)
	}

	mgr.Register(reg)

	globalContext.portal = mgr

	baseLogger.InfoContext(
		ctx,
		"user portal is enabled",
		"sessions_file", filepath.Join(workDir, dataDir, portalSessionsFileName),
	)

	return nil
}

// newPortalSessions creates the session storage of the user portal.
//
// The storage is built on a database of its own, which contains every quota
// user, so that the sessions survive a restart.  Whether a user may sign in at
// all is decided by the portal on every request and not by this database: a
// user without a portal password is simply not authenticated.
func newPortalSessions(
	ctx context.Context,
	baseLogger *slog.Logger,
	usersMgr *users.Manager,
	workDir string,
) (s aghuser.SessionStorage, err error) {
	userDB := aghuser.NewDefaultDB()
	for _, info := range usersMgr.List() {
		err = userDB.Create(ctx, portal.SessionUser(info.UID))
		if err != nil {
			return nil, fmt.Errorf("adding the portal user %q: %w", info.UID, err)
		}
	}

	s, err = aghuser.NewDefaultSessionStorage(ctx, &aghuser.DefaultSessionStorageConfig{
		Logger:     baseLogger.With(slogutil.KeyPrefix, "portal_sessions"),
		Clock:      timeutil.SystemClock{},
		UserDB:     userDB,
		DBPath:     filepath.Join(workDir, dataDir, portalSessionsFileName),
		SessionTTL: portal.DefaultSessionTTL,
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}

// portalLogSource adapts the query log to the user portal.
type portalLogSource struct {
	// log is the query log of this AGHub instance.  It must not be nil.
	log querylog.QueryLog
}

// type check
var _ portal.LogSource = (*portalLogSource)(nil)

// Search implements the [portal.LogSource] interface.
func (s *portalLogSource) Search(
	ctx context.Context,
	req *portal.LogRequest,
) (resp *portal.LogResponse, err error) {
	res, err := s.log.Search(ctx, &querylog.SearchRequest{
		ClientIDs:  req.ClientIDs,
		ClientNets: req.ClientNets,
		Term:       req.Term,
		OlderThan:  req.OlderThan,
		Limit:      req.Limit,
	})
	if err != nil {
		return nil, err
	}

	return &portal.LogResponse{Entries: res.Entries}, nil
}
