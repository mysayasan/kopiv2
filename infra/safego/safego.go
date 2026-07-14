// Package safego runs background goroutines that cannot take the process down.
//
// This is an appliance: it is expected to run unattended for months, and a panic in
// any one background task used to kill everything — recording, the API, the lot. The
// worst case was the vision monitor, which runs one goroutine per camera pushing
// detector payloads through nil-deref-able paths: a single malformed bounding box from
// the Python worker took down the whole NVR.
//
// Two shapes, and the difference matters:
//
//   - Go       — one-shot work. A panic is logged and the goroutine ends.
//   - Supervise — a long-lived loop whose death would silently disable a subsystem.
//     A panic is logged and the loop is RESTARTED with backoff until its context is
//     cancelled. Recovering without restarting is not enough for these: nothing
//     notices the goroutine is gone. The vision monitor, for example, keeps its
//     samplers in a map and only starts one when the map has no entry for a camera —
//     so a sampler that died would leave a live map entry and that camera would
//     silently stop being watched until the process restarted.
package safego

import (
	"context"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

const (
	// initialBackoff is the pause before the first restart after a panic.
	initialBackoff = 1 * time.Second
	// maxBackoff caps the restart pause so a task that panics every time cannot spin,
	// while still retrying often enough to recover from a transient fault.
	maxBackoff = 30 * time.Second
	// healthyRun is how long a supervised task must run without panicking before its
	// backoff is considered paid off and resets. Without this, a task that panics once
	// an hour would creep up to the cap and stay there.
	healthyRun = 2 * time.Minute
)

// LogFunc reports a recovered panic. component is the task name passed to Go/Supervise.
type LogFunc func(component string, format string, args ...any)

func defaultLogger(component string, format string, args ...any) {
	log.Printf("safego "+component+": "+format, args...)
}

// PanicObserver is told about every recovered panic, so a supervised task dying can be
// COUNTED and not merely logged.
//
// This is the gap Tier 1 left open. A supervised loop that panics is caught and restarted —
// which is the whole point — but the only trace is one log line, and nobody reads log lines
// from a healthy-looking appliance. A camera sampler that panics every ninety seconds and is
// dutifully restarted looks perfectly fine from the outside: the process is up, the API answers,
// and the frames quietly do not arrive. A counter makes that visible; an alert on the counter
// makes it findable.
type PanicObserver func(component string)

var (
	logMu   sync.RWMutex
	logger  LogFunc = defaultLogger
	observe PanicObserver
)

// SetPanicObserver routes recovered panics to a metrics recorder. Wire it once at startup,
// alongside SetLogger. Passing nil removes it.
func SetPanicObserver(fn PanicObserver) {
	logMu.Lock()
	observe = fn
	logMu.Unlock()
}

func notifyPanic(component string) {
	logMu.RLock()
	fn := observe
	logMu.RUnlock()
	if fn != nil {
		fn(component)
	}
}

// SetLogger routes recovered panics to the application logger. Wire it once at startup;
// until then panics go to the standard logger, so nothing is ever lost silently.
// Passing nil restores the standard logger.
func SetLogger(fn LogFunc) {
	logMu.Lock()
	if fn == nil {
		logger = defaultLogger
	} else {
		logger = fn
	}
	logMu.Unlock()
}

func logf(component string, format string, args ...any) {
	logMu.RLock()
	fn := logger
	logMu.RUnlock()
	fn(component, format, args...)
}

// Go runs fn in a goroutine, recovering from any panic so it cannot take the process
// down. The goroutine is NOT restarted — use it for one-shot background work where a
// retry would be wrong (a single health probe, a download, a training run).
func Go(name string, fn func()) {
	go func() {
		defer recoverPanic(name)
		fn()
	}()
}

// Supervise runs fn in a goroutine and restarts it, with backoff, if it panics —
// until ctx is cancelled. Use it for long-lived loops (monitors, samplers, ticker-driven
// purges) where the goroutine simply ending would silently disable a subsystem.
//
// fn returning normally is treated as a deliberate stop (a loop exiting on ctx.Done),
// so it is NOT restarted. Only a panic triggers a restart.
func Supervise(ctx context.Context, name string, fn func(context.Context)) {
	go func() {
		backoff := initialBackoff
		for {
			if ctx.Err() != nil {
				return
			}
			startedAt := time.Now()
			if !runGuarded(ctx, name, fn) {
				return // clean return — the task decided it was done
			}
			if ctx.Err() != nil {
				return
			}
			// A task that stayed up for a good while before dying has earned a fresh
			// backoff; only a rapidly re-panicking one gets throttled.
			if time.Since(startedAt) >= healthyRun {
				backoff = initialBackoff
			}
			logf(name, "restarting in %s after panic", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()
}

// runGuarded invokes fn and reports whether it panicked.
func runGuarded(ctx context.Context, name string, fn func(context.Context)) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			logf(name, "PANIC RECOVERED: %v\n%s", r, debug.Stack())
			// COUNT it. A supervised task that panics and restarts is otherwise invisible: the
			// process stays up and the subsystem it drove silently stops.
			notifyPanic(name)
		}
	}()
	fn(ctx)
	return false
}

// recoverPanic is the deferred guard used by Go.
func recoverPanic(name string) {
	if r := recover(); r != nil {
		logf(name, "PANIC RECOVERED: %v\n%s", r, debug.Stack())
		notifyPanic(name)
	}
}
