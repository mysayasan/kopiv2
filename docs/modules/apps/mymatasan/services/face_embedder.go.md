# Module: apps/mymatasan/services/face_embedder.go

## Purpose

`pythonFaceEmbedder` is the production `services.FaceEmbedder` (see `face_gallery.go.md`): it runs
`faces_worker.py` once per enrollment image and parses the faces it found. It is the enrollment
counterpart to the **live** face stage inside the persistent detector worker
(`apps/mymatasan/ai/yolo_worker.py`'s `_faces_detect`) — both use the same model files and the shared
`ai/face_model.py`, so an enrolled faceprint and a live faceprint are directly comparable.

Enrollment is occasional and admin-driven, so a one-shot process (load model, embed, exit) is fine
and avoids contending with the live detector — the same tradeoff `anomaly_worker.py` makes for
fitting an anomaly bank.

## Responsibilities

- `NewPythonFaceEmbedder(python, script, yunetPath, sfacePath, logf) FaceEmbedder` — builds the
  embedder even when the model files or script are missing; `Embed` then returns a clear,
  user-facing error at enrollment time ("face recognition is not set up on this host" /
  "face models are not installed — run the face-recognition setup to download them") rather than
  failing at app startup.
- `Model() string` — returns `"opencv-sface-128"`, stamped onto every stored `FaceEmbedding.Model`.
- `Embed(ctx, imageJPEG) ([]DetectedFace, error)` — writes the image to a temp JPEG, runs
  `python faces_worker.py --embed <tmp> --yunet <path> --sface <path>` under a 60s timeout
  (`context.WithTimeout`) with `procutil.HideWindow`, parses the single JSON stdout line
  (`trimToJSON` tolerates stray stderr/noise ahead of the JSON), and maps each reported face
  (`vector`, `box`, `quality`, base64 `thumb`) into a `DetectedFace`.
- `facesWorkerScript(detectorScript)` — resolves `faces_worker.py` next to the given detector script
  path, mirroring the anomaly-worker resolution, so it is found in both the dev tree (`ai/`) and the
  staged tree (`bin/ai/`). Wired from `wire_vision.go`'s `resolveDetectorModelPaths`.

## Notes

- A worker process failure (non-zero exit, unreadable output, or a reported `{"error": ...}`)
  surfaces as an `Enroll` error the admin sees inline — no silent partial enrollment.
- The temp JPEG is always removed (`defer os.Remove`), including on error paths.
- Depends on nothing app-specific beyond the resolved script/model paths, so it could be reused by
  another app that wants the same enrollment flow.
