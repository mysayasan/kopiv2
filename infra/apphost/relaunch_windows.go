//go:build windows

package apphost

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Windows process-creation flags (not all are named in the syscall package).
const (
	detachedProcess       = 0x00000008 // DETACHED_PROCESS — child gets no console
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
)

// relaunchSelf spawns a fresh, fully DETACHED copy of the app and returns immediately;
// the caller then exits. The detach flags are the crucial part: without them the child
// shares the parent's console and is killed when the parent exits (the reason a bare
// Windows self-restart "didn't restart"). The child waits briefly (KOPIV2_RESTART_DELAY_MS,
// honored in Run) so the parent has fully exited and released the listen port first.
func relaunchSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = filepath.Dir(exe)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "KOPIV2_RESTART_DELAY_MS=1500")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	// Detached: no inherited stdio (the app logs to its own file). Start and don't wait.
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
