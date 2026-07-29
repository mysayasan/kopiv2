package apphost

import (
	"log"
	"strings"
)

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
// The loud case is a CONTRADICTION rather than a guess. There is no reliable way to detect
// "am I one of several replicas" from inside one process, and warning unconditionally would
// train operators to ignore the line. But an operator who configured a DISTRIBUTED
// transaction lock has already told us they expect more than one instance — that is the
// only reason to pay for one — so a distributed lock paired with a per-process session
// cache is a self-inconsistent deployment, and saying so is a fact rather than a hunch.
func warnSharedStateBoundary(cacheProvider, lockProvider string) {
	if isSharedCacheProvider(cacheProvider) {
		log.Printf("deployment: cache provider=%s is shared — sessions survive a restart and are visible to every instance, so this app can run behind a load balancer", cacheProvider)
		return
	}

	if isDistributedLockProvider(lockProvider) {
		log.Printf("deployment: WARNING inconsistent configuration — transaction lock provider=%q is distributed (which only makes sense with more than one instance) but cache provider=%q is per-process. "+
			"Sessions are held in the cache, so a second instance will not recognise this one's sessions and users will be signed out whenever the load balancer moves them. "+
			"Point cache.provider at the same Redis to run more than one instance.", lockProvider, cacheProvider)
		return
	}

	log.Printf("deployment: cache provider=%s is per-process — sessions are held in this process only. "+
		"This instance is SINGLE-INSTANCE ONLY: running a second one behind a load balancer signs users out on every switch, and a restart ends every session. "+
		"Set cache.provider to redis to run more than one.", cacheProvider)
}

// isSharedCacheProvider reports whether the cache is visible to other processes. Kept as a
// name check rather than a capability on the store because the provider string is what the
// operator actually wrote in config.json, and that is what the message has to talk about.
func isSharedCacheProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "redis", "rediscluster", "redis-cluster":
		return true
	default:
		return false
	}
}

func isDistributedLockProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "redis", "rediscluster", "redis-cluster":
		return true
	default:
		return false
	}
}
