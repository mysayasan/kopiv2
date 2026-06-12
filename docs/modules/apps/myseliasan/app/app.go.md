# Module: apps/myseliasan/app/app.go

## Purpose

Implements the `myseliasan` relying control-plane app for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers only lightweight operational entities needed by the local app.
- Disables shared management APIs except the public version endpoint.
- Seeds a local endpoint catalog for rate limiting and runtime metadata.
- Registers relying-app auth/session API routes.
- Registers protected web routes for `/` and `/index.html` before static asset fallback.

## Notes

- MySeliaSan does not register user-management entities.
- Opening `/` without a valid MySeliaSan session redirects to `/api/auth/start`.
- MyIDSan remains the identity provider; MySeliaSan creates its own local session only after code exchange.
