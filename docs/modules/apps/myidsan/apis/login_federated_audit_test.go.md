# Module: apps/myidsan/apis/login_federated_audit_test.go

## Purpose

Locks in the fix found by a live bench against a real Keycloak 26: `providerCallback` used to
audit only successful federated sign-ins, so every refused SSO attempt was invisible on the
audit page while a refused password login was recorded.

## Coverage

- `TestFederatedCallbackAuditsARefusedSignIn` — a provider whose `Callback` returns an
  "email is not available"-style error (its detail naming the address, per
  `infra/login/oidc.go.md`'s `email_verified` fix) results in exactly one
  `services.ActionLoginFailure` / `OutcomeDenied` entry whose `Detail` preserves the reason
  and whose `Metadata` names both `provider` and `method` (`services.MethodOIDC` for a
  non-Google/GitHub key).
- `TestFederatedCallbackAuditsAnIdentityWithNoEmail` — an identity with no email (the GitHub
  "no public email" shape) is refused and audited with `ActorEmail` set to the identity's
  `Subject` — the only attribution available when there is no email — and
  `Metadata.method == services.MethodSocial` for the `github` key.
- `TestFederatedMethodClassification` — table-driven over `federatedMethodForKey`:
  `""`/`"google"`/`"github"` classify as `services.MethodSocial`; any other key (a configured
  `login.oidc[].key`, e.g. `"keycloak"`/`"entra"`) classifies as `services.MethodOIDC` — a
  generic OIDC provider must never be mislabelled `social`, since the audit filter offers a
  closed method list an auditor actually queries against.
- `TestFederatedCallbackFailureDoesNotFeedTheLockout` — a real `sharedapis.LoginGuard` armed
  to lock out after a single failure does **not** trip after a refused federated callback, and
  no `services.ActionLoginLockout` entry is recorded — proving
  `recordFederatedLoginFailure` (`apis/login.go.md`) does not route through the
  lockout-advancing `recordLoginFailure` path. Uses a `stubProvider` (a `login.RedirectProvider`
  whose `Callback` always returns a fixed error/identity) and a `recordingAudit` (an
  `IAuditService` that appends to a slice instead of persisting) to exercise the real
  `providerCallback` handler end-to-end through a `mux.Router`.
