package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/config"
)

// firstRunCredentialFile is the recovery file (in the data dir) that holds the
// bootstrap login, so it survives the console banner scrolling away.
const firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"

// adminResetMarkerFile is the one-shot marker the installer's "reset admin login"
// option drops in the data dir to request a password reset on next start. It is
// also the documented manual recovery path for a locked-out operator.
const adminResetMarkerFile = "RESET_ADMIN"

// mfaResetMarkerFile is the one-shot marker an operator drops in the data dir to
// clear the stock superadmin's second factor on next start — the documented escape
// hatch for the "sole superadmin lost the authenticator AND the recovery codes"
// lockout. It resets ONLY the second factor, not the password.
const mfaResetMarkerFile = "RESET_MFA"

// consumeMfaResetMarker clears the stock superadmin's enrolled second factor when
// the RESET_MFA marker is present, so a lost-device lockout of the sole superadmin
// can be recovered offline (mirrors the RESET_ADMIN password-recovery path). The
// marker is deleted BEFORE the reset runs, so a crash can never silently re-clear
// MFA on a later restart. No-op (nil) when the marker is absent.
//
// audit may be nil, but should not be: this is the one path that removes the second factor
// from the most privileged account on the server without anybody signing in, and it deletes
// the only other evidence the factor existed. The application log records it too, but an
// application log is not the security trail an operator reviews — or retains.
func consumeMfaResetMarker(deps apphost.Dependencies, mfaService services.IMfaService, users services.IUserLoginService, audit services.IAuditService) error {
	marker := filepath.Join(deps.DataDir, mfaResetMarkerFile)
	if _, err := os.Stat(marker); err != nil {
		return nil // absent marker is the normal path
	}
	if err := os.Remove(marker); err != nil {
		return fmt.Errorf("remove mfa reset marker %s: %w", marker, err)
	}
	ctx := context.Background()
	user, err := users.GetByEmail(ctx, deps.Config.LocalAuth.Username)
	if err != nil || user == nil || user.Id == 0 {
		log.Printf("WARNING: RESET_MFA marker found but stock superadmin %q not found — nothing to clear", deps.Config.LocalAuth.Username)
		return nil
	}
	if err := mfaService.Disable(ctx, user.Id); err != nil {
		return fmt.Errorf("clear stock superadmin mfa: %w", err)
	}
	log.Printf("WARNING: RESET_MFA marker consumed — cleared the second factor for stock superadmin %q", deps.Config.LocalAuth.Username)
	if audit != nil {
		// No actor and no client address on purpose: nobody authenticated to cause this.
		// Whoever dropped the marker had filesystem access to the data directory, and that
		// — rather than any account — is what the entry is telling the reader to go look at.
		audit.Record(ctx, services.AuditEntry{
			Action:     services.ActionMfaAdminReset,
			ActorEmail: "system",
			TargetType: "user",
			TargetId:   strconv.FormatInt(user.Id, 10),
			Detail:     "RESET_MFA marker in the data directory cleared this account's second factor on boot",
			Metadata:   map[string]any{"marker": mfaResetMarkerFile, "account": deps.Config.LocalAuth.Username},
		})
	}
	return nil
}

// consumeAdminResetMarker performs the lock-out recovery reset if the marker is
// present, returning the new credential so the caller can announce it. Returns
// (nil, nil) when there is no marker — the normal path.
//
// The marker is deleted BEFORE the reset runs, so a crash mid-reset (or any later
// restart) can never silently re-reset the password behind the operator's back.
func consumeAdminResetMarker(deps apphost.Dependencies, users services.IUserLoginService, superRoleId int64) (*services.StockSeedResult, error) {
	marker := filepath.Join(deps.DataDir, adminResetMarkerFile)
	if _, err := os.Stat(marker); err != nil {
		return nil, nil //nolint:nilnil // absent marker is the normal path
	}
	if err := os.Remove(marker); err != nil {
		return nil, fmt.Errorf("remove reset marker %s: %w", marker, err)
	}
	log.Printf("WARNING: admin reset marker found — resetting the bootstrap superadmin password")
	seed, err := users.ResetStockSuperadmin(context.Background(), deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password, superRoleId)
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

// announceFirstRunAdmin surfaces the bootstrap superadmin credential established
// on a fresh install. It writes a recovery file in the data dir — the reliable,
// platform-independent place to find the login, since a Windows service console
// is invisible and a Docker/journal banner scrolls away — and prints a console
// banner.
//
// The password is echoed only when it was *generated*; a config- or env-supplied
// one is pointed at rather than logged, because the operator already knows it.
// The account is must-change on first sign-in either way. Mirrors myseliasan's
// first-run banner.
func announceFirstRunAdmin(deps apphost.Dependencies, seed services.StockSeedResult) {
	url := firstRunConsoleURL(deps.Config)
	credPath := filepath.Join(deps.DataDir, firstRunCredentialFile)
	saved := writeFirstRunCredentialFile(credPath, url, seed) == nil
	if !saved {
		log.Printf("WARNING: could not write credential recovery file %s", credPath)
	}

	const bar = "======================================================================"
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", bar)
	fmt.Fprint(&b, "  MyIDSan is ready.\n\n")
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
	log.Printf("bootstrap superadmin %q established (must change on next login); sign-in details written to %s", seed.Username, credPath)
}

func firstRunConsoleURL(cfg *config.AppConfigModel) string {
	scheme := "http"
	port := 0
	switch {
	case len(cfg.Server.TLSPorts) > 0:
		scheme = "https"
		port = cfg.Server.TLSPorts[0]
	case len(cfg.Server.NonTLSPorts) > 0:
		port = cfg.Server.NonTLSPorts[0]
	case len(cfg.Server.Ports) > 0:
		port = cfg.Server.Ports[0]
	}
	if port == 0 {
		port = 3001
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, port)
}

// writeFirstRunCredentialFile persists the bootstrap login (0600) so it is
// recoverable after the console banner scrolls away.
func writeFirstRunCredentialFile(path, url string, seed services.StockSeedResult) error {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o750)
	}
	body := "MyIDSan - one-time administrator login\n" +
		"======================================\n\n" +
		"Open:      " + url + "\n" +
		"Username:  " + seed.Username + "\n" +
		"Password:  " + seed.Password + "\n\n" +
		"You will be asked to set your own password on first sign-in.\n" +
		"Delete this file once you have signed in.\n"
	return os.WriteFile(path, []byte(body), 0o600)
}
