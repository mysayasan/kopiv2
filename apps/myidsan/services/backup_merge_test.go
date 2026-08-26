package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
)

// Merge mode — "Keep both" in the UI, offered in four languages and described as adding
// the backup's records alongside what is already here.
//
// A live cross-host bench found it could not do that against any real server. Every
// myidsan install seeds the same stock role names and the same bootstrap admin email, and
// both columns are UNIQUE, so a merge restore hit the constraint on its very first row and
// aborted — handing the operator the driver's own text ("UNIQUE constraint failed:
// access_role.name (2067)") in the middle of a disaster recovery, having already written
// whatever came before the collision.
//
// The tests below pin the behaviour that replaced it: a record already present is SKIPPED
// and COUNTED, the target's own copy is left exactly as it was, and nothing that hangs off
// a skipped record gets re-parented onto the copy that is already here.

func mergeInto(t *testing.T, dst *backupHarness, blob []byte, sections ...string) RestoreResult {
	t.Helper()
	res, err := dst.svc.Restore(context.Background(), blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   sections,
		Mode:       RestoreModeMerge,
	})
	if err != nil {
		t.Fatalf("merge restore failed against a server that already holds these records: %v", err)
	}
	return res
}

func exportFrom(t *testing.T, src *backupHarness) []byte {
	t.Helper()
	blob, err := src.svc.Export(context.Background(), BackupRequest{Passphrase: testPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

func TestMergeSkipsRecordsAlreadyPresentInsteadOfAborting(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	// A target that already holds the same role name and the same account email — which is
	// every real server, since both are seeded identically on first run.
	dst := newBackupHarness(t, cipher)
	if _, err := dst.roles.Create(ctx, "", sharedentities.AccessRole{Name: "operators"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{
		Email: "alice@corp.local", Userpwd: "$2a$10$THE-TARGETS-OWN-HASH", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	res := mergeInto(t, dst, blob, BackupSectionAccess, BackupSectionIdentity)

	if len(dst.roles.rows) != 1 {
		t.Errorf("roles = %d, want 1: a merge duplicated a role name that is UNIQUE", len(dst.roles.rows))
	}
	if len(dst.users.rows) != 1 {
		t.Errorf("users = %d, want 1: an identity server with two rows for one person is "+
			"worse than one that declined the import", len(dst.users.rows))
	}
	if res.Skipped[BackupSectionAccess] == 0 || res.Skipped[BackupSectionIdentity] == 0 {
		t.Errorf("skipped = %v: what was declined must be reported, not silently dropped", res.Skipped)
	}
}

// The target's copy wins. A backup is a snapshot of the past; letting it overwrite a live
// account is how a stale file silently rolls somebody's credentials — and their role —
// backwards, which is the one thing "keep both" must never do.
func TestMergeDoesNotOverwriteTheAccountAlreadyThere(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{
		Email: "alice@corp.local", Userpwd: "$2a$10$THE-TARGETS-OWN-HASH", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	mergeInto(t, dst, blob, BackupSectionAccess, BackupSectionIdentity)

	if got := dst.users.rows[0].Userpwd; got != "$2a$10$THE-TARGETS-OWN-HASH" {
		t.Fatalf("password hash = %q: the backup overwrote the credential of a live account", got)
	}
}

// The security-relevant half. When an account is skipped because one with that email is
// already here, everything that hangs off it must skip too — a second factor re-parented
// onto the account that IS here would hand its login gate to whoever holds the backup.
func TestMergeDoesNotReparentASkippedAccountsSecondFactor(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{
		Email: "alice@corp.local", Userpwd: "$2a$10$THE-TARGETS-OWN-HASH", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	res := mergeInto(t, dst, blob, BackupSectionIdentity, BackupSectionMfa)

	if len(dst.mfaFactors.rows) != 0 {
		t.Fatalf("mfa factors = %d, want 0: a factor was attached to an account the backup "+
			"did not restore", len(dst.mfaFactors.rows))
	}
	if res.Skipped[BackupSectionMfa] == 0 {
		t.Errorf("skipped = %v: the orphaned factors must be counted", res.Skipped)
	}
}

// app_registry carries TWO unique constraints — code and audience — so a record can
// collide on either alone, and a guard that only checked one would still abort.
func TestMergeSkipsAnAppThatCollidesOnAudienceAlone(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	// A different code, the SAME audience.
	if _, err := dst.appRegistry.Create(ctx, "", sharedentities.AppRegistry{
		Code: "fleet-console", Audience: "myseliasan", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	res := mergeInto(t, dst, blob, BackupSectionApps)

	if len(dst.appRegistry.rows) != 1 {
		t.Fatalf("apps = %d, want 1: a second registration claimed an audience already in use, "+
			"which is what the tokens are addressed to", len(dst.appRegistry.rows))
	}
	if res.Skipped[BackupSectionApps] == 0 {
		t.Errorf("skipped = %v", res.Skipped)
	}
}

// A skipped app must take its client credentials and redirect URIs with it. A redirect URI
// is the allow-list /api/auth/authorize checks, so attaching the backup's to the
// registration already here would widen where that app's codes may be sent.
func TestMergeDoesNotGraftRedirectUrisOntoAnAppAlreadyRegistered(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	if _, err := dst.appRegistry.Create(ctx, "", sharedentities.AppRegistry{
		Code: "myseliasan", Audience: "myseliasan", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	mergeInto(t, dst, blob, BackupSectionApps)

	if len(dst.appRedirect.rows) != 0 {
		t.Fatalf("redirect URIs = %d, want 0: the backup's allow-list was grafted onto an app "+
			"registration it does not belong to", len(dst.appRedirect.rows))
	}
	if len(dst.appAuth.rows) != 0 {
		t.Fatalf("auth configs = %d, want 0: a second client secret was attached to an app "+
			"that already has one", len(dst.appAuth.rows))
	}
}

// Replace mode must keep behaving as before — it wipes first, so nothing collides and
// everything lands. The guard is built after the wipe precisely so this stays true.
func TestReplaceModeStillRestoresEverything(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{Email: "alice@corp.local"}); err != nil {
		t.Fatal(err)
	}
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase, Mode: RestoreModeReplace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dst.users.rows) != 1 || dst.users.rows[0].Userpwd == "" {
		t.Fatalf("replace mode did not overwrite the target's account: %+v", dst.users.rows)
	}
	if res.Restored[BackupSectionIdentity] == 0 || res.Restored[BackupSectionMfa] == 0 {
		t.Fatalf("replace mode restored nothing: %v", res.Restored)
	}
}

// A backup carrying two records with the same natural key would fail the same way even
// against an empty table, so the guard claims keys as it inserts rather than only reading
// the target once up front.
func TestRestoreToleratesAnArchiveHoldingDuplicateKeys(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	// Two roles differing only in case — the database collates them apart on some engines
	// and together on others, which is exactly the ambiguity not to depend on.
	if _, err := src.roles.Create(ctx, "", sharedentities.AccessRole{Name: "Operators"}); err != nil {
		t.Fatal(err)
	}
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase, Sections: []string{BackupSectionAccess}, Mode: RestoreModeReplace,
	})
	if err != nil {
		t.Fatalf("an archive holding two same-key records aborted the restore: %v", err)
	}
	if len(dst.roles.rows) != 1 {
		t.Fatalf("roles = %d, want 1", len(dst.roles.rows))
	}
	if res.Skipped[BackupSectionAccess] == 0 {
		t.Errorf("the duplicate must be reported as skipped, not silently dropped: %v", res.Skipped)
	}
}

// Restoring a section WITHOUT the section it depends on.
//
// The UI lets an operator tick "mfa" alone and choose "Replace what is here", and that is a
// reasonable thing to ask for: put the second factors from this backup onto the accounts
// already on this server. It used to wipe user_mfa_factor and every recovery code, restore
// nothing — there was no user mapping to place them on, because that map is only filled by
// restoring the identity section — and report SUCCESS with a skipped count. Every account
// on the server silently lost its second factor: a fleet-wide lockout under a required-MFA
// policy, and a security downgrade of every account otherwise.
func TestMfaOnlyReplaceLandsOnTheAccountsAlreadyHere(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	// A live server holding the same account, under a DIFFERENT id than the backup's.
	dst := newBackupHarness(t, cipher)
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{Email: "filler@corp.local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.users.Create(ctx, "", sharedentities.UserLogin{Email: "alice@corp.local", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	aliceID := dst.users.rows[1].Id

	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   []string{BackupSectionMfa},
		Mode:       RestoreModeReplace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dst.mfaFactors.rows) == 0 {
		t.Fatal("every second factor on the server was wiped and none restored — an " +
			"mfa-only restore locked the whole estate out and called it success")
	}
	if got := dst.mfaFactors.rows[0].UserLoginId; got != aliceID {
		t.Fatalf("factor landed on user %d, want %d: matched by id rather than by the email "+
			"the account actually signs in with", got, aliceID)
	}
	if res.Restored[BackupSectionMfa] == 0 {
		t.Errorf("restored = %v, want the factor counted", res.Restored)
	}
}

// The other half of the same shape, one section along: group mappings decide which role a
// directory login lands on, so losing them silently is a privilege change too.
func TestFederationOnlyRestoreMatchesRolesAlreadyHere(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher)
	if _, err := dst.roles.Create(ctx, "", sharedentities.AccessRole{Name: "operators"}); err != nil {
		t.Fatal(err)
	}
	operatorsID := dst.roles.rows[0].Id

	if _, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   []string{BackupSectionFederation},
		Mode:       RestoreModeReplace,
	}); err != nil {
		t.Fatal(err)
	}
	if len(dst.groupMaps.rows) == 0 {
		t.Fatal("the directory group mappings were wiped and none restored")
	}
	if got := dst.groupMaps.rows[0].RoleId; got != operatorsID {
		t.Fatalf("mapping points at role %d, want %d", got, operatorsID)
	}
}

// A child whose parent is in NEITHER the backup's selected sections nor on this host still
// skips. That is the case the skipping was written for and it must not have been widened
// into "attach it to something".
func TestAChildWithNoParentAnywhereStillSkips(t *testing.T) {
	ctx := context.Background()
	cipher := testCipher(t)
	src := newBackupHarness(t, cipher)
	seedSource(t, src, cipher)
	blob := exportFrom(t, src)

	dst := newBackupHarness(t, cipher) // nobody home at all
	res, err := dst.svc.Restore(ctx, blob, RestoreRequest{
		Passphrase: testPassphrase,
		Sections:   []string{BackupSectionMfa},
		Mode:       RestoreModeReplace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dst.mfaFactors.rows) != 0 {
		t.Fatalf("factors = %d, want 0: a factor was attached to an account that does not exist",
			len(dst.mfaFactors.rows))
	}
	if res.Skipped[BackupSectionMfa] == 0 {
		t.Errorf("skipped = %v, want the orphans counted", res.Skipped)
	}
}

// The restore walks table after table with no enclosing transaction, so a failure partway
// leaves everything before it written. That cannot be fixed by this layer — but an
// operator mid-recovery must at least be told it happened, and where. The bare driver
// message told them neither.
func TestRestoreFailureReportsWhatWasAlreadyApplied(t *testing.T) {
	res := &RestoreResult{Restored: map[string]int{
		BackupSectionAccess:   2,
		BackupSectionIdentity: 7,
	}}
	err := restoreFailure(BackupSectionMfa, res, errors.New("UNIQUE constraint failed: x (2067)"))
	msg := err.Error()

	for _, want := range []string{"mfa", "PARTIALLY restored", "access=2", "identity=7", "replace"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure message does not mention %q, so the operator cannot tell "+
				"what state the server is in: %s", want, msg)
		}
	}
	// The driver's own text is still reachable for a bug report, just no longer the whole
	// of what the operator is told.
	if errors.Unwrap(err) == nil || !strings.Contains(msg, "2067") {
		t.Errorf("the underlying cause was dropped: %s", msg)
	}
}
