# Module: domain/shared/dtos/output/api_endpoint_rbac.go

## Purpose

Defines shared output DTOs for API endpoint RBAC responses.

## Notes

- `ApiEndpointRbacDto` mirrors `entities.ApiEndpointRbac` for write/validate responses.
- `ApiEndpointRbacListDto` is the enriched admin list projection. It keeps `apiEndpointId` and `userRoleId` for edits while adding endpoint title/app/host/path/metadata/tier and role title for display.
- `ApiEndpointRbacJoinDto` mirrors `entities.ApiEndpointRbacJoinModel` for now, including joined endpoint `metadata` so clients can build dynamic menus from the same RBAC access list.
