# Module: apps/mymatasan/apis/camera.go

## Purpose

Registers camera stream API routes for the `mymatasan` app.

## Responsibilities

- Mount camera routes under `/api/camera/stream`.
- Protect camera routes with JWT auth and RBAC.
- List, create, update, and delete camera stream configuration rows.
- Stream MJPEG frames from the camera stream service with `multipart/x-mixed-replace`.

## Notes

- The MJPEG handler returns when the request context is canceled or the frame channel closes.
- Closed frame channels are treated as terminal for the request to avoid a busy loop.
