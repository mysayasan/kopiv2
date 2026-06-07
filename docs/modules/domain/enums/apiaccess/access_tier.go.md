# Module: domain/enums/apiaccess/access_tier.go

## Purpose

Defines API endpoint access tiers used by endpoint metadata and rate-limit classification.

## Values

- `DevOnly = 0`: development or operator-only APIs. These routes still require authentication and RBAC when mounted behind protected handlers.
- `AuthOnly = 1`: authenticated application APIs.
- `Public = 2`: anonymous-callable APIs such as login, callbacks, version, health, and public downloads.

## Notes

- The tier does not replace RBAC. It classifies endpoints for cross-cutting controls such as rate limiting.
- Bootstrap seeds protected shared management APIs as `DevOnly`.
