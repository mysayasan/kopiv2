//go:build !windows

package apphost

// runWithPlatform runs the app directly on every non-Windows OS.
func runWithPlatform(app App) error {
	return runApp(app)
}

// platformShutdownChan has no extra shutdown source off Windows, so it returns a
// nil channel (a select on it never fires).
func platformShutdownChan() <-chan struct{} {
	return nil
}

// platformSupervised is false off Windows: systemd/launchd/Docker supervision is
// signalled via KOPIV2_SUPERVISED instead (there is no in-process way to detect it).
func platformSupervised() bool {
	return false
}
