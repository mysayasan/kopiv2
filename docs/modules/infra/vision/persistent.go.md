# Module: infra/vision/persistent.go

## Purpose

Adapts a long-lived object detector worker process into the reusable `ObjectDetector` interface.

## Responsibilities

- Start the configured detector command lazily on the first detection request.
- Keep the process alive across sampled frames so heavyweight models such as YOLO load once.
- Send each frame as one newline-delimited JSON request with base64 JPEG bytes.
- Read one newline-delimited JSON response per request and parse it with the shared object-candidate parser.
- Restart the worker after read/write failures or timeouts.
- Close the worker process during app shutdown.

## Notes

- Request shape is `{"cameraId":1,"format":"jpeg","image":"<base64>"}`.
- Response shape is the same object-candidate contract as `external.go`: either an array or an object with `detections` or `objects`.
- Worker errors can be returned as `{"error":"message"}` and become detector errors.
- MyMataSan uses this for `vision.detector.mode=persistent`, usually with `apps/mymatasan/ai/yolo_worker.py`.
