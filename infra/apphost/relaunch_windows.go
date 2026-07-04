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
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	createNoWindow        = 0x08000000 // CREATE_NO_WINDOW — run a console app with no console window
)

// relaunchSelf spawns a fresh, detached copy of the app and returns immediately; the
// caller then exits. The flags are the crucial part:
//   - CREATE_NO_WINDOW: the child does NOT inherit the parent's console (so it isn't
//     killed when the parent exits — the reason a bare self-restart "didn't restart")
//     and, critically, Windows does NOT allocate a *new* console window for it. Using
//     DETACHED_PROCESS instead makes Windows give this console-subsystem binary a fresh
//     console window on every relaunch — the stray "DOS windows" a relaunch loop spams.
//   - CREATE_NEW_PROCESS_GROUP: isolates the child from console Ctrl+C/Break aimed at
//     the parent's group.
//
// The child waits briefly (KOPIV2_RESTART_DELAY_MS, honored in Run) so the parent has
// fully exited and released the listen port first.
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
		CreationFlags: createNoWindow | createNewProcessGroup,
	}
	// Detached: no inherited stdio (the app logs to its own file). Start and don't wait.
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
