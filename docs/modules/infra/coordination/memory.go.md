# Module: infra/coordination/memory.go

## Purpose

Implements process-local FIFO transaction locking for development and tests.

## Responsibilities

- Maintains per-resource FIFO wait queues in memory.
- Allows one owner per resource until release.
- Removes canceled or timed-out waiters from the queue.
- Emits acquire, timeout/cancel, and stuck-lock telemetry.

## Notes

- This provider is not safe for multi-instance production because it cannot coordinate across processes.
