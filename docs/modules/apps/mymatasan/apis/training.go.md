# Module: apps/mymatasan/apis/training.go

## Purpose

Registers the custom-model training routes for standalone `mymatasan`: datasets,
labeled images, dataset export, and the model registry (import / train / activate).
All routes use the same local Basic Auth as the other app routes.

## Routes

| Method   | Path                                            | Description |
|----------|-------------------------------------------------|-------------|
| `GET`    | `/api/training/datasets`                        | List training datasets. |
| `POST`   | `/api/training/datasets`                        | Create or update a dataset (`name`, `description`, `classes`). |
| `GET`    | `/api/training/datasets/{id}`                   | Get one dataset. |
| `DELETE` | `/api/training/datasets/{id}`                   | Delete a dataset and its images/files. |
| `GET`    | `/api/training/datasets/{id}/images`            | List a dataset's images. |
| `POST`   | `/api/training/datasets/{id}/images/upload`     | Multipart JPEG upload (`file`). |
| `POST`   | `/api/training/datasets/{id}/images/from-alert` | Import an alert snapshot, pre-labeled from its detection box (`{alertId}`). |
| `GET`    | `/api/training/datasets/{id}/export`            | Download a YOLO dataset zip (`data.yaml` + images/labels train/val split). |
| `DELETE` | `/api/training/images/{id}`                     | Delete an image. |
| `GET`    | `/api/training/images/{id}/file`                | Serve an image (authed). |
| `PUT`    | `/api/training/images/{id}/annotations`         | Save the image's bounding boxes (`{annotations:[{className,x,y,w,h,source}]}`). |
| `POST`   | `/api/training/images/{id}/autolabel`           | Run the active detector on the image and store its boxes as `auto` annotations. |
| `GET`    | `/api/training/capability`                      | Report Python / ultralytics / CUDA availability for in-app training. |
| `GET`    | `/api/training/stock-model`                     | Current base model + selectable options (yolo11n/s/m/l/x). |
| `POST`   | `/api/training/stock-model`                     | Set the base model (`{model}`); known names are downloaded, custom is a local .pt path; reloads the worker. |
| `GET`    | `/api/training/models`                          | List registry models (status, progress, classes, metrics, active flag). |
| `POST`   | `/api/training/models`                          | Start an in-app training run (`{datasetId,epochs,imgsz}`); returns the pending model. |
| `POST`   | `/api/training/models/import`                   | Multipart import of a trained `best.pt` (`file`, `name`, `classes`, `baseModel`). |
| `POST`   | `/api/training/models/{id}/activate`            | Activate a model: hot-swap the live detector and register its classes. |
| `POST`   | `/api/training/models/deactivate`               | Revert to the stock model (clears the active pointer + reloads); lets the active model be deleted. |
| `DELETE` | `/api/training/models/{id}`                     | Delete a model (not while active or running). |

## Notes

- Annotations and exported labels use normalized coordinates `0..1`; annotations
  store top-left `x,y` plus `w,h`, and export converts to YOLO `classIdx cx cy w h`.
- Training runs one at a time as a background job; poll `GET /models` for
  `status` (`pending`/`running`/`completed`/`failed`) and `progress` (0–100).
- Activation writes the model weights path to the active-model pointer file the
  YOLO worker reads on restart (`MYMATASAN_ACTIVE_MODEL_FILE`), then reloads the
  worker. The detector loads one model at a time, so activating a custom model
  replaces the stock one.
- Service: `apps/mymatasan/services/training.go`, `training_export.go`,
  `training_runner.go`. Trainer script: `apps/mymatasan/ai/train_worker.py`.
- Storage root: `vision.training.dataDir` (defaults to a `training` sibling of
  `snapshotDir`).
