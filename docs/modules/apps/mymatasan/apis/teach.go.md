# Module: apps/mymatasan/apis/teach.go

## Purpose

Registers the **Teach wizard** routes for standalone `mymatasan` — the
zero-ML-knowledge camera-teaching surface that drives the training/detection
machinery (`/api/training`, the vision rules, the detector worker) without
exposing datasets, boxes, or model files. All routes use the same local Basic
Auth as the other app routes; writes require an admin local user.

## Routes

| Method   | Path                                        | Description |
|----------|---------------------------------------------|-------------|
| `GET`    | `/api/teach/skills`                         | List taught skills (each carries kind, camera, status, wizard step, config). |
| `POST`   | `/api/teach/skills`                         | Create a draft skill (`name`, `skillType` = `object`\|`inspect`\|`anomaly`, `step`). Rejects a name colliding with a stock detection label. |
| `GET`    | `/api/teach/skills/{id}`                    | Get one skill. |
| `PUT`    | `/api/teach/skills/{id}`                    | Update a skill (name / kind / `cameraId` / `roiPolygon` / step). |
| `DELETE` | `/api/teach/skills/{id}`                    | Forget a skill: deactivate, delete its rule, model, dataset, and (for anomaly) its bank + manifest entry. |
| `GET`    | `/api/teach/skills/{id}/session`            | Poll a teaching session: per-class sample counts + sample-coach hints + live capture state. |
| `POST`   | `/api/teach/skills/{id}/session/start`      | Start a capture session for a class (`{classLabel}`); presence-gated, deduped frames land in the skill's hidden dataset. |
| `POST`   | `/api/teach/skills/{id}/session/stop`       | Stop the running session. |
| `POST`   | `/api/teach/skills/{id}/evaluate`           | Start the accuracy check: quick-train (or fit the anomaly bank) + evaluate on the held-back split. |
| `GET`    | `/api/teach/skills/{id}/evaluation`         | Poll the accuracy check (phase `training`/`evaluating`/`done`/`failed`) + the plain-language report + miss gallery. |
| `POST`   | `/api/teach/skills/{id}/testdrive/start`    | Spin up a second detector worker on the candidate model (env override; the live pipeline is untouched). |
| `POST`   | `/api/teach/skills/{id}/testdrive/stop`     | Stop the test-drive worker. |
| `GET`    | `/api/teach/skills/{id}/testdrive`          | One live annotated frame + detections from the candidate model. |
| `POST`   | `/api/teach/skills/{id}/activate`           | Turn the skill on: hot-swap the model (or register the anomaly manifest entry) and auto-create the detection rule + alert. |
| `POST`   | `/api/teach/skills/{id}/deactivate`         | Turn the skill off: delete its rule, revert detection (samples + model kept). |
| `POST`   | `/api/teach/skills/{id}/export`             | Download a passphrase-encrypted `.mmskill` (`{passphrase, includeImages}`) → `{filename, dataBase64}`. |
| `POST`   | `/api/teach/import/preview`                 | Decrypt only the manifest of an uploaded `.mmskill` (`{dataBase64, passphrase}`) to confirm before import. |
| `POST`   | `/api/teach/import`                          | Import a `.mmskill` as a new, ready-to-activate skill (recreates dataset + model). |
| `GET`    | `/api/teach/skills/{id}/feedback`           | Recent un-reviewed alerts from an active skill, for the "keep teaching" loop. |
| `POST`   | `/api/teach/skills/{id}/feedback`           | File a verdict (`{alertId, verdict}` = `correct`\|`wrong`) back into the dataset; the alert is acknowledged. |
| `GET`    | `/api/teach/active`                         | Cameras with a live teaching session (drives the side-nav "learning" badge). |

## Notes

- Preflight capability (Python / CUDA readiness) reuses `GET /api/training/capability`.
- The auto-created rule sets `detectionType = <class slug>`; it opens in the AI
  Detection editor as **Presence mode** targeting that class and fires because the
  detector matches an unknown detection-type by exact label. Rules created this way
  are badged **"Taught"** in the AI Detection list.
