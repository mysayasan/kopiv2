# Module: infra/control/request.go

## Purpose

Application-level view of the parent→node tunneled HTTP exchange, decoupling callers from the raw `Frame` wire struct.

## Responsibilities

- Define `Request` (Method, Path, Role, Actor, Headers, Body) — the node-local HTTP request a parent operator tunnels to a node.
- Define `Response` (Status, Headers, Body) — the node's reply.
- Convert in both directions: `Request.ToFrame` / `RequestFromFrame` build and extract a `FrameReq`; `Response.ToFrame` / `ResponseFromFrame` build and extract a `FrameRes`, correlated by `id`.

## Notes

- `Path` includes the `/api` prefix and is node-local.
- `Role` is the per-request asserted effective role (`viewer`|`admin`); `Actor` is the parent operator identity, carried so node-side audit attributes writes to a real person.
- This is the Phase-2 command tunnel; the underlying `Frame` already carried these fields (see [frame.go.md](frame.go.md)).
