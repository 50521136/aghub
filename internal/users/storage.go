package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// stateVersion is the version of the on-disk state format.
const stateVersion = 1

// stateFile is the on-disk state of the manager.
type stateFile struct {
	// Users are the user definitions.
	Users []*User `json:"users"`

	// Usage is the runtime usage by user UID.
	Usage map[string]*usageState `json:"usage"`

	// PortalPasswords are the bcrypt hashes of the user portal passwords by
	// user UID.  They are kept out of [User] so that they cannot leak through
	// the administrator API.
	PortalPasswords map[string]string `json:"portal_passwords,omitempty"`

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
		m.settings.Store(&Settings{
			DenyUnmatched: state.Settings.DenyUnmatched,
			UpdateProxy:   state.Settings.UpdateProxy,
		})
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
			us.dayStart.Store(st.DayStart)
			us.dayCount.Store(st.DayCount)

			if len(st.History) > 0 {
				us.history.Store(&st.History)
			}
		}

		m.usage[u.UID] = us
	}

	// The passwords of the users that are gone are dropped, so that deleting
	// and re-creating a user does not restore the old credentials.
	for uid, hash := range state.PortalPasswords {
		if hash == "" {
			continue
		}

		if _, ok := m.defs[uid]; ok {
			m.portalPasswords[uid] = hash
		}
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
			st := &usageState{
				Requests:    us.requests.Load(),
				Total:       us.total.Load(),
				PeriodStart: us.periodStart.Load(),
				LastSeen:    us.lastSeen.Load(),
				DayStart:    us.dayStart.Load(),
				DayCount:    us.dayCount.Load(),
			}

			if h := us.history.Load(); h != nil {
				st.History = *h
			}

			state.Usage[uid] = st
		}
	}

	if len(m.portalPasswords) > 0 {
		state.PortalPasswords = make(map[string]string, len(m.portalPasswords))
		maps.Copy(state.PortalPasswords, m.portalPasswords)
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

	// The state is safely on disk by now, so a failed backup must not fail
	// the save.  It is only reported.
	err = m.backup()
	if err != nil {
		m.logger.Error("backing up user state", "err", err)
	}

	return nil
}

// BackupDirName is the name of the directory that holds the dated backups of
// the state file.  It is created next to the state file.
const BackupDirName = "backup"

// backupKeep is the number of dated backups kept.  Since at most one backup
// per day is written, this is about a week of history.
const backupKeep = 7

// backupDayLayout is the layout of the date in the backup file names.  It sorts
// lexicographically in chronological order.
const backupDayLayout = "2006-01-02"

// backup writes a dated copy of the state file and removes the oldest backups
// beyond [backupKeep].  At most one backup per day is written.
//
// The backup is taken after a successful write, so it always holds a state
// that this program produced, and a state that it was able to read back.
func (m *Manager) backup() (err error) {
	return m.backupAs(m.now().In(m.loc).Format(backupDayLayout))
}

// backupAs is [Manager.backup] with an explicit label instead of the current
// date.  It is used for the states that are not tied to a day, like the one
// taken right before a restore.
func (m *Manager) backupAs(label string) (err error) {
	dir := filepath.Join(filepath.Dir(m.path), BackupDirName)

	err = os.MkdirAll(dir, 0o700)
	if err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	dst := filepath.Join(dir, fmt.Sprintf("%s.%s.bak", filepath.Base(m.path), label))

	_, err = os.Stat(dst)
	if err == nil {
		// That backup is already there.
		return nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking backup %s: %w", dst, err)
	}

	err = copyFile(m.path, dst)
	if err != nil {
		return fmt.Errorf("writing backup %s: %w", dst, err)
	}

	err = m.pruneBackups(dir)
	if err != nil {
		// The backup itself is written, so the pruning failure is only
		// reported.
		m.logger.Error("pruning user state backups", "err", err)
	}

	return nil
}

// pruneBackups removes the oldest backups in dir, keeping the newest
// [backupKeep] of them.
func (m *Manager) pruneBackups(dir string) (err error) {
	prefix := filepath.Base(m.path) + "."

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading backup directory: %w", err)
	}

	names := make([]string, 0, len(entries))

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() ||
			!strings.HasPrefix(name, prefix) ||
			!strings.HasSuffix(name, ".bak") {
			continue
		}

		names = append(names, name)
	}

	if len(names) <= backupKeep {
		return nil
	}

	// The names embed the date in a sortable layout, so sorting them
	// lexicographically orders them by age.
	slices.Sort(names)

	var errs []error

	for _, name := range names[:len(names)-backupKeep] {
		rerr := os.Remove(filepath.Join(dir, name))
		if rerr != nil {
			errs = append(errs, rerr)
		}
	}

	return errors.Join(errs...)
}

