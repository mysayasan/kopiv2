package apphost

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Opening a browser is a convenience for the person who just typed the start command.
// It is NOT something to do on every kind of start: a Windows service has no desktop to
// open one on, a container has no browser at all, and a self-restart after a settings
// change would pop a new tab every time the operator saves. Each of those is a guard
// below rather than an accepted annoyance.

// relaunchedByRestart records that this process was started by a previous instance's
// self-restart. Set once in runApp, because the marker environment variable is unset
// there before anything else can read it.
var relaunchedByRestart bool

// browserOpener is the process launcher, swappable so tests can assert what would be
// run without actually opening anything.
var browserOpener = startDetached

// browserSuppressedBecause returns a human-readable reason not to open a browser, or ""
// when opening is appropriate. The reason is printed in the ready banner, so an operator
// who expected a browser is told why they did not get one instead of wondering.
func browserSuppressedBecause() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KOPIV2_OPEN_BROWSER"))) {
	case "0", "false", "no", "off", "never":
		return "KOPIV2_OPEN_BROWSER is off"
	case "1", "true", "yes", "on", "force":
		// An explicit request wins over the heuristics below. Someone running in an
		// unusual setup (an X-forwarded session, a remote desktop) knows better than
		// the guesses this file can make.
		return ""
	}
	if strings.TrimSpace(os.Getenv("KOPIV2_NO_BROWSER")) != "" {
		return "KOPIV2_NO_BROWSER is set"
	}
	if platformSupervised() {
		return "running as a service"
	}
	if strings.TrimSpace(os.Getenv("KOPIV2_SUPERVISED")) != "" {
		return "running under a process supervisor"
	}
	if relaunchedByRestart {
		return "this is a restart, not a fresh start"
	}
	if inContainer() {
		return "running in a container"
	}
	if !hasDesktopSession() {
		return "no desktop session on this host"
	}
	return ""
}

// launchBrowser opens url in the operator's default browser unless a guard says not
// to. It reports whether it opened and, when it did not, why — so every caller can put
// the reason in front of the operator instead of leaving them wondering where their
// browser went.
func launchBrowser(url string) (opened bool, reason string) {
	if reason := browserSuppressedBecause(); reason != "" {
		return false, reason
	}
	if err := browserOpener(url); err != nil {
		return false, fmt.Sprintf("could not start a browser (%v)", err)
	}
	return true, ""
}

func startDetached(url string) error {
	switch runtime.GOOS {
	case "windows":
		// rundll32, not `cmd /c start`: the cmd form needs an empty-title argument to
		// survive a quoted URL, and it flashes a console window on a GUI-less start —
		// the same console-window nuisance the self-restart path already had to fix.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// inContainer reports the common container markers. It is a best-effort check: a false
// negative only means a browser launch that fails harmlessly and is logged.
func inContainer() bool {
	if strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")) != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return false
}

// hasDesktopSession reports whether there is a GUI to open a browser on. Windows and
// macOS interactive processes always have one (the service case is caught earlier); on
// Unix it takes a display server, which a headless server or an SSH session lacks.
func hasDesktopSession() bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return true
	}
	return strings.TrimSpace(os.Getenv("DISPLAY")) != "" ||
		strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != ""
}
