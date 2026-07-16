# Module: apps/myiotsan/apis/kb.go

## Purpose

Registers the shipped in-app knowledge base (`apps/myiotsan/kb`, `kb.go.md`) under `/api/kb` — the
setup guides for the solar/Modbus device profiles, read-only reference content compiled into the
binary so it works with no network access.

## Responsibilities

- `NewKbApi(router)` mounts `GET /kb` (list, via `kb.Articles()` — metadata only, no body) and
  `GET /kb/{slug}` (one article, via `kb.Get(slug)` — body included).
- `getArticle` responds `404` (`controllers.ErrNotFound`) for an unknown slug rather than a bare
  empty body.
- Both handlers wrap their payload in `{"items": ...}` / `{"article": ...}` via
  `controllers.SendResult`, the same envelope every other myiotsan API uses.

## Notes

- Registered on `protected` in `app.go`'s `RegisterAppRoutes` (§11), alongside
  `apis.NewNotificationsApi` — see `app/app.go.md`.
- Granted to viewer AND operator, not just admin (`services/rbac.go.md`) — unlike most of this
  app's admin-gated areas, there is nothing here an unprivileged read could leak or misuse; it is
  the same documentation an admin would otherwise have to go find externally, which an air-gapped
  appliance cannot do anyway.
