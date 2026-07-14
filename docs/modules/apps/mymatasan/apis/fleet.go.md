# Module: apps/mymatasan/apis/fleet.go

## Purpose

Thin, behavior-preserving bindings that keep `mymatasan`'s call sites unchanged after the
pairing API and control dispatcher moved to `domain/shared/apis` as part of the fleet
extraction (see `docs/MYIOTSAN_PLAN.md` §6/P6). Each function here just forwards to the shared
implementation.

## Responsibilities

- `NewPairingPublicApi(router, svc, kick)` — forwards to `sharedapis.NewPairingPublicApi`. Registers the unauthenticated pairing endpoints (adopt, release, self-drop) a control plane calls on the node.
- `NewPairingApi(router, svc)` — forwards to `sharedapis.NewPairingApi`. Registers the authenticated pairing admin endpoints.
- `NewControlDispatcher(router, roles)` — forwards to `sharedapis.NewControlDispatcher`. Builds the node-side handler for tunneled parent→node commands; see `docs/modules/domain/shared/apis/control_dispatch.go.md` for the security property (the node resolves the parent's asserted role against its OWN roles/matrix — the node's data, the node's policy).

## Notes

- Replaces the former standalone `apps/mymatasan/apis/pairing.go` and `control_dispatch.go`, which now live at `domain/shared/apis/{pairing,control_dispatch}.go`. See those docs for the full endpoint/behavior detail — this file adds nothing beyond the mymatasan-typed function signatures.
- `apps/mymatasan/apis/control_dispatch_test.go` still exercises the dispatcher through this package's alias.
