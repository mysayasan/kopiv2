package apphost

import (
	"context"
	"log"
	"time"

	sharedEntities "github.com/mysayasan/kopiv2/domain/entities"
	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// declaredDeploymentModeTimeout bounds the startup read. The answer only decides the
// wording of one log line, so it must never be able to delay a boot.
const declaredDeploymentModeTimeout = 3 * time.Second

// declaredDeploymentMode reads the operator's declared deployment mode, returning "" when
// there is no answer to read.
//
// Every failure is deliberately silent and returns "". This runs before the schema is
// guaranteed to exist — on a first boot the runtime-setting table may not be there yet —
// and an install that has not been asked the question is exactly the undeclared case the
// caller already handles by inference. Logging a scary error about a missing row on every
// clean first boot would be noise, and noise is what makes the real warning ignorable.
func declaredDeploymentMode(dbCrud dbsql.IDbCrud) string {
	if dbCrud == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), declaredDeploymentModeTimeout)
	defer cancel()

	repo := dbsql.NewGenericRepo[sharedEntities.RuntimeSetting](dbCrud)
	state, err := sharedServices.NewDeploymentModeService(repo).Get(ctx)
	if err != nil {
		return ""
	}
	// Get() reports an absent row as standalone, which is the right default for a caller
	// that wants a usable value but would misreport a never-asked install as an explicit
	// answer here. Only a stored row counts as declared.
	if state.UpdatedAt == 0 {
		return ""
	}
	return state.Mode
}

// warnSharedStateBoundary reports, at startup, whether this process can be run alongside
// another instance of itself.
//
// Sessions live in the cache, and the cache is the authority on whether a session is valid.
// With the in-process memory cache that authority is per-process, so two instances behind a
// load balancer hand out sessions the other one does not recognise: a user is signed out
// every time the balancer moves them. Nothing about that failure looks like a configuration
// problem when it happens — it looks like flaky sign-ins — so the boundary is stated once,
// plainly, at boot, where an operator can find it.
//
// declaredMode is what the operator answered in the setup wizard (see
// sharedServices.DeploymentModeKey), or "" on an install that was never asked. It exists
// because there is no reliable way to detect "am I one of several replicas" from inside one
// process. Without it the only honest signal is a CONTRADICTION — an operator who configured
// a DISTRIBUTED transaction lock has already told us they expect more than one instance, so
// pairing that with a per-process session cache is self-inconsistent and saying so is a fact
// rather than a hunch. With a declared mode we no longer have to infer: "clustered" plus a
// per-process cache is simply wrong, and can be reported as such.
func warnSharedStateBoundary(cacheProvider, lockProvider, declaredMode string) {
	sharedCache := sharedServices.IsSharedCacheProvider(cacheProvider)

	switch declaredMode {
	case sharedServices.ModeClustered:
		if !sharedCache {
			log.Printf("deployment: WARNING this install is DECLARED multi-instance but cache provider=%q is per-process. "+
				"Sessions are held in the cache, so a second instance will not recognise this one's sessions and users will be signed out whenever the load balancer moves them. "+
				"Point cache.provider at the same Redis on every instance.", cacheProvider)
			return
		}
		if !sharedServices.IsDistributedLockProvider(lockProvider) {
			log.Printf("deployment: WARNING this install is DECLARED multi-instance with a shared cache=%s, but transaction lock provider=%q is per-process. "+
				"Scheduled work (retention purges, rollups, digests) is serialized by that lock, so every instance will run it concurrently. "+
				"Point transaction.lockProvider at the same Redis.", cacheProvider, lockProvider)
			return
		}
		log.Printf("deployment: declared multi-instance; cache=%s and transaction lock=%s are both shared, so this instance can run behind a load balancer", cacheProvider, lockProvider)
		return

	case sharedServices.ModeStandalone:
		// An explicit single-instance answer is not a problem to warn about, even on a
		// per-process cache — that is the supported, intended configuration.
		log.Printf("deployment: declared single-instance; cache provider=%s", cacheProvider)
		return
	}

	// Undeclared (pre-wizard installs, and every app during first boot): fall back to
	// inference from the configuration alone.
	if sharedCache {
		log.Printf("deployment: cache provider=%s is shared — sessions survive a restart and are visible to every instance, so this app can run behind a load balancer", cacheProvider)
		return
	}

	if sharedServices.IsDistributedLockProvider(lockProvider) {
		log.Printf("deployment: WARNING inconsistent configuration — transaction lock provider=%q is distributed (which only makes sense with more than one instance) but cache provider=%q is per-process. "+
			"Sessions are held in the cache, so a second instance will not recognise this one's sessions and users will be signed out whenever the load balancer moves them. "+
			"Point cache.provider at the same Redis to run more than one instance.", lockProvider, cacheProvider)
		return
	}

	log.Printf("deployment: cache provider=%s is per-process — sessions are held in this process only. "+
		"This instance is SINGLE-INSTANCE ONLY: running a second one behind a load balancer signs users out on every switch, and a restart ends every session. "+
		"Set cache.provider to redis to run more than one.", cacheProvider)
}

// logSigningSecretFingerprint prints a one-way fingerprint of the JWT signing secret, so an
// operator can confirm that two instances hold the SAME one.
//
// This is the only check available. Every instance must share a signing secret or a token
// minted by one is rejected by the next — which presents as users being randomly signed out
// rather than as a configuration error — and no process can tell from the inside: when none
// is configured the host generates one and writes it back to that instance's own config
// file, so a restart later makes a self-invented secret look exactly like a deliberate one.
//
// Deliberately in the LOG rather than on the deployment checklist. A signing secret only has
// to clear a 16-character floor, so an operator may legitimately have chosen a weak one, and
// publishing a fingerprint of it to every signed-in user would hand them something to attack
// offline for a token-forging key. Logs are already operator-only, and an operator comparing
// two instances is reading both hosts anyway.
//
// Printed only when more than one instance is plausible — a declared cluster, or a shared
// cache or lock. A genuinely standalone install has nothing to compare against, and a line
// that means nothing to most installs is a line everybody learns to skip.
func logSigningSecretFingerprint(secret, cacheProvider, lockProvider, declaredMode string) {
	if declaredMode != sharedServices.ModeClustered &&
		!sharedServices.IsSharedCacheProvider(cacheProvider) &&
		!sharedServices.IsDistributedLockProvider(lockProvider) {
		return
	}
	fingerprint := atrest.FingerprintSecret(secret)
	if fingerprint == "" {
		return
	}
	log.Printf("deployment: signing secret fingerprint=%s — every instance behind the same load balancer must print this same value, or users are signed out whenever they are moved between instances", fingerprint)
}
