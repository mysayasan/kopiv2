//go:build !windows

package procutil

import "os/exec"

// HideWindow is a no-op off Windows: there are no console windows to suppress, and
// child stdout/stderr are captured via pipes rather than a terminal.
func HideWindow(cmd *exec.Cmd) {}
