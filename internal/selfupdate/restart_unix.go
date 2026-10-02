//go:build unix

package selfupdate

import (
	"log/slog"
	"os"
	"syscall"
)

// Restart replaces the current process with the executable at execPath,
// keeping the arguments and the environment.  It does not return on success.
//
// This is the preferred restart strategy on Unix: the process image is
// replaced in place, so the PID, the open sockets and the systemd unit stay
// the same and there is no downtime.
func Restart(logger *slog.Logger, execPath string) (err error) {
	logger.Info("restarting", "path", execPath)

	err = syscall.Exec(execPath, os.Args, os.Environ())
	if err != nil {
		return err
	}

	return nil
}
