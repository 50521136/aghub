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

// backupPath returns the path of the daily backup for the given day.
func backupPath(t *testing.T, m *Manager, day time.Time) (path string) {
	t.Helper()

	return filepath.Join(
		filepath.Dir(m.Path()),
		BackupDirName,
		"users.json."+day.Format(backupDayLayout)+".bak",
	)
}

// hourlyBackupPath returns the path of the hourly backup for the given time.
func hourlyBackupPath(t *testing.T, m *Manager, at time.Time) (path string) {
	t.Helper()

	return filepath.Join(
		filepath.Dir(m.Path()),
		BackupDirName,
		"users.json."+at.Format(backupHourLayout)+".bak",
	)
}

// backupsByTier returns the backup file names grouped by their retention tier.
func backupsByTier(t *testing.T, m *Manager) (perTier map[string][]string) {
	t.Helper()

	dir := filepath.Join(filepath.Dir(m.Path()), BackupDirName)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	prefix := filepath.Base(m.Path()) + "."

	perTier = map[string][]string{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() ||
			!strings.HasPrefix(name, prefix) ||
			!strings.HasSuffix(name, ".bak") {
			continue
		}

		label := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".bak")
		tier, _ := backupTier(label)

		perTier[tier] = append(perTier[tier], name)
	}

	return perTier
}

// backupTierName returns the retention tier of the backup with the given file
// name.  The test manager's state file is always named users.json.
func backupTierName(name string) (tier string) {
	label := strings.TrimSuffix(strings.TrimPrefix(name, "users.json."), ".bak")

	tier, _ = backupTier(label)

	return tier
}

// addTestUser adds a single user to the manager.
func addTestUser(t *testing.T, m *Manager, name string) {
	t.Helper()

	_, err := m.Add(&AddParams{Name: name, IDs: []string{name}, Enabled: true})
	require.NoError(t, err)
}

// TestBackupWritesBothTiers checks that a single save produces the hourly
// snapshot that bounds the data loss and the daily one that keeps the history.
func TestBackupWritesBothTiers(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	require.FileExists(t, hourlyBackupPath(t, m, *now))
	require.FileExists(t, backupPath(t, m, *now))

	hourly, err := os.ReadFile(hourlyBackupPath(t, m, *now))
	require.NoError(t, err)
	daily, err := os.ReadFile(backupPath(t, m, *now))
	require.NoError(t, err)
	assert.Equal(t, hourly, daily)
	assert.Contains(t, string(hourly), "alice")
}

// TestBackupOncePerHourAndPerDay checks that the snapshots of a period are not
// replaced by the later ones, so that the state from the start of the period
// survives until the period rolls over.
func TestBackupOncePerHourAndPerDay(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	firstHour := hourlyBackupPath(t, m, *now)
	firstDay := backupPath(t, m, *now)

	hourContent, err := os.ReadFile(firstHour)
	require.NoError(t, err)
	dayContent, err := os.ReadFile(firstDay)
	require.NoError(t, err)

	// A second save within the same hour and day must not replace either
	// snapshot.
	addTestUser(t, m, "bob")
	*now = now.Add(20 * time.Minute)
	require.NoError(t, m.save())

	sameHour, err := os.ReadFile(firstHour)
	require.NoError(t, err)
	sameDay, err := os.ReadFile(firstDay)
	require.NoError(t, err)
	assert.Equal(t, hourContent, sameHour)
	assert.Equal(t, dayContent, sameDay)
	assert.NotContains(t, string(sameHour), "bob")

	// The next hour gets its own snapshot, and it does hold bob.
	*now = now.Add(40 * time.Minute)
	require.NoError(t, m.save())

	nextHour := hourlyBackupPath(t, m, *now)
	require.FileExists(t, nextHour)
	assert.NotEqual(t, firstHour, nextHour)

	nextHourContent, err := os.ReadFile(nextHour)
	require.NoError(t, err)
	assert.Contains(t, string(nextHourContent), "bob")

	// The daily snapshot of the day is still the first one of that day.
	sameDayAgain, err := os.ReadFile(firstDay)
	require.NoError(t, err)
	assert.Equal(t, dayContent, sameDayAgain)
}

