package notification

import "context"

// Store persists notifications. It is implemented outside this package (by an
// app that owns a database), keeping this engine free of any storage
// dependency.
type Store interface {
	Save(ctx context.Context, n Notification) (int64, error)
}

// storeChannel adapts a Store into a Channel so persistence participates in the
// same fan-out as every other sink.
type storeChannel struct {
	store Store
}

// NewStoreChannel returns a channel that persists each notification via store.
func NewStoreChannel(store Store) Channel {
	return &storeChannel{store: store}
}

func (c *storeChannel) Name() string { return "store" }

func (c *storeChannel) Send(ctx context.Context, n Notification) error {
	_, err := c.store.Save(ctx, n)
	return err
}
