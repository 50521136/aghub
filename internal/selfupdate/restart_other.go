//go:build !unix

package selfupdate

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

// Restart starts the executable at execPath as a new process and exits the
// current one.  On non-Unix systems the process image cannot be replaced in
// place.
func Restart(logger *slog.Logger, execPath string) (err error) {
	logger.Info("restarting", "path", execPath)

	cmd := exec.Command(execPath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("selfupdate: starting new process: %w", err)
	}

	os.Exit(0)

	return nil
}
