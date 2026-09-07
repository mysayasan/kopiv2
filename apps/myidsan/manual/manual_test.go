package manual_test

import (
	"fmt"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myidsan/apis"
	"github.com/mysayasan/kopiv2/apps/myidsan/manual"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/shared/manual/manualcheck"
	"github.com/mysayasan/kopiv2/infra/config"
)

// TestManual runs the suite-wide conformance checks against myidsan's shipped articles: every
// page has the frontmatter the index and print TOC need, every language folder holds the same
// articles, every cross-link resolves, and every heading anchor a contextual "?" button can point
// at exists identically in all four languages.
func TestManual(t *testing.T) {
	manualcheck.Library(t, manual.Library)
}

// TestManualUIReferences checks the other direction: every article slug and heading anchor that a
// contextual "?" button in the SPA points at must actually exist. Renaming an article or dropping
// a `{#anchor}` breaks nothing at build or run time — the button just opens the wrong page — so
// this is the only thing that catches it.
func TestManualUIReferences(t *testing.T) {
	manualcheck.UIReferences(t, manual.Library, "../views/react-webpack/src/views")
}

// TestManualSpecValues asserts that every literal the manual states as a reference value is the
// value the software actually uses.
//
// These three tables are the ones a reader acts on without checking: an integrator configures a
// relying app against the lifetimes, an operator sets a lockout against the sign-in defaults, and
// an investigator filters the trail by action name. A confidently wrong number costs each of them
// an afternoon, where no number at all would only have cost a support call — which is why the
// rule for this manual is that a value goes in a `spec` row only if it can be asserted here.
//
// The zero-value Effective() calls are deliberate: they resolve exactly the way an install with no
// such block in config.json resolves, which is what the article says it is describing.
func TestManualSpecValues(t *testing.T) {
	login := config.LoginSecurityConfigModel{}.Effective()
	password := config.PasswordPolicyConfigModel{}.Effective()

	manualcheck.SpecValues(t, manual.Library, map[string]string{
		// Signing in for the first time — the shipped sign-in policy.
		"first-sign-in/minlength":  fmt.Sprintf("%d characters", password.MinLength),
		"first-sign-in/attempts":   fmt.Sprintf("%d", login.MaxAttempts),
		"first-sign-in/window":     fmt.Sprintf("%ds", login.WindowSeconds),
		"first-sign-in/lockout":    fmt.Sprintf("%ds", login.LockoutSeconds),
		"first-sign-in/lockoutmax": fmt.Sprintf("%ds", login.LockoutMaxSeconds),
		"first-sign-in/delay":      fmt.Sprintf("%dms", login.FailedDelayMs),

		// Connecting an app — the three lifetimes a relying app is configured against.
		"connecting-an-app/code":    fmt.Sprintf("%ds", apis.DefaultAuthCodeTTLSeconds),
		"connecting-an-app/token":   fmt.Sprintf("%ds", apis.DefaultAccessTokenTTLSeconds),
		"connecting-an-app/session": fmt.Sprintf("%ds", apis.DefaultFederatedSessionTTLSeconds),

		// The audit log — the action vocabulary. Filtering is the whole point of a closed set,
		// so a name the manual prints has to be a name the filter accepts.
		"audit-log/login-failure":   services.ActionLoginFailure,
		"audit-log/login-lockout":   services.ActionLoginLockout,
		"audit-log/mfa-recovery":    services.ActionMfaRecovery,
		"audit-log/mfa-admin-reset": services.ActionMfaAdminReset,
		"audit-log/stepup-failure":  services.ActionStepUpFailure,
		"audit-log/role-change":     services.ActionUserRoleChange,
		"audit-log/sso-refused":     services.ActionSsoRefused,
		"audit-log/backup-export":   services.ActionBackupExport,
		"audit-log/audit-purge":     services.ActionAuditPurge,
	})
}