// copyFile copies src to dst, creating dst with mode 0o600.  The copy is
// flushed to disk before returning, so that the backup survives a crash.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		_ = out.Close()

		return fmt.Errorf("copying to %s: %w", dst, err)
	}

	err = out.Sync()
	if err != nil {
		_ = out.Close()

		return fmt.Errorf("syncing %s: %w", dst, err)
	}

	err = out.Close()
	if err != nil {
		return fmt.Errorf("closing %s: %w", dst, err)
	}

	return nil
}

// BackupInfo describes one dated backup of the state file.
type BackupInfo struct {
	// Name is the file name of the backup.  It is the argument of
	// [Manager.RestoreBackup].
	Name string `json:"name"`

	// Time is the modification time of the backup in Unix seconds.
	Time int64 `json:"time"`

	// Size is the size of the backup in bytes.
	Size int64 `json:"size"`

	// Users is the number of users in the backup.  It is negative when the
	// backup cannot be read.
	Users int `json:"users"`
}

// Backups returns the dated backups of the state file, newest first.  A missing
// backup directory is not an error and results in an empty list.
func (m *Manager) Backups() (infos []*BackupInfo, err error) {
	dir := filepath.Join(filepath.Dir(m.path), BackupDirName)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []*BackupInfo{}, nil
		}

		return nil, fmt.Errorf("users: reading backup directory: %w", err)
	}

	prefix := filepath.Base(m.path) + "."

	infos = make([]*BackupInfo, 0, len(entries))

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() ||
			!strings.HasPrefix(name, prefix) ||
			!strings.HasSuffix(name, ".bak") {
			continue
		}

		info, ierr := e.Info()
		if ierr != nil {
			continue
		}

		infos = append(infos, &BackupInfo{
			Name:  name,
			Time:  info.ModTime().Unix(),
			Size:  info.Size(),
			Users: countUsersIn(filepath.Join(dir, name)),
		})
	}

	// The names embed the date in a sortable layout, so sorting them in
	// reverse orders them from the newest to the oldest.
	slices.SortFunc(infos, func(a, b *BackupInfo) (cmp int) {
		return strings.Compare(b.Name, a.Name)
	})

	return infos, nil
}

// countUsersIn returns the number of users in the state file at path, or a
// negative number when it cannot be read.
func countUsersIn(path string) (n int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}

	state := &stateFile{}
	err = json.Unmarshal(data, state)
	if err != nil {
		return -1
	}

	return len(state.Users)
}

// RestoreBackup replaces all users with the ones from the named backup, which
// must be a name returned by [Manager.Backups].  It returns the number of the
// restored users.
//
// The state that is being replaced is backed up first, so that a restore can
// itself be undone by restoring the backup that it creates.
func (m *Manager) RestoreBackup(name string) (n int, err error) {
	// The name comes from the API, so it must not be able to point outside
	// the backup directory.
	if filepath.Base(name) != name || !strings.HasSuffix(name, ".bak") {
		return 0, fmt.Errorf("users: invalid backup name %q", name)
	}

	dir := filepath.Join(filepath.Dir(m.path), BackupDirName)

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return 0, fmt.Errorf("users: reading backup: %w", err)
	}

	state := &stateFile{}
	err = json.Unmarshal(data, state)
	if err != nil {
		return 0, fmt.Errorf("users: parsing backup: %w", err)
	}

	if len(state.Users) == 0 {
		return 0, fmt.Errorf("users: backup %q contains no users", name)
	}

	// Keep the state that is about to be replaced.  The label is dated, so it
	// is listed and pruned along with the daily ones, and the "-before-restore"
	// suffix keeps it from colliding with the backup of the same day.
	label := m.now().In(m.loc).Format(backupDayLayout) + "-before-restore"

	err = m.backupAs(label)
	if err != nil {
		m.logger.Error("backing up user state before restore", "err", err)
	}

	n, err = m.ImportUsers(state.Users)
	if err != nil {
		return 0, fmt.Errorf("users: restoring backup %q: %w", name, err)
	}

	m.logger.Info("restored user state from backup", "name", name, "users", n)

	return n, nil
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

	// The import replaces the whole user list, so the passwords of the users
	// that are gone must go with it.
	maps.DeleteFunc(m.portalPasswords, func(uid string, _ string) (del bool) {
		_, ok := defs[uid]

		return !ok
	})

	m.publishLocked()
	m.dirty.Store(true)

	return len(defs), nil
}
