# Module: apps/mymatasan/services/vision_monitor.go

## Purpose

Runs the MyMataSan background vision monitor that samples saved cameras and persists AI alert events.

## Responsibilities

- Poll detection rules on a fixed interval.
- Filter disabled rules, invalid camera IDs, and rules whose `schedulePolicy` is inactive.
- Group active rules by camera to avoid unnecessary frame captures.
- Capture JPEG frames from the saved RTSP URI when available, otherwise from the ONVIF snapshot URI.
- Run the reusable `infra/vision` detector against each captured frame and active camera rule set.
- Persist detector results as alert events.
- Emit throttled diagnostic alert events for capture failures, detector failures, and successful samples with no threshold-crossing detection.

## Notes

- The default interval is two seconds.
- RTSP frame capture uses the runtime decoder settings and TCP transport.
- Snapshot fetches include saved camera credentials when present.
- The current detector is the reusable motion detector MVP; future detector implementations can satisfy `vision.Detector` and be swapped at this boundary.
