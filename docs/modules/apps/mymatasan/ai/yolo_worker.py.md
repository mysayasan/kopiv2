# Module: apps/mymatasan/ai/yolo_worker.py

## Purpose

Runs Ultralytics YOLO as a persistent MyMataSan detector worker.

## Responsibilities

- Load the configured YOLO model once at process startup.
- Read newline-delimited JSON frame requests from stdin.
- Decode base64 JPEG bytes into a temporary image file for YOLO inference.
- Convert YOLO results into normalized object candidates for Go.
- Write one compact JSON response per request to stdout.

## Notes

- Install Python dependencies from `apps/mymatasan/ai/requirements-yolo.txt`.
- Default model is `yolo11n.pt`, which detects COCO classes such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`.
- Fire, smoke, mouse, rat, and other non-COCO labels require a YOLO model trained for those classes; set `MYMATASAN_YOLO_MODEL` to that model path.
- Optional environment variables: `MYMATASAN_YOLO_CONF`, `MYMATASAN_YOLO_DEVICE`, and `MYMATASAN_YOLO_IMGSZ`.
- CCTV and IR frames often produce useful detections below `0.75`; MyMataSan semantic-rule UI defaults start at `threshold: 0.35` and `minFrames: 2`.
- stdout is reserved for protocol JSON; model logs are redirected to stderr.
