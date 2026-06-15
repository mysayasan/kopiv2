package notification

import (
	"context"
	"log"
)

// logChannel writes each notification to a Logger (or the standard logger when
// none is provided). It is the always-on baseline sink.
type logChannel struct {
	logger Logger
}

// NewLogChannel returns a channel that logs every notification. A nil logger
// falls back to the standard library logger.
func NewLogChannel(logger Logger) Channel {
	return &logChannel{logger: logger}
}

func (c *logChannel) Name() string { return "log" }

func (c *logChannel) Send(_ context.Context, n Notification) error {
	const source = "notification"
	const format = "[%s] %s: %s (category=%s cameraId=%d id=%s)"
	args := []any{n.Severity, n.Title, n.Body, n.Category, n.CameraId, n.ID}
	if c.logger == nil {
		log.Printf(format, args...)
		return nil
	}
	switch n.Severity {
	case Critical:
		c.logger.Errorf(source, format, args...)
	case Warning:
		c.logger.Warnf(source, format, args...)
	default:
		c.logger.Infof(source, format, args...)
	}
	return nil
}
