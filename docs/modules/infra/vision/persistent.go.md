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

- Request shape: `{"cameraId":1,"format":"jpeg","image":"<base64>","lpr":true}`. The `lpr` boolean is forwarded from `Frame.WantLPR` and is omitted when false (zero-value omitempty). The worker runs the plate-localization + OCR stage only when `lpr` is `true`.
- Response shape is the same object-candidate contract as `external.go`: either an array or an object with `detections` or `objects`.
- Worker errors can be returned as `{"error":"message"}` and become detector errors.
- MyMataSan uses this for `vision.detector.mode=persistent`, usually with `apps/mymatasan/ai/yolo_worker.py`.
- The worker's **stderr is drained to the app log** through a Go-created pipe, not inherited from `os.Stderr`. When the app runs without a valid console (a relaunched/detached/service process) `os.Stderr` is an invalid handle, and passing it to the child made the Python worker die at stdio init — surfacing as `persistent detector write failed: ... the pipe has been ended` on the very next write (e.g. a capacity "Run Calibration" 500 within ~100ms). The pipe is always valid and the worker's `ready:`/traceback lines now appear in the structured log.
- Spawned with `procutil.HideWindow` so it never pops a console window on Windows (see `infra/procutil`).
