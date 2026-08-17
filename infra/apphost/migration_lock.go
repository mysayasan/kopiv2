package apphost

import (
	"context"
	"log"
	"strings"
	"time"

	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/config"
)

// Schema bootstrap is the one piece of startup that several instances can perform at
// the same instant.
//
// Everything else in a multi-instance deployment settles by itself: two processes
// serving the same read do no harm. Two processes issuing CREATE TABLE, ALTER TABLE
// and the seed INSERTs against one database at the same moment is different — the
// engine may deadlock, one side may fail on an object the other just created, and the
// seeders can double-insert. It is also the likeliest moment for it to happen, because
// an orchestrator starts replicas together.
//
// Holding a lock across it makes the instances take turns: the first migrates, the
// rest wait and then find the schema already current and continue. That is why this
// uses the QUEUEING Lock rather than TryLock — a follower here must not skip
// bootstrap, it must wait for it and then re-check.

// migrationLockResource is the lock name held across schema bootstrap.
const migrationLockResource = "bootstrap-migration"

// migrationLockWait bounds how long an instance waits for another instance's schema
// work. Deliberately far longer than the transaction default: adding a column to a
// large table is slow, and giving up early would put us back to concurrent DDL, which
// is the thing being prevented.
const migrationLockWait = 10 * time.Minute

// withMigrationLock runs fn while holding a deployment-wide bootstrap lock, and
// returns whatever fn returns.
//
// With a per-process lock provider it simply calls fn: a lock that no other process
// can see would be pure overhead, and a standalone install has nothing to race with.
// Any failure to ACQUIRE is logged and fn runs anyway — refusing to boot because a
// coordination store was briefly unreachable would turn an availability feature into
// an outage, and single-instance installs are the overwhelming majority.
func withMigrationLock(ctx context.Context, appName string, appConfig *config.AppConfigModel, fn func() error) error {
	provider := strings.TrimSpace(strings.ToLower(appConfig.Transaction.LockProvider))
	if provider == "" {
		provider = strings.TrimSpace(strings.ToLower(appConfig.Cache.Provider))
	}
	if !sharedServices.IsDistributedLockProvider(provider) {
		return fn()
	}

	// A locker of its own, closed as soon as bootstrap is done. The process-wide one
	// does not exist yet at this point in startup — it needs the telemetry recorder,
	// which needs the router — and boot-time coordination is over in seconds.
	//
	// migrationLockWait is passed as the locker's OWN wait timeout, not merely as a context
	// deadline. Lock() bounds its wait by the locker's WaitTimeout (30s by default), so a
	// context deadline alone was decoration: a migration that took longer than 30 seconds —
	// exactly the case worth serializing — made every other instance give up and proceed
	// WITHOUT the lock, straight into the concurrent DDL this exists to prevent.
	locker, _, err := buildTransactionLocker(appName, appConfig, nil, migrationLockWait)
	if err != nil {
		log.Printf("bootstrap %s: could not build the migration lock (%v); proceeding without it", appName, err)
		return fn()
	}
	defer locker.Close()

	lockCtx, cancel := context.WithTimeout(ctx, migrationLockWait)
	defer cancel()

	lock, err := locker.Lock(lockCtx, migrationLockResource)
	if err != nil {
		// Proceeding is the lesser evil, but it IS the dangerous case, so say so plainly.
		// Refusing to boot because a coordination store was briefly unreachable would turn
		// an availability feature into an outage; running DDL unserialized only risks a
		// problem when another instance is migrating at the same moment.
		log.Printf("bootstrap %s: WARNING could not acquire the migration lock after %s (%v) — proceeding WITHOUT it. "+
			"If another instance is starting at the same time, both may run schema changes concurrently; "+
			"check the schema and this app's logs on every instance before trusting the result.",
			appName, migrationLockWait, err)
		return fn()
	}
	log.Printf("bootstrap %s: holding the migration lock; other instances will wait for schema work to finish", appName)
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		if rerr := lock.Release(releaseCtx); rerr != nil {
			log.Printf("bootstrap %s: releasing the migration lock failed: %v", appName, rerr)
		}
	}()

	return fn()
}
