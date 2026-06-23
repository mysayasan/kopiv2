//go:build !windows

package apphost

import (
	"os"
	"syscall"
)

// relaunchSelf re-execs the app in place via execve: it REPLACES the current process
// image (same pid) with a fresh copy, so there is no parent/child port hand-off race
// and no chance of two instances. The listen sockets are already closed before this is
// called, so the re-exec'd image rebinds cleanly. On success it never returns.
func relaunchSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
