# Module: domain/utils/middlewares/rate_limit.go

## Purpose

Applies config-driven sliding-window rate limits to `/api` requests by API access tier.

## Strategy

1. Load active `api_endpoint` rows from cache, with read-through fallback to the endpoint service.
2. Match request host/path using the same wildcard-host and segment-boundary behavior as RBAC.
3. Use the longest matching endpoint path so specific public routes can override broader protected bases.
4. Select tier limits from config (`DevOnly`, `AuthOnly`, `Public`).
5. Use the shared cache provider's atomic sliding-window operation to allow or reject the request.

## Notes

- **myseliasan node-fanout exemption**: before tier resolution, `isNodeFanoutPath` exempts three high-volume per-node control-plane path shapes from this generic per-path limiter — `/api/nodes/{id}/proxy[/...]` (the command tunnel that fans a whole node's UI, dashboard/cameras/settings/notifications, through one path), `/api/nodes/{id}/.../webrtc/...` (live-view WebRTC signaling), and `/api/nodes/{id}/recording-stream/...` (range-streamed recording playback, which issues many `Range` requests per clip). Without this, all of a node session's traffic collapses into the single `/api/nodes` bucket and a normal session trips `429`, making the node "go online but can't load data." These surfaces are authenticated + per-node-access-gated at their own handlers, and the node enforces its own limits downstream, so the exemption is safe. Node CRUD/adopt/release/wipe/access (`/api/nodes`, `/api/nodes/{id}`, `/api/nodes/access`) don't match the shape and stay rate-limited.
- Rate limiting runs after API activity logging so `429` responses are still persisted in `api_log`.
- Endpoint tier loading calls the shared endpoint service with empty filters/sorters so it can use the common list service signature.
- `DevOnly` does not bypass authorization. Dev-only routes still require auth/RBAC when mounted behind protected handlers.
- Redis should be used in multi-instance production so counters are shared across app instances.
- Unauthenticated request identity for bucketing is resolved by `(*RateLimitMidware).clientIP`, **not** the shared `clientIPFromRequest` helper: it honors `X-Forwarded-For` / `X-Real-IP` only when the direct TCP peer (`r.RemoteAddr`) matches a configured `RateLimitConfig.TrustedProxies` entry (IP or CIDR, parsed by `parseTrustedProxies`); otherwise it uses the peer address itself. `TrustedProxies` defaults to empty (trust none), so a directly internet-exposed instance cannot have its rate-limit/login-lockout bucket key spoofed by a forged forwarding header — a client behind an untrusted/unset proxy is bucketed by the proxy's own peer address (shared across all its clients), which is the safe fallback. Deployments that terminate TLS at a real reverse proxy must set `rateLimit.trustedProxies` to that proxy's address(es) to get accurate per-client buckets again.
- When `X-Forwarded-For` has multiple comma-separated hops, only the left-most (originating client, as seen by the nearest trusted proxy) is used.
