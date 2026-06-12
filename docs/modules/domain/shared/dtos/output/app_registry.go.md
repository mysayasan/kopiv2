# Module: domain/shared/dtos/output/app_registry.go

## Purpose

Output DTO for app registry responses.

## Notes

- Mirrors public fields from `entities.AppRegistry`.
- Omits `clientSecret` so registry list/read responses do not echo relying-app secrets.
