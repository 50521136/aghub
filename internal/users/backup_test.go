package users

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backupPath returns the path of the backup for the given day.
func backupPath(t *testing.T, m *Manager, day time.Time) (path string) {
	t.Helper()

	return filepath.Join(
		filepath.Dir(m.Path()),
		BackupDirName,
		"users.json."+day.Format(backupDayLayout)+".bak",
	)
}

// addTestUser adds a single user to the manager.
func addTestUser(t *testing.T, m *Manager, name string) {
	t.Helper()

	_, err := m.Add(&AddParams{Name: name, IDs: []string{name}, Enabled: true})
	require.NoError(t, err)
}

func TestBackupOncePerDay(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	first := backupPath(t, m, *now)
	require.FileExists(t, first)

	content, err := os.ReadFile(first)
	require.NoError(t, err)
	assert.Contains(t, string(content), "alice")

	// A second save on the same day must not replace the backup, so that the
	// first state of the day survives until tomorrow.
	addTestUser(t, m, "bob")
	*now = now.Add(2 * time.Hour)
	require.NoError(t, m.save())

	same, err := os.ReadFile(first)
	require.NoError(t, err)
	assert.Equal(t, content, same)
	assert.NotContains(t, string(same), "bob")

	// The next day gets its own backup.
	*now = now.Add(24 * time.Hour)
	require.NoError(t, m.save())

	next := backupPath(t, m, *now)
	require.FileExists(t, next)
	assert.NotEqual(t, first, next)

	nextContent, err := os.ReadFile(next)
	require.NoError(t, err)
	assert.Contains(t, string(nextContent), "bob")
}

func TestBackupPrunesOld(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")

	for i := range backupKeep + 3 {
		if i > 0 {
			*now = now.Add(24 * time.Hour)
		}

		require.NoError(t, m.save())
	}

	dir := filepath.Join(filepath.Dir(m.Path()), BackupDirName)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, backupKeep)

	// The oldest days are the ones that went away.
	assert.NoFileExists(t, backupPath(t, m, now.Add(-time.Duration(backupKeep)*24*time.Hour)))
	assert.FileExists(t, backupPath(t, m, *now))
}

func TestBackupIgnoresForeignFiles(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	dir := filepath.Join(filepath.Dir(m.Path()), BackupDirName)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	// A file that does not look like a backup, and a directory, must both
	// survive the pruning.
	keep := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(keep, []byte("keep me"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "users.json.2020-01-01.bak"), 0o700))

	for i := range backupKeep + 3 {
		if i > 0 {
			*now = now.Add(24 * time.Hour)
		}

		require.NoError(t, m.save())
	}

	assert.FileExists(t, keep)
	assert.DirExists(t, filepath.Join(dir, "users.json.2020-01-01.bak"))
}

func TestBackupFailureDoesNotFailSave(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")

	// A file where the backup directory should be makes the backup fail.
	require.NoError(t, os.WriteFile(
		filepath.Join(filepath.Dir(m.Path()), BackupDirName),
		[]byte("not a directory"),
		0o600,
	))

	// The state itself is still written.
	require.NoError(t, m.save())
	assert.FileExists(t, m.Path())

	reopened, err := New(&Config{Logger: testLogger(), Path: m.Path()})
	require.NoError(t, err)
	assert.Len(t, reopened.List(), 1)
}

func TestBackupsListAndRestore(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	// The next day a second user appears.
	*now = now.Add(24 * time.Hour)
	addTestUser(t, m, "bob")
	require.NoError(t, m.save())

	backups, err := m.Backups()
	require.NoError(t, err)
	require.Len(t, backups, 2)

	// The list is ordered from the newest backup to the oldest.
	assert.Equal(t, "users.json."+now.Format(backupDayLayout)+".bak", backups[0].Name)
	assert.Equal(t, 2, backups[0].Users)
	assert.Equal(t, 1, backups[1].Users)
	assert.Positive(t, backups[0].Size)

	// Restoring the older backup brings the state back to a single user.
	n, err := m.RestoreBackup(backups[1].Name)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	users := m.List()
	require.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)

	// The state that was replaced is kept, so the restore can be undone.
	backups, err = m.Backups()
	require.NoError(t, err)
	require.Len(t, backups, 3)

	var beforeRestore *BackupInfo
	for _, b := range backups {
		if strings.Contains(b.Name, "before-restore") {
			beforeRestore = b
		}
	}

	require.NotNil(t, beforeRestore)
	assert.Equal(t, 2, beforeRestore.Users)

	n, err = m.RestoreBackup(beforeRestore.Name)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Len(t, m.List(), 2)
}

func TestBackupsMissingDirectory(t *testing.T) {
	m, _ := newTestManager(t)

	backups, err := m.Backups()
	require.NoError(t, err)
	assert.Empty(t, backups)
}

func TestRestoreBackupRejectsBadName(t *testing.T) {
	m, _ := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	require.Len(t, m.List(), 1)

	for _, name := range []string{
		"",
		"../../etc/passwd",
		"/etc/passwd.bak",
		"users.json.txt",
		".",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := m.RestoreBackup(name)
			assert.Error(t, err)

			// Nothing was replaced.
			assert.Len(t, m.List(), 1)
		})
	}
}
