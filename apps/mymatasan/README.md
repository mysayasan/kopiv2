# mymatasan

`mymatasan` is the camera and video intelligence app for `kopiv2`.

It is designed to discover ONVIF cameras, persist camera records, and expose live viewing through RTSP-backed streams using decoder support such as H.264 and H.265. It also hosts video intelligence workflows such as object detection and crossing-object detection through VLMS-style processing.

## Current Scope

- Camera stream records under `/api/camera/stream`.
- MJPEG viewing endpoint for selected camera stream records.
- Camera worker autostart and graceful shutdown.
- Shared cache, log, file-storage, version, and Swagger APIs from the shared app host.
- SSO issuer/audience validation through the shared auth middleware.
- Resource-app RBAC policy cache keyed by `mymatasan`, role, and policy version.
- User, role, app-registry, endpoint, and endpoint-RBAC management APIs are intentionally not mounted in `mymatasan`; manage them through `myidsan`.
- App-specific OpenAPI descriptions for camera and app-local endpoints.

## Run

From repository root:

```bash
go run . -app mymatasan
```

Default dev listener:

```text
http://localhost:3000
```

## Identity And SSO

`mymatasan` is a relying/resource app for `myidsan` SSO. Its config expects `sso.issuer=myidsan` and `sso.audience=mymatasan`; local login/OAuth routes are not mounted.

With Redis enabled, `mymatasan` can validate cache-backed sessions and app-scoped RBAC policy locally. With memory cache, a future client-side integration can call `myidsan` internal APIs (`/api/sso/introspect` and `/api/sso/authorize`) when the local process cannot see shared session or policy state.
