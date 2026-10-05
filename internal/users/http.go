package users

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
)

// listResp is the response of the GET /control/users/list HTTP API.
type listResp struct {
	// Summary is the aggregate state of all users.
	Summary *Summary `json:"summary"`

	// Settings is the manager-wide configuration.
	Settings *settingsResponse `json:"settings"`

	// Domain is the domain of the DoT/DoH endpoint, or an empty string when
	// none is configured.  The UI appends it to a client identifier to build
	// the host name a client has to use.
	Domain string `json:"domain"`

	// Users is the state of all users.
	Users []*Info `json:"users"`
}

// actionResp is the response of the mutating HTTP APIs.
type actionResp struct {
	// User is the affected user, if any.
	User *Info `json:"user,omitempty"`

	// Message is the human-readable result message.
	Message string `json:"message,omitempty"`

	// Updated is the number of affected users.
	Updated int `json:"updated,omitempty"`

	// OK is true if the operation succeeded.
	OK bool `json:"ok"`
}

// addReq is the request of the POST /control/users/add HTTP API.
type addReq struct {
	// Name is the human-readable name of the user.
	Name string `json:"name"`

	// Remark is an optional free-form note.
	Remark string `json:"remark"`

	// IDs are the identifiers of the user.
	IDs []string `json:"ids"`

	// RequestLimit is the request quota per period.  A negative value or a
	// missing field means unlimited.
	RequestLimit *int64 `json:"request_limit"`

	// Period is the quota accounting period.
	Period Period `json:"period"`

	// ExpireDays is the number of days until the subscription expires.  Zero
	// or a missing field means that it never expires.
	ExpireDays int64 `json:"expire_days"`

	// Enabled is the initial administrator-controlled switch.
	Enabled *bool `json:"enabled"`
}

// updateReq is the request of the POST /control/users/update HTTP API.
type updateReq struct {
	// Name is the new name of the user.
	Name *string `json:"name"`

	// Remark is the new note of the user.
	Remark *string `json:"remark"`

	// IDs are the new identifiers of the user.
	IDs *[]string `json:"ids"`

	// RequestLimit is the new request quota.
	RequestLimit *int64 `json:"request_limit"`

	// Period is the new accounting period.
	Period *Period `json:"period"`

	// ExpireDays sets the number of days until the subscription expires.
	ExpireDays *int64 `json:"expire_days"`

	// ExtendDays adds the given number of days to the expiration date.
	ExtendDays *int64 `json:"extend_days"`

	// Enabled is the new administrator-controlled switch.
	Enabled *bool `json:"enabled"`

	// UID is the identifier of the user to update.
	UID string `json:"uid"`
}

// uidsReq is the request of the APIs that act on a set of users.
type uidsReq struct {
	// UIDs are the identifiers of the affected users.
	UIDs []string `json:"uids"`
}

// toggleReq is the request of the POST /control/users/toggle HTTP API.
type toggleReq struct {
	// UIDs are the identifiers of the affected users.
	UIDs []string `json:"uids"`

	// Enabled is the new state of the users.
	Enabled bool `json:"enabled"`
}

// importReq is the request of the POST /control/users/import HTTP API.
type importReq struct {
	// Users are the users to import.
	Users []*User `json:"users"`

	// Replace is true if the existing users should be removed.
	Replace bool `json:"replace"`
}

// settingsResp is the response of the GET /control/users/settings HTTP API.
type settingsResp struct {
	// Settings is the manager-wide configuration.
	Settings *settingsResponse `json:"settings"`

	// Warning describes a combination that is very likely a mistake but that
	// the manager still stores, so that the administrator sees the problem
	// without being blocked from saving.  Empty when there is nothing to say.
	Warning string `json:"warning,omitempty"`
}

// bulkAddReq is the request of the POST /control/users/bulk-add HTTP API.
type bulkAddReq struct {
	// Text is the payload, one entry per line.
	Text string `json:"text"`

	// Enabled is the initial state of the created users.  A missing field means
	// enabled.
	Enabled *bool `json:"enabled"`
}

