# Module: infra/vision/external.go

## Purpose

Adapts an external object detector command into the reusable `ObjectDetector` interface.

## Responsibilities

- Start a configured command with optional arguments under a bounded timeout.
- Send the captured JPEG frame bytes to the detector process through stdin.
- Parse stdout as either a direct array of object candidates or an object containing `detections` or `objects`.
- Normalize labels, confidence values, and bounding boxes before returning candidates.

## Notes

- The external detector contract keeps model runtime dependencies outside the Go process.
- Detector stdout should contain normalized boxes shaped as `{"x":0.1,"y":0.2,"w":0.3,"h":0.4}`.
- Detector stdout can also return `{"error":"message"}` to surface a worker-side failure.
- Non-zero command exit status and invalid JSON become detector errors, which the MyMataSan monitor records as throttled diagnostic alert events.
