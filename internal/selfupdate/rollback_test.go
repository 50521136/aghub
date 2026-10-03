package selfupdate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRollbackUpdater returns an updater whose executable is a temporary file
// with the given content.
func newRollbackUpdater(t *testing.T, content []byte) (u *Updater, execPath string) {
	t.Helper()

	execPath = filepath.Join(t.TempDir(), "aghub")
	require.NoError(t, os.WriteFile(execPath, content, 0o755))

	u = &Updater{
		logger:   testLogger,
		status:   &Status{Progress: -1},
		execPath: execPath,
	}

	return u, execPath
}

func TestBackupUnavailable(t *testing.T) {
	u, _ := newRollbackUpdater(t, []byte("current"))

	b := u.Backup()
	assert.False(t, b.Available)
	assert.Empty(t, b.Version)
	assert.Zero(t, b.Size)
}

func TestBackupReportsTheStoredVersion(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))

	content := []byte("previous")
	require.NoError(t, os.WriteFile(execPath+backupSuffix, content, 0o755))
	require.NoError(t, writeBackupInfo(execPath+backupInfoSuffix, &backupInfo{
		Version: "v1.0.3",
		SavedAt: 1790956800,
	}))

	b := u.Backup()
	require.True(t, b.Available)
	assert.Equal(t, "v1.0.3", b.Version)
	assert.Equal(t, int64(1790956800), b.SavedAt)
	assert.Equal(t, int64(len(content)), b.Size)
}

func TestBackupWithoutDescription(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o755))

	// A backup that was made by a build predating the description still has to
	// be usable, only its version is unknown.
	b := u.Backup()
	require.True(t, b.Available)
	assert.Empty(t, b.Version)
	assert.NotZero(t, b.Size)
}

func TestBackupIgnoresBrokenDescription(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o755))
	require.NoError(t, os.WriteFile(execPath+backupInfoSuffix, []byte("not json"), 0o644))

	b := u.Backup()
	require.True(t, b.Available)
	assert.Empty(t, b.Version)
}

func TestBackupIgnoresDirectory(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.Mkdir(execPath+backupSuffix, 0o755))

	assert.False(t, u.Backup().Available)
}

func TestRollbackSwapsTheBinaries(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))

	// This is the state the updater leaves behind: the previous binary is
	// kept next to the current one.
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o755))

	require.NoError(t, u.Rollback(t.Context()))

	got, err := os.ReadFile(execPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("previous"), got)

	// The replaced binary became the new backup, so that the rollback can be
	// undone by rolling back again.
	got, err = os.ReadFile(execPath + backupSuffix)
	require.NoError(t, err)
	assert.Equal(t, []byte("current"), got)

	// No temporary file is left behind.
	_, err = os.Stat(execPath + ".rollback-tmp")
	assert.True(t, os.IsNotExist(err))

	// Rolling back again goes forward.
	require.NoError(t, u.Rollback(t.Context()))

	got, err = os.ReadFile(execPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("current"), got)

	got, err = os.ReadFile(execPath + backupSuffix)
	require.NoError(t, err)
	assert.Equal(t, []byte("previous"), got)
}

func TestRollbackRecordsTheVersion(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o755))

	require.NoError(t, u.Rollback(t.Context()))

	// The version of the replaced binary is known because it is the one this
	// process is running.
	b := u.Backup()
	require.True(t, b.Available)
	assert.Equal(t, Version, b.Version)
	assert.NotZero(t, b.SavedAt)
}

func TestRollbackKeepsTheModeExecutable(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o600))

	require.NoError(t, u.Rollback(t.Context()))

	got, err := os.ReadFile(execPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("previous"), got)

	// The replaced binary is kept as an executable, otherwise the next
	// rollback would restore a binary that cannot be started.
	st, err := os.Stat(execPath + backupSuffix)
	require.NoError(t, err)
	assert.NotZero(t, st.Mode().Perm()&0o100)
}

func TestRollbackWithoutBackup(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))

	err := u.Rollback(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backup")

	// The executable must be untouched.
	got, rerr := os.ReadFile(execPath)
	require.NoError(t, rerr)
	assert.Equal(t, []byte("current"), got)

	// The check runs before the status is touched, exactly like the one in
	// Apply, so the caller gets the error directly instead of polling for it.
	assert.False(t, u.Status().Running)
	assert.Empty(t, u.Status().Error)
}

func TestRollbackReportsAFailureInTheStatus(t *testing.T) {
	u, execPath := newRollbackUpdater(t, []byte("current"))
	require.NoError(t, os.WriteFile(execPath+backupSuffix, []byte("previous"), 0o755))

	// Making the executable a directory makes the first step of the swap fail
	// the way a read error would.
	require.NoError(t, os.Remove(execPath))
	require.NoError(t, os.Mkdir(execPath, 0o755))

	err := u.Rollback(t.Context())
	require.Error(t, err)

	s := u.Status()
	assert.False(t, s.Running)
	assert.NotEmpty(t, s.Error)

	// The backup is still in place, so the rollback can be retried.
	_, serr := os.Stat(execPath + backupSuffix)
	assert.NoError(t, serr)
}
