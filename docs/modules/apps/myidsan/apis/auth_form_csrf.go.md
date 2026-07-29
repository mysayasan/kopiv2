# Module: apps/myidsan/apis/auth_form_csrf.go

## Purpose

CSRF protection for the server-rendered auth pages — `GET/POST /api/auth/{login,mfa,forgot,
reset}` (`federated_auth.go`). These routes are public by necessity (they exist precisely for
callers with no session yet), so they never pass through the auth middleware and cannot use
its session-bound double-submit token. Before this file existed they carried **no** CSRF
protection at all, which allowed:

- login CSRF / session fixation — an attacker submits *their own* credentials from a victim's
  browser, silently signing the victim into the attacker's account, so anything the victim
  then does lands in an account the attacker controls;
- a forced password-reset submission on behalf of a victim holding a valid reset token.

## Responsibilities

A session-less double-submit token: rendering a form mints a random value, sets it as a
cookie on the response, and embeds the same value as a hidden field. A cross-site page can
make the browser send the cookie, but same-origin policy stops it reading the cookie value
to forge a matching hidden field.

- `issueAuthFormCSRF(w, r) string` — mints a 32-byte CSPRNG token (base64 URL-encoded), sets
  it as a cookie (`__Host-kopiv2_auth_csrf` over TLS, `kopiv2_auth_csrf` otherwise —
  `__Host-` pins the cookie to this exact origin with no `Domain` attribute so a sibling
  subdomain can't plant a value that would satisfy the comparison), and returns the value to
  embed in the form. `HttpOnly`, `SameSite=Lax`, no `Max-Age` (a session cookie — it only has
  to outlive the form on screen). Minted fresh on **every** render rather than reused, so an
  abandoned form does not leave a long-lived token lying around; a form submitted after a
  re-render still validates, since the cookie is replaced at the same time as the field. On a
  CSPRNG read failure, returns `""` and lets `validateAuthFormCSRF` fail closed rather than
  render a form that looks protected but isn't.
- `authFormCSRFInput(token) string` — renders the hidden `<input>`, HTML-escaping the value
  even though it is our own base64 output — escaping at the point of output is the habit that
  survives someone later changing where the value comes from.
- `validateAuthFormCSRF(r) bool` — compares the submitted field against the cookie in constant
  time (`crypto/subtle`). Fails closed: a missing cookie, a missing field, or an empty token
  is a rejection. Checks the `__Host-` cookie name first, falling back to the bare name (the
  form was possibly rendered over plain HTTP in local dev). Caller must have already parsed
  the form.

## Gotcha — mint before `WriteHeader`

Every call site (`renderLoginPage`, `renderMfaChallenge`, `forgotPage`, `resetPage`) mints the
token and builds the hidden-field string **before** `w.WriteHeader(status)` and before the
`fmt.Fprintf` call that writes the body. `http.SetCookie` only *appends* to the response's
header map; once the status line is written the headers are already flushed to the
connection, so a `http.SetCookie` call made from inside the `Fprintf` argument list (i.e.
evaluated as part of building the format arguments, after `WriteHeader` already ran) is
silently discarded. The rendered form would then carry a token with no matching cookie, and
**every genuine submission would fail** — this exact bug was hit during development and is
why the token is always computed into a local variable first, ahead of the header write.

## Notes

- Wired into `federated_auth.go`'s `loginPage`/`renderLoginPage` (GET/POST `/api/auth/login`),
  `renderMfaChallenge` (`/api/auth/mfa`), `forgotPage`/`forgotPost` (`/api/auth/forgot`), and
  `resetPage`/`resetPost` (`/api/auth/reset`) — see that file's doc for each handler's full
  flow. A failed check on a POST re-renders the originating form with a "that form expired,
  try again" message and a fresh token, rather than a bare error — a stale/back-navigated tab
  is the common legitimate cause.
- Independent of, and in addition to, the existing authenticated-route CSRF cookie
  (`X-CSRF-Token`, see `docs/TECHNICAL_SPEC.md`'s Security Model) — that mechanism requires an
  existing session and does not cover these pre-session routes at all.
- See `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 3 (§3.5).