// RegisterWebHandlers registers the HTTP handlers of the user manager.
//
// Note that the router is a plain [http.ServeMux] registered by path, so a
// path may only be used once.  That is why the settings are read back as part
// of /control/users/list instead of through their own GET: the write needs the
// POST verb on the same path, and the two would collide.
func (m *Manager) RegisterWebHandlers(reg aghhttp.Registrar) {
	reg.Register(http.MethodGet, "/control/users/list", m.handleList)
	reg.Register(http.MethodGet, "/control/users/export", m.handleExport)
	reg.Register(http.MethodPost, "/control/users/settings", m.handleSetSettings)
	reg.Register(http.MethodPost, "/control/users/add", m.handleAdd)
	reg.Register(http.MethodPost, "/control/users/update", m.handleUpdate)
	reg.Register(http.MethodPost, "/control/users/delete", m.handleDelete)
	reg.Register(http.MethodPost, "/control/users/reset", m.handleReset)
	reg.Register(http.MethodPost, "/control/users/toggle", m.handleToggle)
	reg.Register(http.MethodPost, "/control/users/password", m.handlePortalPassword)
	reg.Register(http.MethodPost, "/control/users/import", m.handleImport)
	reg.Register(http.MethodPost, "/control/users/bulk-add", m.handleBulkAdd)
	reg.Register(http.MethodGet, "/control/users/backups", m.handleBackups)
	reg.Register(http.MethodPost, "/control/users/restore", m.handleRestore)
}

// backupsResp is the response of the GET /control/users/backups HTTP API.
type backupsResp struct {
	// Backups are the dated backups of the user state, newest first.
	Backups []*BackupInfo `json:"backups"`
}

// restoreReq is the request of the POST /control/users/restore HTTP API.
type restoreReq struct {
	// Name is the name of the backup to restore.
	Name string `json:"name"`
}

// restoreResp is the response of the POST /control/users/restore HTTP API.
type restoreResp struct {
	// Users is the number of the restored users.
	Users int `json:"users"`
}

// handleBackups is the handler for the GET /control/users/backups HTTP API.
func (m *Manager) handleBackups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	backups, err := m.Backups()
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &backupsResp{Backups: backups})
}

// handleRestore is the handler for the POST /control/users/restore HTTP API.
func (m *Manager) handleRestore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &restoreReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	n, err := m.RestoreBackup(req.Name)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &restoreResp{Users: n})
}

// handleSetSettings is the handler for the POST /control/users/settings HTTP
// API.
func (m *Manager) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &settingsReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	err = m.SetSettings(req.applyTo(m.GetSettings()))
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	cur := m.GetSettings()
	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &settingsResp{
		Settings: settingsForAPI(cur),
		Warning:  SettingsWarning(cur),
	})
}

// settingsReq is the request body of the POST /control/users/settings HTTP
// API.  Every field is optional: an absent one keeps its current value, so a
// caller that changes a single setting cannot silently clear the others.  A
// plain [Settings] would replace everything, and clearing the portal origins
// by toggling an unrelated switch is not something an administrator can see.
type settingsReq struct {
	DenyUnmatched      *bool     `json:"deny_unmatched,omitempty"`
	UpdateProxy        *string   `json:"update_proxy,omitempty"`
	PortalOrigins      *[]string `json:"portal_origins,omitempty"`
	PortalAPIBase      *string   `json:"portal_api_base,omitempty"`
	PortalOpen         *bool     `json:"portal_open,omitempty"`
	PortalEmailVerify  *bool     `json:"portal_email_verify,omitempty"`
	PortalDefaultQuota *int64    `json:"portal_default_quota,omitempty"`
	PortalDefaultDays  *int64    `json:"portal_default_days,omitempty"`
	PortalAnnouncement *string   `json:"portal_announcement,omitempty"`
	SMTPHost           *string   `json:"smtp_host,omitempty"`
	SMTPPort           *int      `json:"smtp_port,omitempty"`
	SMTPUser           *string   `json:"smtp_user,omitempty"`
	SMTPPassword       *string   `json:"smtp_password,omitempty"`
	SMTPFrom           *string   `json:"smtp_from,omitempty"`
	SMTPPlain          *bool     `json:"smtp_plain,omitempty"`
}

