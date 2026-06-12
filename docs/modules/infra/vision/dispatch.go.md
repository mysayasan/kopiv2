# Module: infra/vision/dispatch.go

## Purpose

Routes detection rules between object and motion detector implementations.

## Responsibilities

- Split active rules by configured motion-backed detection type.
- Run object-backed rules through a semantic detector implementation.
- Run motion-backed rules through the reusable motion detector.
- Merge detections into one result list for the monitor.

## Notes

- MyMataSan uses this for `hybrid` mode, where semantic rules can use an external detector while `intrusion` can remain motion-based.
- MyMataSan can also use this for `persistent` YOLO mode when `intrusion` should stay motion-based.
- If a detector side is not configured, rules assigned to that side produce no detections rather than failing the whole monitor tick.
- If a routed detector implements `io.Closer`, shutdown closes it through this wrapper.
