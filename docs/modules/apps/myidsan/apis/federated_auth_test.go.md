# Module: apps/myidsan/apis/federated_auth_test.go

## Purpose

Validates security helpers for MyIDSan federated auth.

## Coverage

- Confirms the seeded MySeliaSan dev secret matches its stored SHA-256 hash.
- Confirms external login continuation URLs are rejected.
- `TestCleanContinuePathRejectsBackslashBypass` — a table of backslash-prefixed/embedded values (e.g. `/\evil.example`, `\\evil.example`, `/api/auth\@evil.example`) that browsers normalise to a navigable `//host` form; every one must collapse to `"/"`.
- `TestCleanContinuePathRejectsEmbeddedHost` — values carrying an authority (`//evil.example`, `https://evil.example`, `http://evil.example/x`) must be refused even when not scheme-absolute.
