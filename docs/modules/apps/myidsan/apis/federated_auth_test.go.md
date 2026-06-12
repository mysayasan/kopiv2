# Module: apps/myidsan/apis/federated_auth_test.go

## Purpose

Validates security helpers for MyIDSan federated auth.

## Coverage

- Confirms the seeded MySeliaSan dev secret matches its stored SHA-256 hash.
- Confirms external login continuation URLs are rejected.
