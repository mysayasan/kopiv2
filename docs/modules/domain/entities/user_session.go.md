# Module: domain/entities/user_session.go

## Purpose

Shared entity for persisted SSO session audit and future revocation workflows.

## Notes

- The current runtime hot path stores and validates live sessions in cache under `sso:session:<sid>`.
- The table is registered during bootstrap so later revocation/audit persistence can be added without introducing the entity then.
