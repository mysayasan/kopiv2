# Module: apps/myidsan/services/metrics.go

## Purpose

myidsan's runtime metrics catalog. Same rule as the other three apps'
`services/metrics.go.md`: **instrument what FAILS SILENTLY.** An identity provider has an
unusual concentration of that kind, because most of its failures are experienced by
somebody else — a relying app, or an operator with no other window into the trail or the
session index.

## Metrics

- `MetricTokenExchangeTotal` = `myidsan_token_exchange_total` ({outcome}) — every
  authorization-code redemption at `POST /api/auth/token`. A non-success outcome is a
  **relying app that just failed to sign a user in**: the app shows its own user its own
  error, and nothing on the myidsan side raises anything an operator would notice, so
  "every login to the payroll app has been broken since its client secret was rotated" can
  run for days without this. Outcomes are a closed set (see below) so the label stays
  low-cardinality and an operator can alert on a specific failure rather than "not
  success" — `code_invalid` covers ordinary races (a user who took too long, a
  double-submitted callback) and is expected at a low rate, while `secret_invalid` and
  `code_mismatch` are not ordinary and mean a misconfigured client or someone probing.
  Incremented directly from `apps/myidsan/apis/federated_auth.go`'s `token` handler via
  `recordTokenExchange` — see that file's doc.
- `MetricAuditWriteFailuresTotal` = `myidsan_audit_write_failures_total` — audit entries
  that could not be persisted. `IAuditService.Record` swallows its write error on purpose
  (auditing must never fail the action being audited — see `services/audit.go.md`), which
  means a trail that has stopped recording has no other symptom: every other signal stays
  green while the security history quietly develops a hole. This is the metric that exists
  because the failure it reports is deliberately silent. Alert on any increase.
- `MetricAuditRetentionPurgedTotal` = `myidsan_audit_retention_purged_total` — rows removed
  by age-based retention (`services/audit_retention.go.md`'s `PurgeOlderThan`). Paired with
  the trail's own `audit.retention_purge` entry so shrinkage is attributable: rows
  disappearing *without* this counter moving did not come from retention.
- `MetricSessionsActive` = `myidsan_sessions_active` — live sessions the session index
  currently marks active. A gauge, not a counter: the useful signals are the level
  (capacity, and confirming a revocation actually took effect) and a sudden jump, not a
  rate. Set by `services.PublishActiveSessions`, polled rather than incremented at call
  sites — see `services/session.go.md`.
- `DescribeMetrics(m telemetry.Metrics)` registers help text for all four; nil-safe; called
  once at startup (`app/app.go.md`).

## Token-exchange outcomes

A closed set of string constants used as the `outcome` label value, so adding one means
adding it here rather than an ad hoc string appearing at a call site and silently
uncapping the label's cardinality:

`TokenExchangeSuccess` (`success`), `TokenExchangeBadRequest` (`bad_request`),
`TokenExchangeBadGrant` (`unsupported_grant`), `TokenExchangeClientUnknown`
(`client_unknown`), `TokenExchangeSecretInvalid` (`secret_invalid`),
`TokenExchangeRedirectBad` (`redirect_invalid`), `TokenExchangeCodeInvalid`
(`code_invalid`), `TokenExchangeCodeMismatch` (`code_mismatch`),
`TokenExchangeServerError` (`server_error`).

## Notes

- Before this file, myidsan had two app metrics
  (`apis.MetricFederatedLoginTotal`/`apis.MetricMfaChallengeTotal`, described in
  `apis/login.go.md`) and was the only one of the four apps absent from the metrics
  catalogue in `docs/HOWTO.md`. Closes `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` §4.4.
- Deliberately **not** added: upstream LDAP/OIDC latency and error rate, and SSO CA expiry
  as a gauge — both need instrumentation inside the directory client and the CA service
  rather than at a handler boundary; see the Phase 4.4 write-up for why they were left for
  later.
- `deps.Metrics` is never nil (apphost falls back to a no-op recorder when telemetry is
  disabled — `infra/apphost/run.go.md`), but every function here nil-guards anyway so the
  package works when constructed directly in tests.
- Covered by `apps/myidsan/services/metrics_test.go` (write-failure and retention counters,
  the session gauge, all nil-safe paths) and, for `CountActive` specifically, by
  `apps/myidsan/services/session_sqlite_test.go` against a real SQLite database — see
  `services/session.go.md` for why a fake repo cannot be trusted for that one.