// TestBackupPrunesDailyTier checks that the daily tier is capped on its own.
func TestBackupPrunesDailyTier(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")

	for i := range backupKeepDaily + 3 {
		if i > 0 {
			*now = now.Add(24 * time.Hour)
		}

		require.NoError(t, m.save())
	}

	tiers := backupsByTier(t, m)
	assert.Len(t, tiers["daily"], backupKeepDaily)

	// The oldest days are the ones that went away.
	assert.NoFileExists(t, backupPath(t, m, now.Add(-time.Duration(backupKeepDaily)*24*time.Hour)))
	assert.FileExists(t, backupPath(t, m, *now))
}

// TestBackupPrunesHourlyTier checks that the hourly tier is capped on its own.
func TestBackupPrunesHourlyTier(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")

	for i := range backupKeepHourly + 5 {
		if i > 0 {
			*now = now.Add(time.Hour)
		}

		require.NoError(t, m.save())
	}

	tiers := backupsByTier(t, m)
	assert.Len(t, tiers["hourly"], backupKeepHourly)

	// The oldest hours are the ones that went away.
	assert.NoFileExists(t, hourlyBackupPath(t, m, now.Add(-time.Duration(backupKeepHourly)*time.Hour)))
	assert.FileExists(t, hourlyBackupPath(t, m, *now))
}

// TestBackupTiersAreCountedSeparately checks that a burst of hourly snapshots
// does not push the daily history out.  With a single shared cap the oldest
// daily backup here would be evicted by the hourly ones.
func TestBackupTiersAreCountedSeparately(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	firstDay := backupPath(t, m, *now)
	require.FileExists(t, firstDay)

	// More hourly snapshots than the hourly tier keeps, so the hourly cap is
	// certainly hit.
	for range backupKeepHourly + 5 {
		*now = now.Add(time.Hour)
		require.NoError(t, m.save())
	}

	tiers := backupsByTier(t, m)
	assert.Len(t, tiers["hourly"], backupKeepHourly)
	assert.NotEmpty(t, tiers["daily"])

	// The daily snapshot of the first day is still there.
	assert.FileExists(t, firstDay)
}

// TestBackupIgnoresForeignFiles checks that the pruning leaves everything that
// is not one of our backups alone.
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

	for i := range backupKeepDaily + 3 {
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

	// Each day left one daily and one hourly snapshot.
	backups, err := m.Backups()
	require.NoError(t, err)
	require.Len(t, backups, 4)

	// The list is ordered from the newest backup to the oldest, so the
	// newest daily one comes before the older daily one.
	var dailies []*BackupInfo
	for _, b := range backups {
		if backupTierName(b.Name) == "daily" {
			dailies = append(dailies, b)
		}
	}

	require.Len(t, dailies, 2)
	assert.Equal(t, "users.json."+now.Format(backupDayLayout)+".bak", dailies[0].Name)
	assert.Equal(t, 2, dailies[0].Users)
	assert.Equal(t, 1, dailies[1].Users)
	assert.Positive(t, dailies[0].Size)

	// Restoring the older backup brings the state back to a single user.
	n, err := m.RestoreBackup(dailies[1].Name)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	users := m.List()
	require.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)

	// The state that was replaced is kept, so the restore can be undone.
	backups, err = m.Backups()
	require.NoError(t, err)

	var beforeRestore *BackupInfo
	for _, b := range backups {
		if strings.Contains(b.Name, backupRestoreSuffix) {
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

// TestBackupEveryRestoreIsKept checks that a second restore on the same day
// still gets its own pre-restore snapshot instead of colliding with the first
// one.
func TestBackupEveryRestoreIsKept(t *testing.T) {
	m, now := newTestManager(t)

	addTestUser(t, m, "alice")
	require.NoError(t, m.save())

	*now = now.Add(24 * time.Hour)
	addTestUser(t, m, "bob")
	require.NoError(t, m.save())

	backups, err := m.Backups()
	require.NoError(t, err)

	var dailies []string
	for _, b := range backups {
		if backupTierName(b.Name) == "daily" {
			dailies = append(dailies, b.Name)
		}
	}

	require.Len(t, dailies, 2)

	// Two restores on the same day.  The label carries the second, so the
	// second restore gets its own snapshot instead of colliding with the one
	// the first restore left.  The old day-precision label collided here.
	_, err = m.RestoreBackup(dailies[1])
	require.NoError(t, err)

	*now = now.Add(time.Second)

	_, err = m.RestoreBackup(dailies[0])
	require.NoError(t, err)

	backups, err = m.Backups()
	require.NoError(t, err)

	var restores []string
	for _, b := range backups {
		if backupTierName(b.Name) == "restore" {
			restores = append(restores, b.Name)
		}
	}

	assert.Len(t, restores, 2)
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