// settingsResponse is the API representation of the settings.  The mail
// password is replaced by a flag, so that a secret which is never serialised
// cannot leak through a new field or a log line.
type settingsResponse struct {
	*Settings

	// SMTPPasswordSet is true when a mail password is stored.
	SMTPPasswordSet bool `json:"smtp_password_set"`
}

// settingsForAPI returns the settings as they are sent to a client.
//
// Embedding the settings promotes every field of them into the response, so
// the copy has to be stripped of the secret before it is embedded.  Clearing
// the field rather than relying on a JSON tag is what makes the secret
// impossible to serialise, whatever tag it ends up with.
func settingsForAPI(s *Settings) (r *settingsResponse) {
	cp := *s
	cp.PortalOrigins = slices.Clone(s.PortalOrigins)

	passwordSet := cp.SMTPPassword != ""
	cp.SMTPPassword = ""

	return &settingsResponse{
		Settings:        &cp,
		SMTPPasswordSet: passwordSet,
	}
}

// applyTo returns cur with the fields present in req replaced.
func (r *settingsReq) applyTo(cur *Settings) (s *Settings) {
	if r.DenyUnmatched != nil {
		cur.DenyUnmatched = *r.DenyUnmatched
	}

	if r.UpdateProxy != nil {
		cur.UpdateProxy = *r.UpdateProxy
	}

	if r.PortalOrigins != nil {
		cur.PortalOrigins = *r.PortalOrigins
	}

	if r.PortalAPIBase != nil {
		cur.PortalAPIBase = *r.PortalAPIBase
	}

	if r.PortalOpen != nil {
		cur.PortalOpen = *r.PortalOpen
	}

	if r.PortalEmailVerify != nil {
		cur.PortalEmailVerify = *r.PortalEmailVerify
	}

	if r.PortalDefaultQuota != nil {
		cur.PortalDefaultQuota = max(0, *r.PortalDefaultQuota)
	}

	if r.PortalDefaultDays != nil {
		cur.PortalDefaultDays = max(0, *r.PortalDefaultDays)
	}

	if r.PortalAnnouncement != nil {
		cur.PortalAnnouncement = *r.PortalAnnouncement
	}

	if r.SMTPHost != nil {
		cur.SMTPHost = strings.TrimSpace(*r.SMTPHost)
	}

	if r.SMTPPort != nil {
		cur.SMTPPort = *r.SMTPPort
	}

	if r.SMTPUser != nil {
		cur.SMTPUser = strings.TrimSpace(*r.SMTPUser)
	}

	// An empty password means "keep the stored one", so that the administrator
	// can change the host without retyping the secret.
	if r.SMTPPassword != nil && *r.SMTPPassword != "" {
		cur.SMTPPassword = *r.SMTPPassword
	}

	if r.SMTPFrom != nil {
		cur.SMTPFrom = strings.TrimSpace(*r.SMTPFrom)
	}

	if r.SMTPPlain != nil {
		cur.SMTPPlain = *r.SMTPPlain
	}

	return cur
}

// handleBulkAdd is the handler for the POST /control/users/bulk-add HTTP API.
func (m *Manager) handleBulkAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &bulkAddReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	res := m.BulkAdd(req.Text, enabled)
	if len(res.Errors) > 0 {
		// The payload is reported back field by field, so answer with 200 and
		// let the UI render the problems instead of showing a bare message.
		aghhttp.WriteJSONResponseOK(ctx, l, w, r, res)

		return
	}

	l.InfoContext(ctx, "users bulk added", "count", len(res.Users))

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, res)
}

// handleList is the handler for the GET /control/users/list HTTP API.
func (m *Manager) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	users := m.List()
	if users == nil {
		users = []*Info{}
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &listResp{
		Users:    users,
		Summary:  m.Summary(),
		Settings: settingsForAPI(m.GetSettings()),
		Domain:   m.Domain(),
	})
}

// handleExport is the handler for the GET /control/users/export HTTP API.
func (m *Manager) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	m.mu.Lock()
	users := make([]*User, 0, len(m.defs))
	for _, u := range m.defs {
		users = append(users, u)
	}
	m.mu.Unlock()

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, &importReq{Users: users})
}

