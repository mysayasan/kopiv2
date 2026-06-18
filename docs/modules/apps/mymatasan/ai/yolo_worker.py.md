# Module: apps/mymatasan/ai/yolo_worker.py

## Purpose

Runs Ultralytics YOLO as a persistent MyMataSan detector worker.

Companion script `train_worker.py` is a one-shot trainer used by the in-app
training feature; it runs `ultralytics` `.train()` on an exported dataset and
streams JSON progress to stdout (see the Custom Model Training API).

## Responsibilities

- Load the configured YOLO model once at process startup.
- Read newline-delimited JSON frame requests from stdin.
- Decode base64 JPEG bytes into a temporary image file for YOLO inference.
- Convert YOLO results into normalized object candidates for Go.
- Write one compact JSON response per request to stdout.

## Notes

- Install Python dependencies from `apps/mymatasan/ai/requirements-yolo.txt`.
- Default model is `yolo11n.pt`, which detects COCO classes such as `person`, `car`, `truck`, `bus`, `motorcycle`, `bicycle`, `bird`, `cat`, `dog`, `horse`, `sheep`, `cow`, `elephant`, `bear`, `zebra`, and `giraffe`.
- Fire, smoke, mouse, rat, and other non-COCO labels require a YOLO model trained for those classes; set `MYMATASAN_YOLO_MODEL` to that model path, or train/import and **activate** a custom model from the Training tab.
- **Parallel stock + custom models.** The worker always loads a **stock** model (`MYMATASAN_YOLO_MODEL` env, else bundled `yolo11n.pt`) and, when a custom model is active, **also** loads it from the active-model pointer file (`MYMATASAN_ACTIVE_MODEL_FILE`, default `active_model.txt` next to this script). Both run on every frame and their detections are merged (same-label boxes with IoU > 0.55 de-duplicated; different labels at the same spot are both kept). So activating a custom model **adds** its classes on top of stock rather than replacing it. Activate/deactivate restarts the worker to reload the pair; only one custom model is active at a time. A custom-model load failure does not take down stock detection.
- Optional environment variables: `MYMATASAN_YOLO_CONF`, `MYMATASAN_YOLO_DEVICE`, and `MYMATASAN_YOLO_IMGSZ`.
- CCTV and IR frames often produce useful detections below `0.75`; MyMataSan semantic-rule UI defaults start at `threshold: 0.35` and `minFrames: 2`.
- stdout is reserved for protocol JSON; model logs are redirected to stderr.
