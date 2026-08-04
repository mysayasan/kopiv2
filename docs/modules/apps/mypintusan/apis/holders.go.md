# Module: apps/mypintusan/apis/holders.go

## Purpose

People and their badges — the operator's daily surface: holder CRUD-lite
(`GET`/`POST /holders`, `GET /holders/{id}`) plus credential issue/revoke
(`GET`/`POST /holders/{id}/credentials`, `POST /holders/{id}/credentials/{credId}/revoke`).

## Responsibilities

- `create` — a new holder starts `HolderActive` but reaches nothing: access comes from access-
  group membership, and creating a holder creates none. That is the fail-closed default — a
  person exists in the system before they are permitted anywhere. `SsoUserId` is deliberately
  **not** taken from the request body: linking a badge to a myidsan identity is a privileged
  operation (`docs/MYPINTUSAN_DATA_MODEL.md` §1), and accepting an id from whoever is creating a
  visitor record would let a badge be bound to somebody else's account.
- `listCredentials` — uses `Get` with an explicit `HolderId` filter, **never** `GetByForeign`:
  that path hardcodes `limit=1` (the suite-wide `GetByForeign` trap — see
  `infra/db/sql/generic_repo.go.md`) and would show an operator only a holder's *first* badge,
  the kind of quiet truncation that makes someone think a card was never issued.
- `issueCredential` — enrols a card or a PIN.
  - PINs are hashed with `services.HashPIN` (`services/alarm.go.md`) inside this handler; the
    plaintext never leaves the function, and `Credential.PinHash`/`DuressPinHash` carry
    `json:"-"` so they cannot travel back out in a response either.
  - A card credential requires `Format` + `CardNumber` — the facility code is part of the match
    key (`services/store_sql.go.md`'s `CredentialByCard`), not decoration.
  - A duress PIN identical to the normal PIN is **rejected**: it would silently disable duress
    (every entry would raise a silent alarm), so the site would turn it off within a day.
- `revokeCredential` — **updates, never deletes.** The access log references the credential id
  by `CredentialId`, and deleting the row would orphan every historical decision that mentions
  it; a revoked badge must still be explicable years later. `lost`/`stolen`/`suspended` are kept
  distinct from a plain revocation — a card presented after being reported stolen is an incident
  worth surfacing on its own, not merely a denial.

## Notes

- No delete route anywhere in this file, deliberately — mirrors `apis/events.go.md`'s read-only
  posture for the access log: both are evidentiary records.
- Live-verified: a holder created, a card credential issued, and revoke exercised end to end
  against a booted app.
