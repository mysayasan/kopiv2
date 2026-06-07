package scheduler

import (
	"context"
	"time"
)

type Logger interface {
	Infof(source string, format string, args ...any)
	Warnf(source string, format string, args ...any)
}

type Task struct {
	name     string
	interval time.Duration
	run      func(context.Context) error
	logger   Logger
}

type Scheduler struct {
	ctx    context.Context
	logger Logger
}

// New creates a scheduler bound to the app lifecycle context.
func New(ctx context.Context, logger Logger) *Scheduler {
	return &Scheduler{
		ctx:    ctx,
		logger: logger,
	}
}

// StartPeriodic runs a task immediately, then on each interval until the app context stops.
func (s *Scheduler) StartPeriodic(name string, interval time.Duration, run func(context.Context) error) {
	if s == nil {
		return
	}
	StartPeriodic(s.ctx, name, interval, s.logger, run)
}

// StartPeriodic runs a task immediately, then on each interval until ctx stops.
func StartPeriodic(ctx context.Context, name string, interval time.Duration, logger Logger, run func(context.Context) error) {
	if interval <= 0 {
		interval = time.Hour
	}

	task := Task{
		name:     name,
		interval: interval,
		run:      run,
		logger:   logger,
	}
	go task.loop(ctx)
}

func (t Task) loop(ctx context.Context) {
	t.execute(ctx)

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.execute(ctx)
		}
	}
}

func (t Task) execute(ctx context.Context) {
	if t.run == nil {
		return
	}
	if err := t.run(ctx); err != nil {
		if t.logger != nil {
			t.logger.Warnf("scheduler", "%s failed: %v", t.name, err)
		}
		return
	}
	if t.logger != nil {
		t.logger.Infof("scheduler", "%s completed", t.name)
	}
}
