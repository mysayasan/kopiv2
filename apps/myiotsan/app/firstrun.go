package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
)

// firstRunCredentialFile is the recovery file (in the data dir) holding the bootstrap
// login, so it survives the console banner scrolling away.
const firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"

// announceFirstRunAdmin surfaces the bootstrap admin credential established on a fresh
// install. It writes a recovery file in the data dir — the reliable, platform-independent
// place to find the login, since a Windows service console is invisible and a Docker or
// journal banner scrolls away — and prints a console banner.
//
// The password is echoed only when it was GENERATED; a config- or env-supplied one is
// pointed at rather than logged, because the operator already knows it and writing it to a
// log file is a leak. The account is must-change on first sign-in either way.
func announceFirstRunAdmin(deps apphost.Dependencies, seed sharedservices.AdminSeedResult) {
	url := firstRunConsoleURL(deps.Config)
	credPath := filepath.Join(deps.DataDir, firstRunCredentialFile)
	saved := writeFirstRunCredentialFile(credPath, url, seed) == nil
	if !saved {
		deps.Logger.Warnf("myiotsan.setup", "could not write credential recovery file %s", credPath)
	}

	const bar = "======================================================================"
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprint(&b, "  MyIotSan is ready.\n\n")
	fmt.Fprintf(&b, "  Open:      %s\n", url)
	fmt.Fprintf(&b, "  Username:  %s\n", seed.Username)
	if seed.Generated {
		fmt.Fprintf(&b, "  Password:  %s\n", seed.Password)
		fmt.Fprint(&b, "\n  This one-time password was generated for this install; you must set\n")
		fmt.Fprint(&b, "  your own on first sign-in.\n")
	} else {
		fmt.Fprint(&b, "  Password:  the value from your config.json localAuth / LOCAL_ADMIN_PASSWORD\n")
		fmt.Fprint(&b, "\n  You must change it on first sign-in.\n")
	}
	if saved {
		fmt.Fprintf(&b, "\n  Saved to:  %s  (delete after you sign in)\n", credPath)
	}
	fmt.Fprintf(&b, "%s\n", bar)
	// stdout so it lands verbatim in `docker logs` / journal / the terminal, then a
	// password-free line for the persistent log.
	fmt.Print(b.String())
	deps.Logger.Infof("myiotsan.setup", "bootstrap admin %q established (must change on next login); sign-in details written to %s", seed.Username, credPath)
}

// firstRunConsoleURL guesses the address an operator should open, from the configured
// listeners. It prefers TLS, since that is what the app ships with.
func firstRunConsoleURL(cfg *config.AppConfigModel) string {
	if cfg == nil {
		return "https://localhost:3003"
	}
	if len(cfg.Server.TLSPorts) > 0 {
		return fmt.Sprintf("https://localhost:%d", cfg.Server.TLSPorts[0])
	}
	if len(cfg.Server.NonTLSPorts) > 0 {
		return fmt.Sprintf("http://localhost:%d", cfg.Server.NonTLSPorts[0])
	}
	return "https://localhost:3003"
}

// writeFirstRunCredentialFile persists the bootstrap login where an operator can find it.
// It is written 0600 and the banner tells them to delete it after signing in.
func writeFirstRunCredentialFile(path, url string, seed sharedservices.AdminSeedResult) error {
	var b strings.Builder
	fmt.Fprint(&b, "MyIotSan initial administrator login\n")
	fmt.Fprint(&b, "===================================\n\n")
	fmt.Fprintf(&b, "Open:     %s\n", url)
	fmt.Fprintf(&b, "Username: %s\n", seed.Username)
	if seed.Generated {
		fmt.Fprintf(&b, "Password: %s\n", seed.Password)
	} else {
		fmt.Fprint(&b, "Password: the value configured in config.json localAuth / LOCAL_ADMIN_PASSWORD\n")
	}
	fmt.Fprint(&b, "\nYou must change this password on first sign-in.\n")
	fmt.Fprint(&b, "Delete this file once you have signed in.\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
