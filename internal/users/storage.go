package users

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// stateVersion is the version of the on-disk state format.
const stateVersion = 1

// stateFile is the on-disk state of the manager.
type stateFile struct {
	// Users are the user definitions.
	Users []*User `json:"users"`

	// Usage is the runtime usage by user UID.
	Usage map[string]*usageState `json:"usage"`

	// Settings is the manager-wide configuration.
	Settings *Settings `json:"settings,omitempty"`

	// Version is the version of the state format.
	Version int `json:"version"`
}

// load reads the state from the state file.  A missing file is not an error.
//
// m.mu is expected to be held by the caller.
func (m *Manager) load() (err error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Debug("no user state file, starting empty", "path", m.path)

			return nil
		}

		return fmt.Errorf("reading user state: %w", err)
	}

	state := &stateFile{}
	err = json.Unmarshal(data, state)
	if err != nil {
		return fmt.Errorf("decoding user state: %w", err)
	}

	if state.Version > stateVersion {
		return fmt.Errorf("user state version %d is newer than %d", state.Version, stateVersion)
	}

	if state.Settings != nil {
		m.settings.Store(&Settings{DenyUnmatched: state.Settings.DenyUnmatched})
	}

	for _, u := range state.Users {
		if u == nil || u.UID == "" {
			continue
		}

		u.RequestLimit = max(Unlimited, u.RequestLimit)
		u.Period = normalizePeriod(u.Period)
		u.IDs = slices.Compact(slices.Clone(u.IDs))
		m.defs[u.UID] = u

		us := &usage{}
		if st := state.Usage[u.UID]; st != nil {
			us.requests.Store(st.Requests)
			us.total.Store(st.Total)
			us.periodStart.Store(st.PeriodStart)
			us.lastSeen.Store(st.LastSeen)
		}

		m.usage[u.UID] = us
	}

	m.logger.Info("loaded user state", "path", m.path, "users", len(m.defs))

	return nil
}

// save writes the state to the state file atomically.
func (m *Manager) save() (err error) {
	m.mu.Lock()

	state := &stateFile{
		Version:  stateVersion,
		Users:    make([]*User, 0, len(m.defs)),
		Usage:    make(map[string]*usageState, len(m.usage)),
		Settings: m.GetSettings(),
	}

	for uid, u := range m.defs {
		state.Users = append(state.Users, u)

		if us := m.usage[uid]; us != nil {
			state.Usage[uid] = &usageState{
				Requests:    us.requests.Load(),
				Total:       us.total.Load(),
				PeriodStart: us.periodStart.Load(),
				LastSeen:    us.lastSeen.Load(),
			}
		}
	}

	m.mu.Unlock()

	slices.SortFunc(state.Users, func(a, b *User) (cmp int) {
		return int(a.CreatedAt - b.CreatedAt)
	})

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding user state: %w", err)
	}

	dir := filepath.Dir(m.path)

	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("creating user state directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".users-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary user state file: %w", err)
	}

	tmpName := tmp.Name()

	defer func() {
		// Clean up the temporary file if it is still there, which means that
		// the rename below has not happened.
		_ = os.Remove(tmpName)
	}()

	_, err = tmp.Write(data)
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing user state: %w", err)
	}

	err = tmp.Sync()
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("syncing user state: %w", err)
	}

	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("closing user state: %w", err)
	}

	err = os.Chmod(tmpName, 0o600)
	if err != nil {
		return fmt.Errorf("chmod user state: %w", err)
	}

	err = os.Rename(tmpName, m.path)
	if err != nil {
		return fmt.Errorf("replacing user state: %w", err)
	}

	return nil
}

// ImportUsers replaces all users with the given ones.  It is used by the
// import API and returns the number of imported users.
func (m *Manager) ImportUsers(users []*User) (n int, err error) {
	if len(users) == 0 {
		return 0, fmt.Errorf("users: no users to import")
	}

	now := m.now()

	defs := make(map[string]*User, len(users))
	usages := make(map[string]*usage, len(users))
	seenIDs := map[string]string{}

	for i, u := range users {
		if u == nil {
			continue
		}

		name := u.Name
		if name == "" {
			name = fmt.Sprintf("user-%d", i+1)
		}

		ids, nerr := normalizeIDs(u.IDs)
		if nerr != nil {
			return 0, fmt.Errorf("users: user %q: %w", name, nerr)
		}

		if len(ids) == 0 {
			return 0, fmt.Errorf("users: user %q has no valid identifiers", name)
		}

		for _, id := range ids {
			if owner, ok := seenIDs[id]; ok {
				return 0, fmt.Errorf("users: identifier %q is used by both %q and %q", id, owner, name)
			}

			seenIDs[id] = name
		}

		uid := u.UID
		if uid == "" {
			uid, err = NewUID()
			if err != nil {
				return 0, err
			}
		}

		if _, ok := defs[uid]; ok {
			return 0, fmt.Errorf("users: duplicated uid %q", uid)
		}

		createdAt := u.CreatedAt
		if createdAt <= 0 {
			createdAt = now.Unix()
		}

		defs[uid] = &User{
			UID:          uid,
			Name:         name,
			Remark:       u.Remark,
			IDs:          ids,
			RequestLimit: max(Unlimited, u.RequestLimit),
			Period:       normalizePeriod(u.Period),
			ExpiresAt:    max(0, u.ExpiresAt),
			CreatedAt:    createdAt,
			Enabled:      u.Enabled,
		}

		usages[uid] = &usage{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.defs = defs
	m.usage = usages
	m.publishLocked()
	m.dirty.Store(true)

	return len(defs), nil
}
