package notification

import (
	"context"
	"io"
	"sync"
)

// ReloadableChannel is a channel whose set of child channels can be replaced at
// runtime. It lets configuration-driven sinks (webhook, telegram, ...) be
// reconfigured live without recreating the hub or disturbing the always-on
// channels (store, log, SSE). It is safe for concurrent use.
type ReloadableChannel struct {
	name     string
	mu       sync.RWMutex
	children []Channel
}

// NewReloadableChannel creates an empty reloadable channel.
func NewReloadableChannel(name string) *ReloadableChannel {
	return &ReloadableChannel{name: name}
}

func (c *ReloadableChannel) Name() string { return c.name }

// Send fans out to the current set of child channels.
func (c *ReloadableChannel) Send(ctx context.Context, n Notification) error {
	c.mu.RLock()
	children := c.children
	c.mu.RUnlock()
	var firstErr error
	for _, ch := range children {
		if err := ch.Send(ctx, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Set atomically replaces the child channels. Previously-set children that
// implement io.Closer are closed in the background so their internal queues
// drain without blocking the caller.
func (c *ReloadableChannel) Set(children []Channel) {
	c.mu.Lock()
	old := c.children
	c.children = children
	c.mu.Unlock()
	go closeAll(old)
}

// Close closes the current children and clears the set.
func (c *ReloadableChannel) Close() error {
	c.mu.Lock()
	old := c.children
	c.children = nil
	c.mu.Unlock()
	closeAll(old)
	return nil
}

func closeAll(channels []Channel) {
	for _, ch := range channels {
		if closer, ok := ch.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}
