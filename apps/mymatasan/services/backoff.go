package services

import (
	"context"
	"time"
)

// sleepCtx waits for d, or returns false if the context is cancelled first. It lived alongside
// the control channel until that moved to domain/shared/fleetnode; the media channel (which is
// camera-only and stays here) still needs it.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
