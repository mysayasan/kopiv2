//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// createNoWindow = CREATE_NO_WINDOW: run a console child with no console window.
const createNoWindow = 0x08000000

// HideWindow makes a child console process (ffmpeg, ffprobe, python, nvidia-smi, …)
// run without popping a console window on Windows. A console app whose parent has no
// console of its own — which is the case after a detached self-restart, when the app
// runs as a Windows service, or from some launchers — otherwise gets a brand-new
// console window allocated on every spawn. A per-camera stream/recording pipeline that
// retries a failing ffmpeg then turns that into an endless storm of "DOS" windows
// (notably after restoring a backup onto a machine where some camera URLs don't
// resolve). Apply this to every exec.Cmd before Start/Run/Output. Existing flags are
// preserved. No-op on other operating systems.
func HideWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