// handleAdd is the handler for the POST /control/users/add HTTP API.
func (m *Manager) handleAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &addReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	u, err := m.Add(&AddParams{
		Name:         req.Name,
		Remark:       req.Remark,
		IDs:          req.IDs,
		RequestLimit: req.RequestLimit,
		Period:       req.Period,
		ExpireDays:   req.ExpireDays,
		Enabled:      enabled,
	})
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "user added", "uid", u.UID, "name", u.Name)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "user added",
		User:    m.InfoOf(u.UID),
	})
}

// handleUpdate is the handler for the POST /control/users/update HTTP API.
func (m *Manager) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &updateReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if req.UID == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "uid is empty")

		return
	}

	_, err = m.Update(req.UID, &UpdateParams{
		Name:         req.Name,
		Remark:       req.Remark,
		IDs:          req.IDs,
		RequestLimit: req.RequestLimit,
		Period:       req.Period,
		ExpireDays:   req.ExpireDays,
		ExtendDays:   req.ExtendDays,
		Enabled:      req.Enabled,
	})
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "user updated", "uid", req.UID)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "user updated",
		User:    m.InfoOf(req.UID),
	})
}

// handleDelete is the handler for the POST /control/users/delete HTTP API.
func (m *Manager) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &uidsReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if len(req.UIDs) == 0 {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "uids are empty")

		return
	}

	n := 0
	for _, uid := range req.UIDs {
		if err = m.Remove(uid); err == nil {
			n++
		}
	}

	l.InfoContext(ctx, "users removed", "count", n)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "users removed",
		Updated: n,
	})
}

// handleReset is the handler for the POST /control/users/reset HTTP API.
func (m *Manager) handleReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &uidsReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if len(req.UIDs) == 0 {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "uids are empty")

		return
	}

	n := m.Reset(req.UIDs)

	l.InfoContext(ctx, "user counters reset", "count", n)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "counters reset",
		Updated: n,
	})
}

// handleToggle is the handler for the POST /control/users/toggle HTTP API.
func (m *Manager) handleToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &toggleReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if len(req.UIDs) == 0 {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "uids are empty")

		return
	}

	n := m.SetEnabled(req.UIDs, req.Enabled)

	l.InfoContext(ctx, "users toggled", "count", n, "enabled", req.Enabled)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "users updated",
		Updated: n,
	})
}

// portalPasswordReq is the request body of the POST /control/users/password
// HTTP API.
type portalPasswordReq struct {
	// UID is the user to change the password of.
	UID string `json:"uid"`

	// Password is the new portal password.  An empty value revokes the
	// portal access of the user.
	Password string `json:"password"`
}

// handlePortalPassword is the handler for the POST /control/users/password
// HTTP API.  It sets or clears the password the user signs in to the user
// portal with.
func (m *Manager) handlePortalPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &portalPasswordReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if req.UID == "" {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "uid is empty")

		return
	}

	if req.Password == "" {
		err = m.ClearPortalPassword(req.UID)
	} else {
		err = m.SetPortalPassword(req.UID, req.Password)
	}

	if err != nil {
		// The error explains why the password was refused, which is useful
		// to the administrator and reveals nothing about other users.
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "portal password changed", "uid", req.UID, "cleared", req.Password == "")

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "portal password updated",
		User:    m.InfoOf(req.UID),
	})
}

// handleImport is the handler for the POST /control/users/import HTTP API.
func (m *Manager) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &importReq{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding request: %s", err)

		return
	}

	if !req.Replace {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusBadRequest,
			"import requires replace to be true",
		)

		return
	}

	n, err := m.ImportUsers(req.Users)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	l.InfoContext(ctx, "users imported", "count", n)

	aghhttp.WriteJSONResponseOK(ctx, l, w, r, &actionResp{
		OK:      true,
		Message: "users imported",
		Updated: n,
	})
}

// InfoOf returns the API representation of the user with the given UID, if
// any.
func (m *Manager) InfoOf(uid string) (i *Info) {
	snap := m.snap.Load()
	if snap == nil {
		return nil
	}

	for _, e := range snap.list {
		if e.user.UID == uid {
			return m.info(e)
		}
	}

	return nil
}
