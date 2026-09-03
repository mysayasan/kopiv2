# Module: infra/apphost/firstboot/server.go

## Purpose

Serves the pre-boot setup wizard's HTTP page and API, and owns everything about WHERE and
HOW SAFELY it listens: the port, the security headers, and the one-time token that guards
it the moment it leaves loopback. See `firstboot.go.md` for why this package exists and
what it writes.

## `Run(ctx, opts) (Result, error)`

Blocks until the operator finishes the wizard or `ctx` is cancelled (Ctrl+C, a service
stop — the caller installs its own `signal.NotifyContext`, since boot has not reached the
point where the app's own shutdown handling exists yet). Reads the current config once,
binds the listener (`listen`), builds a `wizard`, prints the "finish setup here" banner
(`announce`) and opens a browser if `opts.Browser` is set, then serves until:

- the context is cancelled — returns `ctx.Err()`, and nothing has been written (`commit`
  is a single atomic write at the very end, so an interrupted setup never leaves a
  half-written `config.json` behind);
- the HTTP server stops on its own before completion — an error;
- `handleComplete` closes `srv.done` — shutdown drains the in-flight completion response
  (`shutdownGrace`, 5s) before the listener closes, so the browser always receives the
  start URL it is about to follow, then removes `SETUP_URL.txt` and returns the `Result`.

## Listening (`listen`, `defaultSetupAddr`, `allowRemote`, `forceLoopback`)

`defaultSetupAddr` is `127.0.0.1:39530` — loopback-only by default and deliberately so:
this page collects database credentials and the administrator password with no
authentication in front of it (there is no user store yet to authenticate against).
Exposing it beyond loopback is an explicit opt-in (`setup.allowRemote` config field, or
`KOPIV2_SETUP_ALLOW_REMOTE=1`/`0` overriding it) and costs a one-time token
(`newToken`/`tokenOK`) that must accompany every request once the listener is not
loopback (`isLoopbackListener`).

The bind address preference order is `KOPIV2_SETUP_ADDR` env, then the config's
`setup.address`, then `defaultSetupAddr`. If that address is already taken (a second app
first-booting on the same host, or an unrelated service squatting the port — the first
real run of this feature found a stale audio-driver service holding `9080`, the port
originally chosen), `listen` falls back to an ephemeral port on the same host and LOGS why
— an operator who set `setup.address` and then found the wizard somewhere else entirely
has to be told, or the port they configured looks silently ignored. `forceLoopback`
rewrites a wildcard/remote host to `127.0.0.1` whenever `allowRemote` is false, keeping
the port — the loopback default is a security property, not merely a bind preference.

## Content-Security-Policy (`setupCSP`, `guard`)

`script-src 'self'; style-src 'self'` with no inline exception — the page ships CSS/JS as
separate files for exactly that reason, and nothing is fetched off-box (the air-gapped
installs require it). `connect-src` additionally allows loopback origins on any port
(`http(s)://localhost:*`, `http(s)://127.0.0.1:*`) ONLY, because the final pane polls the
real app's own URL — a different origin from the wizard's — to know when it is up and
follow it; a bare `'self'` would silently block that probe. `guard` sets these headers plus
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control:
no-store` on every response, and rejects (`401`) any request missing the one-time token
once the listener is not loopback.

## Routes (`wizard.routes`)

`/assets/*` (embedded `assets` dir), `GET /api/state` (pre-fills the form from
`currentAnswers`; every secret is blanked and paired with a `"<field>PasswordSet"` flag —
a blank secret on submit then means "keep the stored one", the same contract the in-app
Settings editor uses), `POST /api/test/db` / `POST /api/test/cache` (validate then call
`opts.ProbeDB`/`opts.ProbeCache` under a 15s timeout; reported `{"ok": false, "error":
...}` rather than an HTTP error so the form can show it inline), `POST /api/complete`
(`handleComplete`), and `/` (`handlePage`, serves `assets/index.html`).

`handleComplete` validates (`validate.go.md`), then under `wizard.mu` writes the config
exactly once: a second submit (a re-posted form, a second browser tab) is answered with
the SAME start URL rather than rewriting the file the app is already booting from
(`w.completed` guard). `fillDBSecret`/`fillCacheSecret` restore the stored password when
the form submitted a blank one, so an operator who never touches the password field cannot
blank it by accident.

## Console announcement (`announce`, `setupURLFile`)

Prints a banner naming the setup URL, opens it in a browser via `opts.Browser` (reporting
what actually happened, same pattern as `infra/apphost/announce.go.md`'s ready banner),
and — because the console is not always visible (no console at all under the Windows
Service Control Manager, a container's banner scrolls away) — writes the same URL to
`SETUP_URL.txt` in the data dir (`writeSetupURLFile`), removed once setup completes
(`removeSetupURLFile`). Mirrors why each app's first-run credential banner is also written
to a file (`INITIAL_ADMIN_LOGIN.txt`).

## Notes

- `wizard.mu` serializes `handleComplete`'s write so two browser tabs racing on Finish
  cannot interleave two writes of the file.
- `port` — the wizard's own listening port — is passed into `validate.go.md`'s
  `validateWeb` as `reservedPort`, refusing an app port equal to the wizard's own: the app
  told to listen there would fail to bind on the boot that follows, exactly the unbootable
  state this wizard exists to prevent.
- `startURL` is derived from the SUBMITTED answers, not from re-reading the config (which
  has not been reloaded at this point in the process).
