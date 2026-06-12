# Module: domain/shared/apis/dto_decode.go

## Purpose

Provides strict request DTO decoding helpers for shared and app-owned API handlers.

## Responsibilities

- Limit JSON request bodies to the shared API body-size cap.
- Reject unknown JSON fields.
- Decode into caller-selected input DTOs first.
- Project input DTOs into entity models before calling write-oriented service methods.
- Expose the helper as a shared API contract so app modules do not need duplicate decoder implementations.
