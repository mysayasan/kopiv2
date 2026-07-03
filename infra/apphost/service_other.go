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
