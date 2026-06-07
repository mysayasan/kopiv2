# Module: domain/shared/apis/dto_decode.go

## Purpose

Provides strict request DTO decoding helpers for shared API handlers.

## Responsibilities

- Limit JSON request bodies to the shared API body-size cap.
- Reject unknown JSON fields.
- Decode into shared input DTOs first.
- Project input DTOs into entity models before calling write-oriented service methods.
