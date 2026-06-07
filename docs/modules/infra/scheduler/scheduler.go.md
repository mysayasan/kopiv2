# Module: infra/scheduler/scheduler.go

## Purpose

Provides a small shared periodic task runner for apphost-managed and app-specific background jobs.

## Responsibilities

- Run a task immediately at startup.
- Repeat the task on a configured interval.
- Stop naturally when the supplied context is cancelled during app shutdown.
- Report task success/failure through the injected runtime logger.
- Expose a reusable `Scheduler` instance through apphost dependencies.

## Notes

- Runtime log cleanup uses this scheduler with `logging.cleanup.frequencyMinutes`.
- API log cleanup uses this scheduler with `apiLog.cleanup.frequencyMinutes`.
- App modules can use `deps.Scheduler.StartPeriodic(...)` for reminders, notifications, or other periodic jobs.
- Invalid or missing intervals default to one hour.
