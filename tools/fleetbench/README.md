# fleetbench — a real fleet, on demand

`fleet_harness.py` stands up the setup several flagship benches share: a containerised
myseliasan control plane and two genuinely adopted mymatasan nodes, with certificates issued
by the real fleet CA and both nodes dialing the real mTLS control channel. No cameras and no
recording — those benches add their own sources (see
`docs/FLAGSHIP_BENCH_CHECKLIST.md`).

`bench_w25_rollout.py` (W2-5, staged version rollout) needs one thing none of the other
benches do: a node whose application directory it can actually WRITE INTO, because a
self-update bench that cannot replace the binary measures the refusal, not the feature. Run
the harness with `KOPIV2_NODE_HOME_RW=1` first — see "Self-update benching" below.

`docs/FLAGSHIP_BENCH_CHECKLIST.md` has said "build it once, reuse it" since W1-1. This is
that, written down: the wiring took about ten minutes the first time and every awkward part
is a trap rather than a difficulty.

## Run it

```
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/myseliasan ./cmd/myseliasan
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/mymatasan  ./cmd/mymatasan
python tools/fleetbench/fleet_harness.py      # stand up cp + node-a + node-b, adopt both
python tools/fleetbench/bench_w22_sla.py      # W2-2: node state history + SLA reporting
python tools/fleetbench/bench_w24_search.py   # W2-4: federated cross-node search
KOPIV2_NODE_HOME_RW=1 python tools/fleetbench/fleet_harness.py  # rerun with writable node homes
python tools/fleetbench/bench_w25_rollout.py  # W2-5: staged version rollout
python tools/fleetbench/bench_w27_email.py    # W2-7: the email channel, both flagships
node   tools/fleetbench/uicheck.js .artifacts/fleetbench objects   # drive a SCREEN
node   tools/fleetbench/uicheck_settings.js  .artifacts/fleetbench en   # myseliasan settings
node   tools/fleetbench/uicheck_settings.js  .artifacts/fleetbench ar   # ...and in RTL
node   tools/fleetbench/uicheck_mail_dest.js .artifacts/fleetbench      # mymatasan, TYPES
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w31_timeline.py
node   tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench en  # mymatasan, PLAYS
node   tools/fleetbench/uicheck_timeline.js .artifacts/fleetbench ar  # ...and in RTL
python tools/fleetbench/bench_w32_embedding.py            # W3-2: does the MODEL discriminate?
python tools/fleetbench/bench_w32_appearance.py           # W3-2: search + federation
node   tools/fleetbench/uicheck_appearance.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_appearance.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w33_cases.py  # W3-3a: cases
node   tools/fleetbench/uicheck_cases.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_cases.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w33b_walls.py       # W3-3b: video walls (no footage needed)
node   tools/fleetbench/uicheck_wall.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_wall.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w34_dwell.py        # W3-4: time-based rules (no footage needed)
node   tools/fleetbench/uicheck_dwell.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_dwell.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w35_ptz.py          # W3-5a: PTZ, against a real ONVIF device
node   tools/fleetbench/uicheck_ptz.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_ptz.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w35b_events.py      # W3-5b: events, inputs and relay outputs
node   tools/fleetbench/uicheck_relay.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_relay.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w36_privacy.py  # W3-6
node   tools/fleetbench/uicheck_privacy.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_privacy.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/bench_w37_failover.py # W3-7
node   tools/fleetbench/uicheck_failover.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_failover.js .artifacts/fleetbench ar
KOPIV2_NODE_IMAGE=debian-ffmpeg-face:bench python tools/fleetbench/bench_w36b_faceredact.py
node   tools/fleetbench/uicheck_faceredact.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_faceredact.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w39_push.py         # W3-9: mobile push, against a REAL push service
node   tools/fleetbench/uicheck_push.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_push.js .artifacts/fleetbench ar
node   tools/fleetbench/uicheck_policy.js .artifacts/fleetbench en   # W2-1: the policy SCREEN
node   tools/fleetbench/uicheck_policy.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w37b_capacity.py     # W3-7b: failover capacity, real estimate
python tools/fleetbench/bench_w33c_case_feed.py   # W3-3c: a feed entry into a case file
node   tools/fleetbench/uicheck_case_feed.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_case_feed.js .artifacts/fleetbench ar
python tools/fleetbench/bench_w33d_fleet_wall.py  # W3-3d: fleet video wall across appliances
node   tools/fleetbench/uicheck_fleet_wall.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_fleet_wall.js .artifacts/fleetbench ar
node   tools/fleetbench/uicheck_bundle.js apps/myidsan/static myidsan   # does the SPA MOUNT?
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/myidsan ./cmd/myidsan
python tools/fleetbench/idsan_harness.py       # myidsan + a real relying app + redis
python tools/fleetbench/bench_idsan_sso.py     # the sign-in every other app depends on
python tools/fleetbench/bench_idsan_mfa.py     # the second factor + step-up (FRESH stand-up)
python tools/fleetbench/idsan_harness.py       # RE-STAND between them (see below)
python tools/fleetbench/bench_idsan_backup.py  # disaster recovery across two hosts
python tools/fleetbench/bench_idsan_lockout.py # the lockout, against a real 2-node CLUSTER
python tools/fleetbench/bench_idsan_session_revoke.py # does a revoke reach the RELYING app?
node   tools/fleetbench/uicheck_idsan_admin.js .artifacts/fleetbench en  # myidsan's FIRST screen check
node   tools/fleetbench/uicheck_idsan_admin.js .artifacts/fleetbench ar  # (also ms, zh)
node   tools/fleetbench/uicheck_idsan_webauthn.js .artifacts/fleetbench  # a REAL FIDO2 ceremony
python tools/fleetbench/bench_myseliasan_lockout.py  # myseliasan's lockout — it had NONE
python tools/fleetbench/pintusan_harness.py     # mypintusan + a REAL OSDP reader on the bus
python tools/fleetbench/bench_pintusan_door.py  # does the door open for the right person?
python tools/fleetbench/bench_pintusan_securechannel.py  # can a reader you cannot trust still open a door?
python tools/fleetbench/bench_pintusan_alarms.py  # which of the six alarms can actually fire?
python tools/fleetbench/bench_pintusan_offline.py  # offline mode and the cache TTL: does it ever expire?
python tools/fleetbench/bench_pintusan_schedules.py  # night shifts, holidays and the site timezone
python tools/fleetbench/bench_pintusan_bus.py    # bus faults, reconnect, lockdown across an outage
python tools/fleetbench/bench_pintusan_screens.py  # THE SCREENS: 4 languages x 3 roles, offers vs matrix
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .artifacts/fleetbench/bin/myiotsan ./cmd/myiotsan
python tools/fleetbench/iotsan_harness.py            # myiotsan + its EMBEDDED broker
python tools/fleetbench/bench_iotsan_actuation.py    # does anything reach a relay off-path?
python tools/fleetbench/bench_iotsan_modbus.py       # does a WRITE reach the wire, and move plant?
python tools/fleetbench/bench_iotsan_commands.py     # does a failed command STAY failed?
python tools/fleetbench/bench_iotsan_flows.py        # can the JS in a flow escape, or starve the rest?
python tools/fleetbench/bench_iotsan_telemetry.py    # the deadband gate + can the rows come back OUT?
python tools/fleetbench/bench_iotsan_discovery.py    # READ-ONLY/LAN-local/bounded/cancellable, against a real Modbus tripwire
python tools/fleetbench/seed_iotsan_screens.py         # populated estate over the real broker + operator/viewer accts
node   tools/fleetbench/uicheck_iotsan.js .artifacts/fleetbench en admin      # myiotsan's FIRST screen check (Part A + auto Part B)
node   tools/fleetbench/uicheck_iotsan.js .artifacts/fleetbench ar admin      # Part A only, RTL (also ms, zh)
node   tools/fleetbench/uicheck_iotsan.js .artifacts/fleetbench en operator   # Part A + auto Part C (screen OFFER vs server ALLOW)
node   tools/fleetbench/uicheck_iotsan.js .artifacts/fleetbench en viewer     # Part A + auto Part C, the more-locked-down role
```

`bench_idsan_lockout.py` is the only myidsan bench that stands up a **cluster**: a Postgres
container plus two myidsan instances sharing that database AND the harness's Redis. It
removes all three in a `finally`. It needs the base harness to have run first (for the
docker network, the bench certificate and the built image), but it does not use the base
instance.

Why a cluster: a failed-login lockout is per-process state, and "Tier A, genuinely
clusterable" is a documented, wizard-declarable deployment for this app. A single-instance
bench cannot see that the lockout stops being one. It also runs with the SHIPPED posture —
generic rate limiter ON, login-security at its defaults — because "under real attack" means
the configuration customers run.

Traps, all of which cost real time:

- **Two different throttles answer 429 here and they mask each other.** The generic
  per-endpoint rate limiter and the lockout both refuse with 429 and Retry-After, so "got a
  429" proves nothing. An early run of this bench passed two throttle checks purely on
  rate-limit refusals while the lockout it was measuring did not exist at all. `is_lockout()`
  tells them apart (the lockout body carries `retryAfterSeconds`; the limiter says "rate
  limit exceeded"), and `status_of()` renders a rate-limit refusal as `"rl"` so it can never
  be mistaken for one in a status list.
- `/api/login/*` shares ONE 30-per-minute bucket, and the bench spends most of it failing to
  sign in — so a later probe of `change-password` (same prefix) is refused by the limiter
  before the lockout can engage. `cooldown_rate_limit()` waits that window out. `/api/step-up`
  and the security-key routes have their own buckets and need no pause.
- **Do not wait out a lockout by signing in correctly.** A success legitimately resets the
  escalation counter, which is exactly what must not happen while measuring escalation.
  `wait_until_unlocked()` probes with a WRONG password instead — a locked request never
  reaches the credential check, so it costs nothing.
- **The source key is shared by every attempt the bench makes**, so each phase inherits the
  last one's failures unless it explicitly resets. `reset_guard()` does that and asserts it
  worked; several checks failed on the first run purely from leftovers.
- **Set one jwt secret across the instances.** Left empty each mints its own and a session
  issued by one is rejected by the other as a bad signature — which is a checklist row for
  real clusters, and which this bench walked into before setting it.
- **Never rebuild the binary while containers are running from it.** The bind-mounted
  executable is overwritten under a running process and every instance dies with exit 139
  and no log line. `docker stop` first, build, then start.

`bench_myseliasan_lockout.py` is the same shape aimed at the OTHER Tier A clusterable app,
and it stands up its own everything — its own network (`selbench`), Postgres, Redis and two
myseliasan instances — so it needs no prior harness run. It builds into
`bin/myseliasan-sel`, a **private binary name**, precisely so it can be rebuilt while the
base `fleet_harness.py` containers are still running off `bin/myseliasan`.

It exists because the myidsan lockout bench turned up something it could not fix: myseliasan
had **no failed-login lockout at all**. So this bench had to be able to fail honestly before
it could pass, and its headline number is the attacker's real budget rather than a boolean:
**13 consecutive guesses served, at ~61ms each, and then the correct password accepted**.

The before/after was measured against **one identical set of checks** rather than against
whichever version the bench happened to have on the day. `KOPIV2_SKIP_BUILD=1` exists for
that: build `bin/myseliasan-sel` from the other commit (a `git worktree` at `main` works
fine), then run the same bench file against it.

There is also a **screen check**, because the API bench cannot see the half a locked-out
operator actually experiences. The server's refusal is one English sentence and this app ships
in four languages, so the countdown is rendered client-side from `retryAfterSeconds`; a wrong
field name or a swallowed envelope degrades silently to "Invalid username or password", which
tells a locked-out user to keep typing while every API assertion still passes.

```
python tools/fleetbench/bench_myseliasan_lockout.py --standup   # cluster only, no attack
node   tools/fleetbench/uicheck_sel_lockout.js .artifacts/fleetbench en
node   tools/fleetbench/uicheck_sel_lockout.js .artifacts/fleetbench ms
python tools/fleetbench/bench_myseliasan_lockout.py --teardown
```

It types REAL keystrokes (`Input.dispatchKeyEvent`, not `value=`) — setting `.value` directly
never fires React's `onChange`, so the component state stays empty and the form submits blank,
failing for a reason that has nothing to do with the lockout.

**Only the `char` event may carry `text`.** Chrome inserts the character for a `keyDown` that
has `text` *and* again for the `char` event, so the obvious three-event sequence types
everything TWICE. The first run of this check filled the username with `aaddmmiinn`, reported
6/6, and had proved the lockout against an account nobody was attacking — a nonexistent
username is also a failed sign-in, and the per-IP key locked out regardless. **Open the PNG**:
the assertion text said nothing, the screenshot said `aaddmmiinn`. The check now reads the
field back after typing and throws if it does not hold what it meant to type, so it can never
again lie about its own input.

Traps specific to it, on top of every one listed for the myidsan lockout bench (which all
apply — two throttles answering 429, the shared source key, the one jwt secret, never
rebuilding a binary under a running container):

- **A bench for a control that does not exist must be written to fail, not to error.** Every
  check here reports what the deployment actually did — "13 guesses served before anything
  stopped us" — rather than raising on the first surprise. That number is the finding.
- **`base_config` disables the rate limiter and this bench must put it back.** The generic
  limiter is the only throttle the unfixed app has, so a bench that turns it off measures a
  server nobody runs and reports a scarier number than the truth.
- **A check that passes on an EMPTY result is not a check.** "The trail never records the
  password attempted" passed against the unfixed app for the worst possible reason: the trail
  had recorded nothing at all. It is now conditioned on the trail being non-empty. "No
  evidence of X" and "evidence of no X" are different checks and only one of them is worth
  running.
- **The attempt that TRIPS a lockout still gets its credential verdict**, because the guard is
  consulted before the credential and the threshold is only crossed by recording the failure.
  So the lockout applies from the *next* request. The account-axis check failed on the first
  fixed run purely from that off-by-one — it made one attempt from a second source address
  and expected a 429. It takes two.
- **Count from a clean slate or do not quote a count.** The first fixed run reported "7
  guesses served" against a threshold of 8, because the baseline phase's one wrong password
  was already on the counter and invisible to the phase that thought it was starting at zero.
- Bring the escalation down between phases with a **successful** sign-in (`reset_guard`): it
  clears the escalation counter as well as the window, and without it the waits compound —
  60s, then 120s, then 240s — and the run stops being worth waiting for.
- myseliasan's local sign-in is `POST /api/auth/local-login` and its change-password is
  `POST /api/auth/change-password` — **not** myidsan's `/api/login/default`. Its password
  minimum is **8** characters, not myidsan's 12.

`bench_idsan_session_revoke.py` runs on the ordinary `idsan_harness.py` stand-up and drives
**two real processes**: it signs a real account in through the whole authorization-code flow,
then asks whether revoking at myidsan reaches the app the user is actually holding open. It
scored **18/27 against main and 27/27 against the fix**.

Traps, each of which cost a wrong answer first:

- **Cookies ignore ports.** myidsan and the relying app are the same host IP on two ports and
  both set a session cookie under the same name, so ONE `requests.Session` driving the flow
  keeps whichever was written last and every later assertion silently measures the wrong app.
  `sign_in_through_sso()` returns TWO jars, captured from the two responses that set them.
  Without that split, "dead at myidsan" and "dead at the relying app" are one confused
  question.
- **`/api/sso/introspect` cannot answer about the relying app's cookie.** It validates a token
  with *myidsan's* key, and the relying app mints its own under its own key — so it replies
  `{"active":false,"reason":"token signature is invalid"}`, which is correct and reads exactly
  like a revoked session. The first run of this bench believed it. Decode the relying app's JWT
  locally for the session id (unverified, purely to know what to ask the SERVER about) and let
  every verdict come from a real request.
- **Use a REAL second account.** An earlier draft ran everything as the stock admin, so
  "revoke every session for this user" also signed the OPERATOR out; the audit read that
  followed came back empty and looked like a missing trail. Create the victim via
  `POST /api/user-credential` with `userRoleId` set — a new account with **no** role lands in
  pending-clearance and cannot complete the flow.
- **Revocation is bounded, not instant, and the bench must say which.** The relying app
  re-asks on `sso.policyCacheTtlSeconds` (30s) rather than per request. Asserting an instant
  fails against correct behaviour; this bench MEASURES the window instead and asserts it is
  within the configured one (30.7s observed). Hiding it behind a `sleep` would have documented
  nothing, and the delay is exactly what an operator revoking access in an emergency needs to
  know.
- The first defect masks the second. Until sessions are indexed, `revoke` reports
  `{"revoked":0}` and every downstream check fails for that reason instead of its own — fix
  the indexing, re-run, and only then read the relying-app result.
`uicheck_idsan_admin.js` is the first screen check myidsan has ever had. Every bench before it
was an API bench, and this suite has now been taught four separate times that a green API run
and a working screen are different claims. It drives all twelve admin sections, then drives real
workflows and confirms the SERVER changed. **21/21 English, 8/8 ms, 8/8 zh, 9/9 ar against the
fix; 19/21 against main.**

Part A runs per language: every section renders with a heading, leaks no untranslated dictionary
key, and every enabled control is **hit-testable at its own centre** (`document.elementFromPoint`
at the control's midpoint — the test that once found a button which rendered perfectly and could
not be pressed, in Arabic only). Part B runs in English only and drives create **and DELETE** on
Roles, the audit CSV export, the Settings sections, and finally the destructive one.

Traps, each of which produced a wrong answer first:

- **The first-run wizard sits in front of every admin screen** until dismissed, in every
  language. Without skipping it the run reports an empty navigation and nothing else.
- **The SPA's writes need the double-submit CSRF header.** A raw in-page `fetch` gets
  `401 csrf token not found`, which reads exactly like "the screen cannot do this" — the first
  run reported a role-create failure that was entirely its own.
- **Anchor the untranslated-key regex to the app's real dictionary prefixes.** A bare
  `word.word` match flags the audit log's own DATA: action names like `login.success` are
  content, not missing translations.
- **A check that passes on an empty result is not a check** (this suite's second time).
  "The deleted role disappears from the screen" passed while the create had failed with that
  CSRF error and the role had never existed. It is now conditioned on the create having worked.
- **Order Part B so the destructive step is LAST.** "End all sessions" pressed on the
  administrator's own row ends the session running the check; every screen after it renders
  empty. The first run read the audit log afterwards, got zero rows, and reported a broken
  screen that was perfectly fine.
- The audit export is `/api/audit/export.csv`, not `/api/audit/export`.
- `Page.javascriptDialogOpening` is an EVENT, not a command — subscribe to it on the socket and
  answer with `Page.handleJavaScriptDialog`, or `window.confirm` blocks the page forever.
- **To bench the UNFIXED bundle**, run `idsan_harness.py` from a `git worktree` at `main` with
  `KOPIV2_BENCH_DIR` pointed at your existing artifacts dir: the harness bind-mounts
  `REPO/apps/myidsan`, so that mounts main's built bundle while reusing your binaries and cert.

`uicheck_idsan_webauthn.js` drives the security keys end to end with a **real WebAuthn
ceremony**. Chrome's DevTools `WebAuthn` domain installs a virtual CTAP2 authenticator — real
key material, real signatures, real client-data hashing — so `navigator.credentials` runs the
genuine ceremony, the server verifies a genuine attestation, and the app's own client code
(`lib/webauthn.js`) is what drives it. Nothing is stubbed. **19/19 against the fix, 18/19
against main.**

It covers enrolment, the audit entry, a key-only account still being CHALLENGED at sign-in
(the MFA-bypass shape the server-rendered login page once had), the key completing that
challenge, rename, and removal — including that removal demands the password, refuses a wrong
one, and is throttled (`401`×8 then `429`), which is PR #206's fix proven live from a browser.

Traps:

- **`localhost`, NOT `127.0.0.1`.** WebAuthn requires the relying-party id to be a registrable
  domain and a bare IP is not one: Chrome refuses with `SecurityError: This is an invalid
  domain.` The first run clicked "Add a security key", saw nothing happen, and reported the
  enrolment as broken. The harness certificate already names `localhost` and myidsan derives
  its RP id from the request host, so no product change is needed — just reach it by name.
- `isUserVerified: true` **and** `automaticPresenceSimulation: true` on the virtual
  authenticator, or the ceremony waits forever for a human to touch a key.
- **A check that passes on an empty result is not a check** (the third time here): "the key is
  gone from the list" passed on the run where nothing had ever been enrolled. It is now
  conditioned on the removal having returned 200.
- The security-key card sits **below the fold** on the Profile page — scroll it into view
  before clicking, or the screenshot shows the top of the page and tells you nothing.
- `WebAuthn.getCredentials` is the independent witness: it asks the AUTHENTICATOR what it
  holds, rather than asking the server whether it thinks a key exists.

`bench_idsan_mfa.py` needs a **freshly stood-up harness**: it enrols a second factor on the
stock superadmin, and its first check is that the account starts without one. Run
`idsan_harness.py` immediately before it. Things it costs time to rediscover:

- **The TOTP replay guard will refuse two codes generated seconds apart** — they are the
  same code, and any step ≤ the last accepted one is rejected. The `Totp` helper waits out
  the 30-second step; `repeat()` hands back the spent code so the replay itself is testable.
  The generator is hand-rolled rather than `pyotp` on purpose: a bench that shares a library
  with the thing under test can pass because both are wrong the same way.
- **Failed second-factor attempts feed the SAME eight-in-five-minutes lockout a wrong
  password does**, and this bench produces them deliberately. `clear_guard()` (one
  successful password POST) resets the counters between batches; without it the bench ends
  up measuring the lockout instead of the check it meant to make.
- The throttle check **deliberately trips the lockout**, and a tripped lockout is evaluated
  before the credential — so the account cannot clear it by signing in correctly.
  `wait_out_lockout()` waits the 60s out rather than measuring it.
- It drops a **`RESET_MFA` marker into `.artifacts/fleetbench/idsan/` and restarts the
  container** to exercise the documented lost-device escape hatch. That asymmetry is the
  control being tested: the hatch must work from the host and be unreachable over HTTP.
- **A refusal has to be tested where only the thing under test can refuse.** A session-less
  client is turned away by the CSRF check first, which proves nothing about authorization,
  so the escape-hatch checks plant a matching CSRF pair and the challenge token as the
  access cookie — leaving the auth check as the only thing left that can say no.

**Re-stand the harness between these two.** `bench_idsan_backup.py` enrols a second factor
on the stock superadmin and leaves it enrolled, so a following `bench_idsan_sso.py` cannot
sign in with a password alone — its whole flow then fails, for a reason that has nothing to
do with SSO. Every myidsan bench assumes a fresh stand-up.

`bench_idsan_backup.py` stands up **its own second myidsan** (`idsan-dr`, port 18453, redis
DB 1) and removes it in a `finally`. That second host is the entire point: TOTP secrets and
the LDAP bind password are sealed with the **host's** at-rest key, which is minted per data
directory, so only a restore onto a genuinely different key proves the unseal-on-export /
re-seal-on-restore design works. The bench asserts the two keys differ before it trusts any
cross-host result — if they ever matched, every claim in it would pass for the wrong reason.

Traps in it, all of which cost real time:

- **The TOTP replay guard rides along in the backup.** A first draft spent a code for step
  N+1, restored, waited 31s and presented step N — correctly refused as a replay, which
  reads *exactly* like the at-rest re-sealing having failed. That is the headline defect
  this bench exists to detect, and it would have been reported as one. Never spend a future
  step; the `Totp` helper only ever hands out a step beyond the last one consumed.
- A restore **invalidates the caller's own session**, so re-authenticate after every one.
- Export and restore are **step-up gated**, and once MFA is enrolled step-up needs a code
  too — which means another fresh time-step each time.
- If a previous run left `idsan-dr` attached, `idsan_harness.py` fails at
  `docker network create`. `docker rm -f idsan-dr` first.

`bench_w37_failover.py` (W3-7, N+1 failover) needs the **ffmpeg node image on BOTH nodes** —
the SPARE is the one that has to record, so patching only the protected node's ffmpeg path
produces a takeover that reports success and writes nothing. It runs for about fourteen
minutes, most of it the real 120s hold-down plus the liveness grace window, because the
feature is ABOUT waiting long enough. It `docker stop`s node-a mid-run and starts it again,
and it downloads a segment the spare wrote and runs ffprobe over it — "a file exists" passes
on a zero-byte file and "the API says enabled" passes on a node with no ffmpeg.

It removes its camera source (`fosrc-one`) in a `finally`, so **seed a recording camera again
before `uicheck_failover.js`** — that check needs node-a to hold one. The screen check creates
its own PLAN through the form, deletes anything a previous run left, and asserts the badge
STATE (`data-fo-ready`) rather than its text, which is what lets it run in four languages and
still assert that the text is not the state token.

`bench_w36b_faceredact.py` (W3-6b, face redaction on export) needs a THIRD node image —
`debian-ffmpeg-face:bench`, which is `debian-ffmpeg:bench` plus python3 and
opencv-python-headless — and `KOPIV2_NODE_PYTHON=python3` when standing the harness up, because
the shipped config names the interpreter by its HOST path and no container has it. It also needs
the YuNet model at `apps/mymatasan/ai/face_detection_yunet_2023mar.onnx`, where a real install
puts it.

**It films a DRAWN face, and YuNet detects it.** That is what lets a fleet with nothing to film
bench a detector end to end: a real detection at a position the bench knows, so the output can be
MEASURED (face region black, background not) rather than admired. The face is stationary and the
background moves — see the checklist entry for why both halves matter.

`uicheck_faceredact.js` drives the export dialog and runs a real face-redacted export from the
browser, so it is slow (a face pass reads every frame). Run the API bench first — the screen
check needs a camera with footage on node-a.

## myiotsan: the actuation chokepoint

`iotsan_harness.py` stands up myiotsan the way a site runs it — no redis, no postgres, sqlite and
the in-process cache, the shipped defaults — and, crucially, **publishes its embedded MQTT broker**
so a real device can sit on the wire. `bench_iotsan_actuation.py` then drives every path in the app
that can move a physical device.

Why this shape. myiotsan's whole safety story is one function, `CommandService.Issue`, and six
ordered gates behind it (read-only by default, admin only, only-what-the-profile-declares,
server-side bounds, a per-device duty cycle, an audit row for every attempt **including every
refusal**). A chokepoint is a claim about the whole program, and no unit test can check it: each
caller's own test can pass while a caller nobody wrote a test for reaches the wire another way. So
the bench enumerates them — the direct API, a scene, a schedule (test-fired **and** fired by the
clock), a flow's command node, a flow's `mqtt_out` node, and the rule engine — and drives each for
real.

What makes the assertions mean anything: a `paho` client authenticated as the **real provisioned
device**, confined by the **real broker ACL**, sits on that device's command topic for the whole
run. "Refused" means the API said no AND nothing arrived on that wire. Every negative assertion is
conditioned on a positive that ran first on the same wire.

It found one, and it is the one that mattered: the flow canvas's `mqtt_out` node publishes through
`broker.Publish` — the server's OWN handle, subject to no ACL. Aimed at a device's command topic it
moved a relay whose actuation was switched **off**, with a value outside the declared safe range,
past the duty-cycle limit, and left nothing in the trail. Four gates bypassed at once, by the one
node in the palette whose stated job is bridging a value out. 41/45 against unfixed main, 45/45
against the fix.

Facts worth not rediscovering:

- The bootstrap admin is **must-change**; everything but `/api/auth/session` and
  `/api/auth/change-password` 403s until it is rotated. The session probe is
  `GET /api/auth/session` — not myseliasan's `/api/auth/local-login`, not myidsan's
  `/api/login/default`.
- `mqtt.addr` must be `0.0.0.0:1883` **and** the port published, or a device on the host connects
  to nothing and every wire assertion silently passes on an empty result — which is
  indistinguishable from the gate having refused.
- The broker ACL confines a client to topics **containing its own key**, so a bench device may
  subscribe to `iot/cmd/<key>/#` and to a bridge topic under its own name, and to nothing else.
- `UpdateDeviceRequest` REPLACES rather than patches and rejects unknown fields (there is no
  `protocol` on it): resend `profileId` or the device silently loses its type and every later
  command is refused for the wrong reason.
- The device's broker password is returned **once**, at provisioning. A re-run against a surviving
  instance rotates it (`POST /api/devices/{id}/password`) rather than trying to read it back.
- The six shipped **builtin** solar flows cannot be deleted by design. Counting them as leftovers
  makes a cleanup check fail for a reason that has nothing to do with the bench.
- The scheduler ticks on the minute, so the clock-fired check costs up to two minutes. It is worth
  it: that is the path with no user behind it, and the one where a trail can quietly read "System".
- Isolate before asserting an absence. The bench's own flows are bound to the device's telemetry
  key, so a published reading drives them too — their output is indistinguishable from a rule
  actuating. They are deleted before the rule check runs.


## myiotsan: the Modbus write

`bench_iotsan_modbus.py` starts `tools/sunspec-sim` itself and drives myiotsan's guarded
holding-register write against it — a solar inverter that answers, produces, and can be curtailed.

Why it does not trust the app. `Issue` -> `sendModbus` -> `modbus.WriteConfirm` confirms a write by
**reading back the register it just wrote**. That is the right design, and it is also
self-certifying: a write that went to the wrong register, the wrong unit, or with the wrong sign
confirms itself exactly as happily as a correct one. So the bench carries its own Modbus TCP client
(`iotsan_harness.Modbus`, stdlib sockets) and reads the simulator directly. And because a register
is not the point either — a curtailment that lands in the right register and changes nothing is a
confirmed command that did nothing — every write is checked three ways: the app's status, the raw
word on the wire, and what the plant does next, read back through the product's own telemetry.

It found one, in the configuration path rather than the write path: a **SunSpec profile that
declares no telemetry keys** was polled happily and stored nothing at all, silently. Both Modbus
modes now refuse a profile that could never store a reading. 29/30 against unfixed main, 30/30 with
the fix.

Facts worth not rediscovering:

- **`-tod` is why this bench works at all.** The simulator starts at 06:00, where the inverter
  produces almost nothing, so every curtailment assertion would compare zero against zero. The
  bench starts it at midday, at REALTIME speed — the simulator's own compressed day moves the
  physics too far between two reads for a before/after comparison to mean anything.
- **`inv_ac_power` is not the curtailment signal you want.** It is what the inverter puts on the AC
  bus AFTER the battery takes its share, so at midday a 10 kW plant charging at 5 kW reads ~4.7 kW
  and a 30% cap has little to bite on. `inv_operating_state` (5 = THROTTLED) is the direct signal
  and does not move with how the plant splits production.
- **A reading's numeric value is `num`, not `value`** (`entities.DeviceReading`). Reading the wrong
  field yields `None`, which `or 0` then turns into a confident 0 W — and every "the plant
  responded" comparison silently becomes zero-against-zero. This bench only caught it because each
  check requires the BEFORE value to be high before believing the after.
- **`GET /api/devices/{id}/latest` answers a MAP** keyed by telemetry key, not the `{items:[...]}`
  envelope the rest of the app uses. Reaching for `items` gives `{}`, which reads as "the app is
  not polling" — the wrong conclusion, and it cost a run.
- **A refused or failed command is NOT echoed in the response body** — the handler answers with the
  reason alone. Its status has to be read from the recorded row in the command history.
- A SunSpec model's address depends on the chain in front of it, so the bench WALKS the chain to
  find models 123/124 rather than doing arithmetic, and asserts each model's length before binding
  a command to an offset inside it — a changed model shape must fail the bench, not write a
  curtailment into whatever now lives at that address.
- A stale simulator holds the port, so the next one dies on bind while the port still answers, and
  the bench then measures the OLD binary with none of the flags it just asked for. `start_sim`
  refuses to return until the process IT launched is both alive and serving.


## myiotsan — the life of a command

`bench_iotsan_commands.py`, **34/34** (27/32 against unfixed main on the identical file — two of
the 34 only become reachable once an interrupted command actually ends). It takes the app at its word on the one
design decision it argues for hardest: **an actuation is never automatically retried**, because
re-sending a relay write is a second physical action and a retry whose first attempt landed opens
the door twice.

That promise has two halves, and the second is the dangerous one. *Nothing retries* is easy to
believe. *Nothing is silently lost* is not — and it is what makes the first half safe: refusing to
retry is only defensible because a HUMAN is handed the decision, so a command that reaches no
human is a silent drop rather than a safe refusal. A command that ends up neither confirmed nor
failed is worse than a retry: the operator is never told, the metric never counts it, and the row
sits there claiming to be in flight.

It found two, and both are of that second kind.

**One report confirmed three commands.** Confirmation matched a report against the commands
waiting on a device by comparing the reported NUMBER alone. A building controller routinely has
several commands outstanding that share a number — a lock and a fan are both switches, so both are
`1`. Reporting the lock's state marked the fan `confirmed`, the strongest claim this system makes
about a physical action, and the fan's command then never became the failure the operator would
have been shown. One report invented an actuation and lost a failure in the same stroke. It even
confirmed a command whose profile declares NO confirm key — the case the code documents as an
honest "sent, never confirmed". Confirmation now requires the report to be on the key the
profile declares for that command (`confirmsKey`, unit-pinned).

**A command interrupted mid-write stayed `pending` forever.** The row is written down BEFORE the
send, deliberately — an actuation that was sent but never recorded is the worst ordering. But
`SweepUnconfirmed` only ever looked at `sent`, so a process that stopped in between left a row no
timeout applied to: never counted, never notified, and rendered by the UI as still in flight, a
spinner that never resolves. The sweep now ends `pending` too, timed from `RequestedAt` (a pending
row has no `SentAt` — that is exactly what it never got), with a reason that is honest about the
difference: it may or may not have reached the device.

Everything else held, and is now proven rather than assumed: an unconfirmed command becomes
`failed` on the timeout and NOTHING is re-sent across the whole window; the failure reaches the
operator's notification feed; a LATE report does not resurrect it; a restart with a command in
flight ends that command and re-sends nothing; and a wedged Modbus device is dialled exactly once
per attempt, with no re-dial after a restart.

How the two hard cases are driven:

- **A wedged device is a socket that accepts and never answers** (`Blackhole` in the bench). That
  stalls a Modbus write inside its 3-second per-operation timeout, which is the window a
  `docker kill` has to land in — and it COUNTS dials, which is how "nothing re-drove it" is
  measured on the polled transport, the Modbus equivalent of watching the MQTT wire.
- **The Modbus bench profile declares no telemetry keys on purpose.** The poller refuses such a
  profile out loud (the fix from the Modbus bench), so nothing but the command path ever dials the
  endpoint and the dial count means what it says. A command binds an absolute register and needs
  no read map.
- The device that CONFIRMS is the same `paho` client that watches the wire: it reports state back
  over the real broker, through the real ingest path, so a confirmation is the product's own loop
  closing rather than an API poke.
- Every negative is preceded by the positive that proves the mechanism works — the confirm path is
  demonstrated end to end before any "it did not confirm" is believed.


## myiotsan — arbitrary JavaScript inside the app that writes to the building

`bench_iotsan_flows.py`, **41/41** (32/43 against unfixed main — the count differs because two of
the old checks asked what a broken flow LOOKS like once stored, and a broken flow can no longer be
stored). The flow canvas runs user-authored JavaScript on every reading, on purpose: the fork was
chosen deliberately for Node-RED parity. `flow_eval.go` states the three-line safety model that
makes that acceptable — no host bindings in the sandbox, a watchdog on every call, and no way to
actuate except an output node routed through `CommandService.Issue`. This bench asks all three of
a running app.

**The sandbox holds, completely, and that is worth saying plainly.** A script's whole global object
is the two names the flow bootstrap puts there (`flow`, `__flowctx`); `require`, `process`, `fs`,
`net`, `os` and `child_process` are all absent; the Function-constructor escape lands in the same
empty global; a runaway loop and unbounded recursion are both interrupted inside one script budget
without touching the process; a throwing node drops its own branch and not its sibling's; and one
flow cannot read another's scratchpad. Nothing was found there. The bench also closed a gap the
engine's own verification left open: it was never proven on REAL broker telemetry, because the
broker was off when it was written. It is now.

What it found was on the other side — not what a script can REACH, but what it can COST, and what
happens to a flow that cannot run at all.

**A flow whose script would not compile saved, enabled, listed as enabled, and never ran.** The
graph's shape was validated at save; its SCRIPTS were not. A typo in a function node produced a
`200 OK`, an enabled row, an On badge on the canvas, and then one INFO log line per reconcile on a
worker nobody is watching. An operator who drew an alerting flow believed it was armed. Scripts are
now compiled at save and the typo is refused where the person who made it is still looking at it;
a flow that still cannot compile at runtime (an import, an older row) is reported to the
notification feed once and rendered on the list as **Not running** rather than On.

**One flow's slow scripts stalled every other flow in the install.** Every flow shares one worker
goroutine. `flowScriptTimeout` fences a single script and nothing fenced the EVENT, so a graph of
scripts that each finish just inside their budget — nothing misbehaving by any rule the runtime
could see — spent as long as it liked per reading. Measured: **one reading into a 190-node graph of
80ms scripts delayed an unrelated flow by 15.1 seconds**, and the ceiling the runtime actually
permitted (`flowMaxSteps` × `flowScriptTimeout`) is a hundred seconds per sample. A second shape,
a script that never returns, cost the same worker a full budget on every message forever. Both are
now bounded: one reading gets `flowEventBudget` across the whole graph, and a flow whose script
times out five times in a row is quarantined and the operator is told which flow to look at. The
same two measurements after: **0.8 s** and **0.8 s**.

**Events the runtime shed were invisible.** `FlowRuntime` counted the telemetry events it dropped
when its queue overflowed into a field whose comment said "for logging" and which nothing read.
That is the exact failure `services/metrics.go` says an instrument exists for: when the flow worker
falls behind, the readings still land and the charts still draw, and only the automation quietly
stops. There is now `GET /api/flows/stats`, a ramped log line, and Prometheus gauges for shed
events, queue depth and stopped flows. (The counter also raced — it was a plain `int64` written
from the broker hook and the Modbus poller — and is now atomic.)

**And one that is not myiotsan's at all.** Chasing why an install's 110 enabled flows produced 100
derived series led to `infra/db/sql`: all three drivers stopped scanning a result set at a
**hardcoded 100 rows, whatever limit the caller asked for**, while `x_rows_cnt` still reported the
true total from the database — so a truncated read was indistinguishable from a complete one.
Seventy call sites across the suite ask for more than a hundred rows. Live, on this app alone: a
telemetry series asked for 2000 points and drew 100; `Latest` folded the newest 500 readings into
"the current value of every key" and saw 100, so a busy key crowded the others off the device page;
and the flow runtime asked for its 500 enabled flows and compiled 100 — the hundred-and-first flow
was listed nowhere and never ran, with no error at any layer. The scan now takes exactly what the
caller asked for; only a caller that passes no limit at all gets a ceiling. Pinned by a real-sqlite
test that writes 250 rows and reads them back.

How the starvation half is driven, because the measurement is the whole argument:

- **A WITNESS flow is the clock.** A trivial flow on its own telemetry key, timed from publish to
  stored derived value. Its baseline with nothing else running (0.3 s) is measured first, so a slow
  docker host cannot be mistaken for starvation.
- **The slow graph uses scripts that each stay INSIDE the budget.** That is the point: a graph
  built only of well-behaved nodes was the case no rule in the runtime could see.
- The burst size is 200 readings on purpose. At 100 the unfixed app landed at 9.7 s against a 10 s
  budget, and a knife edge is not a measurement.


## myiotsan — the deadband gate and telemetry read-back

`bench_iotsan_telemetry.py`, **50/50** (33/44 against unfixed main). `entities/telemetry_key.go`
states the claim this bench exists to test: "THE DEADBAND IS THE STORAGE DESIGN." `deadband_test.go`
already pins `Admit()` as a pure function, and it passes — what a unit test cannot see is
everything around it: whether an edited deadband reaches the hot path without a restart, whether a
deleted device's baseline is dropped, whether liveness really bypasses the gate, and whether the
rows the gate was careful to keep can actually be READ BACK. A store that admits the right samples
and then cannot show them is the same outage as a store that dropped them.

**The gate itself held completely.** First-sample admission, the `>=` boundary, a baseline that
does not creep across a slow sub-deadband drift, heartbeat-before/after, a deadband edit taking
effect with no restart, and the property `services/ingest.go` calls "the worst possible bug this
app could contain" — a suppressed sample still reaching the rule engine — all measured, all
correct, against a real broker and a real device. What broke was the read-back half.

**A chatty key crowded every other key off the device page.** `Latest` folded a device's newest
500 rows in memory, on the reasoning that "the tail is small because the deadband keeps it small"
— true per key, not for a device as a whole. Publishing 520 rows on one key of a seven-key device
left the "current value of every key" panel showing exactly one key. `Latest` now takes the
profile's declared key list and does one indexed seek per key instead of sharing a tail.

**A wide chart drew the wrong end of the window.** `/readings` asked the database for the first
`maxPoints` rows ascending, which is the OLDEST slice of a window over the cap — a chart of a busy
key over a wide range drew the start of the range and stopped, indistinguishable from a device
that died. It now reads newest-first and reverses, so the cap discards the old end.

**The rollup table had zero readers.** `Series`'s own comment had always promised that over the
cap "it reads the ROLLUPS instead, which is what they exist for." Nothing called `Rollups`.
Confirmed live: a 24-hour window on a busy key now comes back `span=1m`, reaching both ends of the
range — the recent end via a raw top-up of what the worker has not folded yet, the old end via the
buckets themselves — where before it silently returned the same truncated raw slice regardless of
how wide the window claimed to be. `/readings` now answers `{items, span, truncated}` instead of a
bare list, so a chart drawing hourly summaries has to say so.

**A rollup bucket could be permanently wrong, and the worker had never once run to find out.**
`rollupOnce`'s cutoff was `now - width`, not a bucket boundary — at 10:01:30 that cuts mid-bucket,
writes the 10:00 bucket from half its data, and advances the cursor past the readings from
10:00:30 to 10:00:59 with nothing left to fold them. The shipped interval was a hardcoded,
unsettable hour, so on every bench this app has had, the rollup worker had never actually run. A
new `telemetry_store.rollupIntervalMs` (default unchanged at 1h; the harness sets it to 5s) let
this bench watch its arithmetic directly against the raw table — no API exposes a bucket — and
confirm every 1-minute bucket's count/min/max matches the raw rows underneath it, no bucket is
written twice, and the still-filling current bucket is never rolled up early.

**The deadband gate never forgot a deleted device.** `DeadbandGate.Forget` shipped with a comment
naming exactly why it exists and had zero production callers, only its own unit test — the gate
map is the one structure `services/ingest.go` calls unbounded. This bench deletes a device,
creates a replacement (SQLite reissues the freed rowid, so the replacement can land on the same
id), and checks the new device's first reading is stored rather than compared against the deleted
device's last value, then checks `myiotsan_ingest_series` actually falls after the delete.

**`written` was the sum of insert ids.** `ReadingWriter` added `CreateMultiple`'s return value —
the driver's last insert id — into a running total instead of counting rows, so the number an
operator compares against `stored` to see whether the batcher is keeping up grew with the size of
the table: 2,666 readings stored, 3,555,111 reported written. Checked directly against a row count
of the SQLite file itself, not against the app's own claim about itself.

`db()` (new in `iotsan_harness.py`) is why the rollup and `written` checks are real: it copies the
app's live SQLite file (WAL/shm included, to avoid a torn read against a database the app holds
open) into a scratch directory and queries it directly. Asking the app whether it stored something
correctly is asking the accused to testify; this is the bench's own eyes on the table.

## myiotsan — the first screen check

Every bench of this app before now was an API bench. `uicheck_iotsan.js` is the first to drive its
eleven screens in headless Chrome, seeded over the real MQTT broker (`seed_iotsan_screens.py` — a
browser cannot speak MQTT) with a populated estate plus a real operator and a real viewer account,
because a screen check run against an estate that silently failed to seed reports empty screens as
broken ones. **PART A** sweeps all eleven nav sections per language and per role: renders, has a
heading, leaks no dictionary key, fits its viewport, nav rail on screen, every enabled control
hit-testable at its own centre. **PART B** (`uicheck_iotsan_partb.js`, auto-run for
admin+English) drives real workflows — including actuation, the thing this app exists to do, and
delete, last, because it removes what everything above needs — with real mouse and keyboard events
and confirms the SERVER changed, not just the screen. **PART C** (`uicheck_iotsan_partc.js`,
auto-run for operator/viewer) checks that what the screen OFFERS matches what the server ALLOWS,
in both directions: a menu entry the server refuses is a dead end with no way to tell "not
allowed" from "broken"; a capability the server allows and the nav hides is invisible to everyone
including whoever granted the role. **114/114 with the fixes; 93/106 against the unfixed app**
(locked-out roles attempt fewer checks because the device-stage checks are gated on being able to
open a device).

**Every non-admin was completely locked out, and no bench before this one could have seen it.**
`POST /api/auth/login` succeeded for an operator or a viewer; `GET /api/auth/session` — the route
the SPA asks on boot to learn who it is talking to — was absent from `services/rbac.go`'s
deny-by-default catalog and answered `403`. `App.js` treated any non-OK answer as "nobody is
signed in" and rendered the sign-in card, so signing in successfully led straight back to the
sign-in card, forever, with no error anywhere. Every API bench this app has ever had ran as admin,
which bypasses the matrix entirely — the one role that could never have found this. Fixed by
adding `/api/auth/session` to the catalog as `read` for viewer and operator; existing installs
pick it up on upgrade, since `EnsureApplianceRoles` adds missing catalog rows to already-configured
roles.

**A refusal rendered as an absence, twice.** The SPA now tells `403` apart from `401` on the
session probe — a new `NoAccessScreen` ("you are signed in, but this account has not been granted
access") replaces the login-card loop. And a viewer, correctly refused telemetry history, used to
see the device Readings tab draw nine empty charts saying "No readings in this period" — the exact
words a dead sensor would produce. `loadSeries` now carries `denied: r.status === 403`, the charts
say "Your account cannot see reading history", and one banner explains it once above all nine
rather than making the operator read the same lie nine times.

**The Add-device form offered a protocol that has never had anywhere to go.** `PROTOCOLS =
['mqtt', 'http']`, and grepping the whole repo finds no HTTP ingest route, ever. `Protocol` is read
by no code — the broker admits on credentials, the Modbus poller reads the profile's transport —
so a device created as `"http"` was provisioned, enabled, and permanently mute, with nothing
anywhere reporting the mistake, and no way to correct it afterward (`UpdateDeviceRequest` has no
`Protocol` field). The form now offers `mqtt` only; `services/device.go` gained a closed
`supportedProtocols` set (`mqtt`, `modbus`) that `Create` refuses anything outside of, with a
readable message. The `protocol.http` i18n key stays, deliberately, so an install with an existing
such device still renders its label instead of the raw key.

**Also proven good, precisely because nothing here needed a fix**: all eleven sections render in
en/ms/zh/ar with no leaked dictionary keys and no unhittable controls; Arabic lays out RTL
correctly with the nav rail on the right and nothing overflowing; actuation from the Control tab
demands a confirmation stating the physical consequence, cancelling issues nothing, confirming
produces exactly one `device_command` row attributed to the real user; a device with
`actuationEnabled` off explains itself instead of offering a dead button; create/delete round-trip
through the screen and reach the server.

## myiotsan — active network discovery scanning

`bench_iotsan_discovery.py`, **38/38 with the fix; 36/38 against unfixed main**. Every bench of
this app so far acted on a device an admin had already entered: a known endpoint, a chosen
profile, a deliberate command. Discovery inverts that — an admin types a network range and the
appliance goes and touches everything in it, equipment it was never told about, possibly on an OT
network, possibly mid-duty. `infra/iot/discover`'s package doc states the posture that is supposed
to make that acceptable in one sentence — READ-ONLY, LAN-local, bounded, cancellable — and this
bench takes each claim and tries to break it, because three of the four fail silently: a write a
device accepts looks identical from the caller's side to a read it answered; a host that accepts a
connection and then stalls looks identical to one that was never dialled; a sweep that quietly
reached past the LAN returns the same `200 OK` a legitimate one does.

`modbus_tripwire.py` is what makes the READ-ONLY claim measurable rather than assumed: a real
Modbus TCP device that answers as a plausible SunSpec device (the longest, most-identifying code
path the scanner has) and records the **function code** of every request it receives, split into
reads and writes. It deliberately *accepts* a write with a success reply instead of an exception —
a scanner that only falls back to a read on a write failure would otherwise hide the evidence by
never sending one that fails. It also carries `Blackhole`, a socket that accepts a TCP connection
and then says nothing, which is the only thing that actually exercises a per-host timeout: a closed
port fails instantly and proves nothing about a bound.

**It found one defect, and it is the claim stated most plainly: LAN-local was never enforced.** The
multicast scanners (mDNS/SSDP/EtherNet-IP/BACnet) are link-local by construction — they take no
target and broadcast to a fixed address — but the Modbus sweep takes its target from a CIDR string
an admin types, and nothing checked it. Measured against a running appliance: a sweep of
`192.0.2.0/24` (TEST-NET-1, public address space) was accepted and really ran, taking 6.4s; so was
`127.0.0.1/32`, the appliance's own loopback. An unconstrained sweep target makes the endpoint a
general-purpose port scanner for whoever holds the admin session, pointed wherever the appliance
has a route — outward past the plant network, or inward at services bound to loopback that were
never meant to face a network at all. Fixed in `discover.Hosts` — the one funnel every sweep target
passes through — with a `checkLANLocal`/`isLANLocal` pair that refuses anything outside RFC 1918 +
RFC 6598 CGNAT + RFC 3927 link-local + IPv6 ULA/link-local, plus loopback/unspecified/multicast
outright. After the fix, the refusal is instant (0.0s) where the sweep used to cost 6.4s.

**Everything else held, and is now proven live rather than merely designed that way**: against the
tripwire, the whole scan issued exactly four requests, all function code 3 (read holding
registers) at the SunSpec model-chain addresses — no write-single, write-multiple or mask-write
ever arrived; a `/8` is refused before any probe goes out ("scan range too large"); a stalling host
is bounded by the per-op timeout rather than left to the OS; abandoning the request mid-scan stops
the scanner dialling (the blackhole's connection count froze and stayed frozen); a scan produces
candidates and adds no device, an identified SunSpec hit gets a profile *suggested* not assigned,
re-scanning refreshes rather than duplicates; operator and viewer are both refused
`POST /api/discovery/scan` and `POST /api/discovery/window`; and a scan is recorded in the
notification feed with what it found.

**An honest limit, not a defect, worth writing down rather than tripping over later**: the scan is
synchronous and holds its HTTP request open for the whole sweep. 256 unreachable hosts cost 6.4s at
the shipped 800ms/host and 32-way concurrency, so the 1024-host cap is roughly 26s, and the full cap
with all five scanners selected measured **41.7s**. That only completes because myiotsan's shipped
`config.json` disables the write timeout (`writeTimeoutSeconds: 0`); apphost's default of 30s when
the key is absent would be exceeded mid-scan. The host cap and that timeout are two numbers that
have to move together — now stated on `ScanService`'s own doc comment
(`apps/myiotsan/services/scanner.go`).

Facts worth not rediscovering:

- The tripwire and the blackhole each bind their own port (`TRIPWIRE_PORT`/`BLACKHOLE_PORT`); both
  must be free before the bench starts, and both are on `HOST`'s address, not `127.0.0.1` — the
  appliance dials out from inside its own network namespace.
- `192.0.2.0/24` (TEST-NET-1, RFC 5737) is the right probe for the public-address-space question
  precisely because it is reserved-for-documentation and unrouted — it proves the scanner does not
  confine itself to the LAN without pointing a real sweep at anyone's real network.
- The starvation check (Part 6) reads `GET /api/devices/stats`'s `dropped` count before and after a
  scan rather than asserting a bare "did it error" — the same before/after shape
  `bench_iotsan_flows.py` used to prove a flow worker was not being starved.

## mypintusan — the first bench of the app that opens doors

`pintusan_harness.py` + `bench_pintusan_door.py`, **32/32**. The app had the thinnest unit
coverage in the suite and no live exercise at all. Its decision path is a pure function with
genuinely good table tests, which is exactly the shape that lulls: `Decide()` can be flawless
while the SNAPSHOT handed to it is wrong, and no test of a pure function will ever notice. So
every claim is made by presenting a card on a real OSDP bus and reading what the product
recorded.

The core access path held up — a valid badge grants, a revoked card stops opening the door
within a second, lockdown denies, a duress PIN grants while raising a critical alarm, and a
remote unlock is recorded with the operator's name.

Things it cost a run to learn:

- **The simulator's DEFAULT CARD could never open a door.** `deadbeef` fails Wiegand-26 leading
  even parity, and a CP treats a parity failure as a hard denial (a card one bit out may be
  somebody else's). The first run watched every badge denied and looked like a broken grant
  path. The default is now `00880040` — facility 1, card 4096, valid parity. **If you change
  it, decode the new value with `services.DecodeCard` first.**
- **mypintusan is an APPLIANCE app**: shared local Basic auth (`GET /api/auth/session`), like
  mymatasan and myiotsan. Not myseliasan's `/api/auth/local-login`, not myidsan's
  `/api/login/default`. Guessing wrong costs a confusing run of 404s.
- **Creating a DOOR creates its reader.** `POST /api/doors` takes `busPort` + `osdpAddress` and
  writes both rows; readers are list-only, so there is no reader-create endpoint to hunt for.
- **A holder needs `ref` AND `name`** (the API decodes an `entities.Holder` directly).
- **Lockdown takes `{"lockdown": true}`, not `{"on": true}`, and returns the RESULTING state.**
  Assert the state: this bench first passed on a 200 whose body said `lockdown: false`, having
  asked for something the API never parsed.
- **Alarms surface as NOTIFICATIONS** (`/api/notifications`). There is no `/api/events/alarms`;
  asking for one returns nothing, and a substring fallback then reported a duress alarm on a run
  with no duress in it.
- **The access event carries no strike duration**, so a strike-parity check compares two absent
  fields and passes on `None == None` — the fourth empty-result pass in this bench series.
  Duress strike parity is structural (one expression, no duress branch) and belongs to the unit
  tests; what a live run can show is that the DECISION is indistinguishable.
- **Badge before provisioning if you want to test the unprovisioned state.** Asking "was an
  unenrolled reader misreported?" after provisioning finds no such events and passes on nothing.
- **The bus port must be `tcp://host:port`** and the container must reach the simulator on the
  host's LAN address, not `127.0.0.1`. Buses are seeded from config on FIRST BOOT only — a
  surviving data dir means the second run silently ignores the bus it was just handed, so the
  harness wipes it.
- Kill a stale simulator with `taskkill //F //IM osdp-sim.exe` on Windows; `pkill` does not
  reach it, and the old process keeps the port and keeps badging the OLD card.
- **Seen but NOT fixed:** a remote unlock that races another command on the same reader is
  refused with the raw protocol string `osdp: superseded by a newer command`. It is a genuine
  transient (12/12 succeeded once the bus was up) and the superseded command really did not
  execute — but it is the one action where an operator most needs to know whether the door
  opened, and a protocol string does not tell them.

## mypintusan — the OSDP Secure Channel

`bench_pintusan_securechannel.py`, **18/18** (13/18 against unfixed main). Item 1 of a new
mypintusan hardening register — the app that physically opens doors, which had exactly one live
bench before this (`bench_pintusan_door.py`, above). Secure Channel is the only thing standing
between a screwdriver and a building: RS-485 is a two-wire bus in a ceiling void, and with no
Secure Channel, card numbers cross it in clear and replies replay. Every other gate in the
decision path — schedule, grant, revocation — is reasoning about a credential the reader TOLD it
about, so all of them are worth exactly what the reader's word is worth.

`tools/osdp-sim` shipped six Secure Channel scenarios and three of them — `refuse-sc`, `no-sc`,
`wrong-key` — had never actually badged a card, so they could prove a session was refused and
nothing about what happens next, which is the half of "must fail closed" that matters. They now
run a card loop like the other three. Each episode in this bench is a **fresh boot**: a reader's
key and its `RequireSecureChannel` policy are seeded from config on first boot only, and there is
no API to change either afterwards, so "what happens with the wrong site key" can only be asked
of a new appliance.

Two real defects, both fixed:

- **A session that established and then dropped left the door granting on cleartext.**
  `infra/access/osdp/bus.go`'s `secureChannelLost` only re-announced the downgrade from
  `StatusSecuring` — a reader that never got a session up. The harder case is the one that missed:
  a session that establishes and then drops leaves the reader `StatusOnline`, so no event fired
  and every consumer kept the `SecureSession: true` it was told at handshake, permanently.
  Measured against the simulator's `sc-drop` scenario: a door requiring an encrypted session went
  on granting badges on a reader whose session had died. Fixed by re-announcing from `StatusOnline`
  too, so the downgrade is a fact the caller learns rather than one only the bus knows.
- **A `critical`/`perimeter` door created without mentioning Secure Channel did not require one.**
  `entities/door.go`'s own field comment has always said `RequireSecureChannel` "defaults on for
  perimeter and critical," but the create handler passed the request body's boolean straight
  through while every neighbouring field (`UnlockSeconds`, `HeldOpenSeconds`, `OfflinePolicy`) got
  a real default. Measured: a card on a plaintext reader opened a `critical` door. Fixed with
  `entities.SecureChannelDefault(class)` and turning the request field into a `*bool`, so "the
  caller said false" is distinguishable from "the caller said nothing." This one mattered more
  than an ordinary defaulting bug because **there is no `PUT /api/doors`** — a door created with
  the wrong policy keeps it for good.

Proven good and unchanged, recorded rather than reported as fixed: a healthy secure session
grants; `no-sc` and `refuse-sc` both deny with the specific reason `secure-channel-failed`, not a
vaguer one; a door that does **not** require SC still works with a reader that cannot do it, so
the guard is not a blanket no; a key mismatch never becomes a cleartext session and IS alarmed
(`access.secure-channel`, "Reader secure channel fault").

**A feature gap, found and reported, not built:** `Reader.ScbkState` is written once at creation
as `default` and never updated — `ScbkRekeyed`/`ScbkFailed` are declared in `entities/reader.go`
and assigned nowhere. `infra/access/osdp/pd.go` already answers a PDCAP capability request and
`bus.go` already carries the result as `Event.DefaultKeySession`/`Event.DefaultKey`, but nothing
in `apps/mypintusan` reads either field. The simulator's own `default-scbk` description names the
rule that is therefore unimplemented: such a reader "must be capped at `interior` until rekeyed."
The direction of the error is the safe one — the app never claims a reader is rekeyed without
evidence — but it cannot distinguish a factory-keyed reader from a properly keyed one, and no
trust cap is applied.

## mypintusan — which of the six alarms can actually fire

`bench_pintusan_alarms.py`, **19/19** (9/19 against unfixed main). Item 2 of the mypintusan
hardening register. `services/controller.go` declares six alarm kinds — `duress`, `tamper`,
`reader-offline`, `secure-channel`, `door-forced`, `door-held-open` — and each has a severity, a
headline written to be read at 3am on a phone, and a translation in four languages. That is what a
declared alarm looks like from the inside, and none of it is evidence that the condition can ever
reach it. This is the detector that found five dead audit constants on myidsan, pointed at the app
that opens doors: take each constant and make the real condition happen.

**Three of the six could not fire on any installation.** The cause was one missing thing on the
wire. A PD answers `POLL` with an `ACK`, or with a queued card read or keypad entry — it **never
volunteers a status change** — so `LSTAT` (tamper, mains) and `ISTAT` (the door-position contact)
have to be scheduled by the CP, and nothing in `infra/access/osdp` ever sent either. Downstream of
that hole everything existed and was unit-tested: the door state machine, `Controller.ContactChanged`,
the forced and held-open events, the audit reasons, the CRITICAL severity on `door-forced`, and
`TestBusTamperSurfaces`, which passed for the life of the driver by handing the bus an `LSTAT`
itself. The alarms were reachable from a test and from nowhere else.

- **`tamper` never fired.** Somebody opening a reader's enclosure — step one of getting at the
  RS-485 pair that Secure Channel exists to protect — raised nothing.
- **`door-forced` never fired.** A door opening with no grant and no exit request raised nothing.
- **`door-held-open` never fired.** A door wedged open past its threshold raised nothing. This is
  the commonest way a secure door stops being one.

Fixed by giving the poll loop a supervision schedule (`Options.StatusInterval`, default 1s,
`statusMillis` per bus in config), alternating `LSTAT` and `ISTAT`, skipping `ISTAT` on a reader
whose PDCAP claims no inputs, and emitting both on the EDGE so a case left open reports once rather
than once a second forever. `Controller` turns an input report into `ContactChanged` for the door
bound to that reader — **input 0 is the door contact**, which is a convention the driver documents
rather than something OSDP states.

**A fourth defect, in the other half of an alarm: the reader list.** `Reader.TamperState` was
written once as `ok` when the reader was enrolled and never again, and `LastSeenAt` was never
written at all. Measured live: while the `reader-offline` alarm was firing correctly, the readers
screen still said `tamperState: ok, lastSeenAt: 0`. The alarm woke somebody and the screen they
opened lied to them. Now `Controller.markReader` persists what the bus observed, and `LastSeenAt`
is deliberately **not** stamped when a reader is declared offline — it was last seen when it last
answered, not when we gave up on it.

**A defect this work introduced, and the existing suite caught.** The first version of the
scheduler gated only on elapsed time. `TestBusOneReaderDownDoesNotTakeTheBus` went red, and the
reason is worth keeping: a card is queued at the PD and handed over on a `POLL`, and every dead
reader on a segment costs a full reply timeout — so a degraded bus can take longer to go round than
the status interval, at which point **every single transaction is a status command and no badge is
ever delivered**, while the bus looks up and supervision looks healthy. Supervision is now capped as
a fraction of a reader's slots (one in four) as well as by time, pinned by
`TestBusStatusNeverStarvesCardDelivery`.

What the bench had to build to ask the question at all: `tools/osdp-sim`'s `tamper` and `silent`
scenarios presented no card, so neither could establish that its reader was ever in service — the
same defect `refuse-sc`/`no-sc`/`wrong-key` had in item 1, and both now badge. Door position had no
scenario whatsoever, so `contact-open` and `contact-cycle` are new.

The episode that matters most is the negative one. A forced-door alarm on every legitimate entry is
worse than no alarm at all — it is the fastest way to teach a site to ignore the one alarm that
means somebody is inside who should not be. But "no forced alarm" is also exactly what a contact
that was never reported produces, so the episode sets the door's held-open threshold *below* how
long the contact stays open: **`door-held-open` firing is the positive evidence that the opening
reached the state machine**, and only then is the forced alarm's absence a statement about the
shunt.

Proven good and unchanged: `duress` fires at CRITICAL (re-run as the control for the whole
notification path, so that "no alarm arrived" below it is never confusable with "the feed is
broken"); `reader-offline` fires, and at WARNING rather than CRITICAL, which is the deliberate
choice `alarm.go` argues for — paging on one flaky segment trains people to ignore alarms.

**Still not built, and reported rather than papered over:** the `myiotsan` door-contact binding
(`Door.ContactDeviceKey`). A site whose contacts terminate on a relay board rather than on the
reader still has no forced or held-open detection. `Controller.RequestToExit` likewise has no
caller, so a REX button wired to the reader does not shunt the alarm.

## mypintusan — offline mode and the cache TTL

`bench_pintusan_offline.py`, **19/19** (12/19 against unfixed main). Item 3 of the mypintusan
hardening register. `Decide()`'s GATE 10 is the app's answer to the oldest attack on an access
control system, and the code says so in its own words: "past the TTL the door denies... there is
no allow-all option anywhere in this path: 'fail open on network loss' is a documented attack —
cut the uplink, walk in." `docs/MYPINTUSAN_DATA_MODEL.md` §2 turns that into a table an installer
can read — interior serves from cache for 72h, perimeter for 24h and raises a degraded-mode alert
immediately, critical for 8h and only for holders flagged `OfflineAllowed`. Every line of it is
implemented in `Decide()`, and its unit tests are good — they even pin the gate ordering. What a
unit test cannot see is where the numbers it hands the gate come from on a real controller, which
is exactly the shape #220 found in the alarms: the gate was right, the reason codes were right,
the translations were right, and nothing fed the gate a number.

**Four defects, all of the same shape — "the code was right and nothing could reach it" — all
fixed:**

1. **The cache could never expire.** `ControllerConfig.CacheAge` was declared and compared in
   GATE 10 and never assigned outside a unit test. In production it was nil, so
   `Snapshot.CacheAge` was always zero, `s.CacheAge > ttl` was always false, and
   `offline-cache-expired` could not be produced on any install. Measured: a door 20 seconds past a
   2-second TTL still granted. Fixed by new `services.CacheClock` — persisted (so a reboot mid-
   outage cannot reset staleness to zero), fed by two contact signals: the fleet control channel
   being live (`ControlChannelManager.SetOnContact`, fired on hello and on every parent frame
   including the keepalive pong) and any accepted administrative change to the access rules on the
   appliance itself (`ruleChangeTouch` middleware, scoped by path prefix and to 2xx responses).
2. **`offlinePolicy: deny` and `offlineTtlSeconds` were unreachable.** `createDoorRequest` had
   neither field; the handler hardcoded `cached` at the class default TTL (8/24/72 hours), and
   there is no `PUT /api/doors`, so every door on every install was born that way and kept it for
   good. Fixed: both fields accepted on create; `offlinePolicy` is validated to `cached`/`deny` and
   REFUSED for anything else — notably any spelling of "allow" — rather than coerced, because there
   is no fail-open policy in this product; a negative TTL is refused too.
3. **Turning offline mode on did not reach the running controllers.** The runtime read `Offline`
   once at process start. `PUT /api/settings/access` with `offline: true` persisted, returned 200,
   read back `true`, and every door carried on deciding as though online. Fixed:
   `ControllerConfig.Offline` became `func() bool` bound to an atomic on the runtime, and
   `IAccessSettingsService` gained `OnChange`, published on Save and Reset. Only `offline` applies
   live — buses, reader keys, timers and the site timezone still need a restart, and the Settings
   screen's restart note was reworded to say exactly that.
4. **Nobody was told the site was degraded.** `docs/MYPINTUSAN_DATA_MODEL.md` §2 has said since P1
   that a controller serving from cache "raises a degraded-mode alert immediately"; nothing raised
   one, and a cached grant is byte-identical to a live one at the door and on every screen. Fixed:
   new alarm kind `services.AlarmDegraded` ("degraded"), WARNING, headline "Running from cache —
   access rules cannot be updated", raised on the transition into offline mode AND at boot for an
   appliance that starts offline (which never crosses the edge and would otherwise run degraded
   forever with nobody told).

**Also added, not a defect but a gap:** offline mode had no control on the Settings screen at all,
so the only way to turn it on was editing `config.json` before an appliance's first boot — on a
facilities-managed install, never. A checkbox was added
(`settings.offlineHead`/`offline`/`offlineHint`, translated in en/ms/zh/ar).

**The positive control, and it is load-bearing.** Offline mode changes almost nothing you can see
— a cached grant looks exactly like a live one. Episode 3 badges at a CRITICAL door with a holder
who is not flagged `OfflineAllowed` and requires the `offline-not-allowed` denial, which can only
come from inside GATE 10. Until it is observed, "the TTL did not deny" and "the gate never ran at
all" are the same observation, and the whole file would be measuring nothing.

Proven good and unchanged: `Holder.OfflineAllowed` was already reachable via `POST /api/holders`
and works end to end — a holder flagged offline-allowed opens a critical door while offline, one
who is not is refused; the access log's `offline` flag is set correctly on a cache-served
decision; a revoked credential is denied `credential-revoked`, not masked by a stale cache — but
only provably so once the TTL denial above actually fires, so the bench reports this check as
UNPROVABLE rather than a false pass if it doesn't; and no `allow-all` offline policy can be
created, before or after the fix.

**Two things deliberately not built, reported rather than papered over:**

- There is still no `PUT /api/doors` and no doors-admin screen — doors are only created by the
  first-run wizard — so the new offline policy fields are reachable through the API only.
- There is no replica-refresh mechanism anywhere in this suite: `myseliasan` pushes no access data
  down, and no other route does either, so the appliance's own database remains its only authority
  and "offline mode" stays an operator-declared deployment mode rather than something the
  controller detects for itself. `docs/MYPINTUSAN_DATA_MODEL.md` §2's sentence describing "a full
  local replica... refreshed on change" described a topology that never existed and has been
  corrected to say so.

## mypintusan — the screens

`bench_pintusan_screens.py` + `uicheck_pintusan.js` (`_partb`, `_partc`), **166/166** (91/96
against unfixed main, where the whole operator and viewer half was unreachable and therefore
unmeasurable). Item 6 of the mypintusan hardening register, and the first screen check this app has
ever had — six API benches in, it was the last app in the suite with none.

Six passes: PART A in **all four languages** including RTL Arabic (every section renders, has a
heading, leaks no dictionary key, every enabled control is hit-testable at its own centre), PART B
admin workflows driven with real input events and confirmed against the server (lockdown engaged
and released from the Doors screen, a remote unlock that travels OSDP to the strike, a badge issued
and revoked, a grant revoked, a holiday created, the timezone saved), and PART C for the operator
and viewer — **does what the screen OFFERS match what the server ALLOWS, in both directions?**

**PART C is the one that paid.** This app decided authorization twice: the server with the
deny-by-default matrix in `services/rbac.go`, the SPA with a client-side `user.isAdmin`. The two
drifted in **both** directions at once — a viewer was offered an Unlock button on every door card,
while an operator granted read on grants and schedules had the entire Access rules section hidden.
Three of the five defects meant a non-admin could not use the product at all: the session probe was
not in the catalog (sign in succeeds, then the sign-in card again, forever), the Doors screen
loaded the door list and the admin-only lockdown state in one `Promise.all` (so the home screen was
a permission error), and the operator's defining power — `POST /api/doors/{id}/unlock` — was
governed by a rule written `/api/doors/unlock`, three segments against four, matching nothing.

Fixed at the root rather than patched: `GET /api/auth/capabilities` answers by calling `Authorize`
on the REAL route each control would send, and every screen renders from it. And
`apps/mypintusan/services/rbac_test.go` — the first test that catalog has ever had — asserts every
rule governs a route the app serves and every route is covered by a rule, in both directions, which
catches the segment-count class of bug at build time. `PathGoverns` is exported from
`domain/shared/services` so any app's catalog can be tested the same way.

**AND THE ONE NO ASSERTION COULD SEE.** Every check passed while every table on every screen
rendered **raw white browser buttons labelled "Sort"** jammed under each column heading, in all four
languages, on every install: `@shared/DataTable` ships markup and no style — "styled by the
consuming app's stylesheet", says its own header — and mypintusan was the only app in the suite that
used it and never wrote them. The screens rendered, the controls were hit-testable, no key leaked,
the API answered correctly. **Open the PNG. It is the third time on this suite that a green screen
check has coexisted with a visibly broken screen.**

Traps this one added, in the order they cost a run:

- **`timeout python …` strips `TMP`/`TEMP`.** The MSYS `timeout` wrapper drops them crossing into a
  native Windows process, and `go build` then tries `C:\Windows\go-build…` and fails with "Access is
  denied" — which reads as a broken toolchain. Use the runner's own timeout, not `timeout`.
- **A response-body cap silently empties a list.** The first draft sliced `api()` bodies to 6000
  characters — sensible for an error message, fatal for a hundred access-log rows: `JSON.parse`
  threw, the unwrap returned null, `items()` handed back `[]`, and every before/after diff compared
  nothing with nothing. It reported that pressing Unlock had not reached the controller and that
  revoking a grant told nobody — **two defects that did not exist, in a bench written to find
  defects that do.** The cap is now 400_000 and `items()` records any OK response that failed to
  become a list, asserted at the end of PART B.
- **The sign-out button is a `.nav-item`.** A bare `button.nav-item` sweep clicks it somewhere in
  the middle of the section sweep, and every later check then fails against a login card. The
  selector excludes the rail foot, and a check asserts the exclusion is actually holding something
  back rather than silently matching nothing.
- **`\s` inside a template literal is `s`.** The blocks passed to `js()` are template literals, so
  an unrecognised escape loses its backslash before the browser sees it: `/time\s*zone/` arrives as
  `/times*zone/` and reports a field that is right there on screen as missing. Double them.
- **A control that exists once per row has to be located through its row.** A bare text match on
  "Revoke" picked the first badge rather than the one just issued, then reported that the server had
  not recorded the revocation. It had — the bench revoked something else, and the simulator spent
  the rest of the run badging a card that was now `lost`.
- **A probe that really opens a door can fail for reasons that are not permission.** PART C's unlock
  anchors on "not refused" rather than 200: a dead simulator answers 400 "PD did not reply", which
  says nothing about whether the operator is allowed. The driver also restarts the simulator between
  passes — a ten-minute run over six browser passes will outlive one.

## `onvifsim.py` — a real ONVIF device, on demand

The bench cameras are mediamtx RTSP sources with **no ONVIF service at all**, and everything
in W3-5 is an ONVIF conversation. `onvifsim.py` is a small ONVIF PTZ device (stdlib only,
runs in a bare `python:3-slim` container on `benchnet`) that answers the SOAP calls the
product makes, keeps the state a real dome keeps, and — the part that makes it worth having —
**records every call**, at `GET /journal`.

That is what lets `bench_w35_ptz.py` assert the appliance *sent* `GotoPreset` for the stops of
a tour, in order, spaced by the dwell. A patrol that persuaded its own database it was
running while sending nothing would pass a status-code check and fail this one. It also
exposes `POST /presets/wipe` (somebody clearing the presets from the camera's own web page)
and `POST /journal/reset`.

Every bench and screen check that needs it starts and removes it itself, so none of them
depends on another having been run. **Extend it rather than starting again** for anything
else ONVIF — W3-5b's PullPoint events and relay outputs went into the same file, which took
about twenty minutes.

It now also speaks the W3-5b surface, with two controls a real camera will not give you:

* `POST /inputs/<token>` — flip a digital input. This is how a bench opens a door.
* `POST /subscriptions/expire` — drop every subscription **without telling anybody**, which
  is exactly what a camera does when a lease is not renewed, and the whole reason the event
  listener treats silence as a fault.

For W3-6 it also speaks **Media2 privacy masks**, and it can be told to LIE about them,
which is the whole reason that bench is worth running:

* `POST /masks/mode/<honest|shifted|rectangle|drop>` — store something OTHER than what it was
  sent: a different coordinate space, a squared-off polygon, or nothing at all. A camera that
  accepts a mask with HTTP 200 and applies something else is a real device behaviour, and the
  product's read-back verification exists for exactly it.
* `POST /masks/support/<on|off>` — a camera with no mask support at all.
* `POST /masks/limit/<n>` — how many masks it will hold.

`LOCK` is an **RLock**, not a Lock: `note()` takes it to append to the journal and several
handlers record something while already holding it. With a plain Lock that is a deadlock, and
the symptom is the entire simulator going silent mid-bench — which reads exactly like a
product failure. It was found precisely that way.

`bench_w34_dwell.py` (W3-4) states at the top of the file what it does NOT claim: no
evaluator is driven end to end, because the harness films test patterns and the detector
finds no person, bag or vehicle to track. It benches everything around them — creation,
refusals, persistence across a restart, the alert and notification path, the role model — and
the evaluators are unit-tested and mutation-checked instead.

**`result_list(response, *keys)`** is in the harness now. `result_of` re-wraps a bare array as
`{"result": [...]}`, and three benches have iterated the dict instead, got the string
`"result"`, and reported that the fleet had no nodes / no roles / no rules. Use `result_list`
for any endpoint that answers with a list.

`bench_w33b_walls.py` (W3-3b) runs on the plain node image in about a minute: a wall is an
arrangement of camera ids, so it needs no footage. It restarts node-a to prove walls survive
it, and deletes a camera to prove a wall reports the loss — which is how it found that
deleting a camera failed on most cameras. `uicheck_wall.js` runs TWO Chrome profiles and the
second one is the point: what this feature replaces was a cookie, so reading a wall back in
the profile that saved it proves nothing. Run the API bench first — the screen check looks
for the wall whose camera it deleted.

`bench_w33_cases.py` (W3-3a, case files) needs the **ffmpeg node image** and runs for about
nine minutes: it records real footage on two cameras, opens a case over some of it, then
moves both the segments and the case items three days into the past so the shipped one-day
retention policy considers them expired, and drives every deletion path at them. It leaves
two camera sources behind (`casesrc-one`, `casesrc-two`) and writes `w33_context.json`.
`uicheck_cases.js` does not depend on that file — it creates its own case through the UI —
but it does need footage on the node, so run the bench first.

`bench_w32_embedding.py` (W3-2) runs on the HOST interpreter rather than in a container: the
appearance stage rides torch + torchvision, which the anomaly feature already requires and
which no bench image carries. `bench_w32_appearance.py` needs the fleet up, creates its own
cameras, and SEEDS descriptors directly into each node's sqlite — the harness films synthetic
test patterns, so no detector produces a person to describe. It writes `w32_context.json` for
`uicheck_appearance.js`, so run them in that order.

`bench_w31_timeline.py` (W3-1, timeline playback) needs the **ffmpeg node image** and runs
for about eight minutes: it stands two mediamtx sources up, records both, then `docker
pause`s one for 150 seconds to punch a real hole in its footage. It writes
`w31_context.json` into the bench dir; `uicheck_timeline.js` reads that, so run the two in
that order. The screen check PLAYS rather than looks — it clicks the bar, presses play,
changes speed and reads `currentTime`/`playbackRate` back out of the live `<video>`
elements, because those are the four things that can only fail in a browser.

`bench_w27_email.py` brings its own dependency: `smtp_sink.py`, a real recording SMTP server
run as a container on `benchnet`. **Remove it before re-running the harness** —
`docker rm -f smtp-sink` — or `teardown` cannot delete the network, `docker network create`
fails, and the fleet comes up unadopted.

The same applies to any bench that attaches its own containers to `benchnet`.
`bench_w31_timeline.py` leaves two camera sources behind:

```
docker rm -f smtp-sink camsrc tlsrc-steady tlsrc-gappy casesrc-one casesrc-two
```

**Swapping a node's binary needs the per-node copy.** `node_binary()` gives each container
its own `bin/mymatasan-<name>`, so rebuilding `bin/mymatasan` alone changes nothing until
those copies are refreshed and the containers restarted. The symptom is a bench asserting
against response fields the running build has never heard of.

is the sweep to run before re-standing the fleet up. The symptom of forgetting is always the
same and never mentions containers: the network cannot be recreated, so both nodes come up
unadopted and every assertion fails for a reason unrelated to the item under test.

**`uicheck.js` is the screen half, and it is not optional for an item that ships a screen.**
W2-4's API bench passed 36/36 and the screen it shipped still lied — every sighting from a
camera that records nothing was labelled "Recording…" forever. A green backend and a
misleading screen look identical from the API side. It drives headless Chrome over CDP (no
puppeteer, no npm install): signs in, skips the first-run wizard, clicks the nav entry whose
label you name, submits the page's form, prints a JSON summary of the rendered DOM, and
writes a screenshot. **Assert on the JSON, not on the screenshot** — an assertion you have
to squint at is not one.

Container data dirs and bench output go to `.artifacts/fleetbench/` (gitignored); override
with `KOPIV2_BENCH_DIR`. **Point it at a roomy drive**: the node's disk guard reads the
HOST volume through the bind mount, so a nearly-full disk pauses recording fleet-wide and
any bench that needs footage measures nothing. The guard working is a feature. This bit
W3-3a on a drive at 95%: the nodes paused recording within a minute of boot, and the
symptom — no segments, ever — is indistinguishable from ffmpeg not running. The `bin/`
directory the containers mount lives under the bench dir too, so it moves with it.

A bench that needs the nodes to RECORD must also run them on an image that has ffmpeg —
`KOPIV2_NODE_IMAGE=debian-ffmpeg:bench python tools/fleetbench/fleet_harness.py`. Without
it the recorder cuts nothing, silently. Rerunning the harness wipes those dirs, so each run starts from a
fresh install rather than inheriting a rotated password and an already-paired node.

Docker is the only prerequisite; sqlite is pure Go, so no CGO and no database container.

## Self-update benching (W2-5, `KOPIV2_NODE_HOME_RW`)

By default the harness bind-mounts `apps/<app>` **read-only** into `/home/app` — safe, but a
node whose home is read-only fails `canSelfUpdate`'s writable-probe (a throwaway file it tries
to create) exactly the same way a real package/container install does. That is the correct
default for every bench that doesn't touch self-update; it is a trap for one that does, since
the failure looks identical to "the feature doesn't work" until you notice it's the mount, not
the code.

Set `KOPIV2_NODE_HOME_RW=1` before running `fleet_harness.py` for any bench that exercises a
node's own updater (directly, or driven by a `myseliasan` fleet rollout). This makes
`node_home()` copy `apps/<app>` (minus `views`/`node_modules`/`data`/`logs` — the React source
tree is large and unused at runtime) into a private, writable directory per node under
`KOPIV2_BENCH_DIR`, and bind-mounts *that* instead. It costs a one-time copy per node at
startup and nothing else; every other bench is unaffected since the flag defaults off. Never
make the repo checkout itself writable from a container — a bug in a self-update bench would
then be a bug that can write into your working tree.

## The traps, all of which cost real time

- **`pairing.parentBaseUrl` decides whether the fleet works at all.** The parent STAMPS this
  URL onto every node it adopts, and the node enrolls and dials the control channel with it.
  Left at its default the node records `localhost:3002` — its own localhost — so enrollment
  fails forever, no certificate is ever issued, the control channel never comes up, and the
  node drifts to "lost" 90 seconds after adoption on its own. A bench that then stops a
  container measures an outage that was already happening. The harness sets it to
  `https://cp:3002`; `certExpiresAt != 0` on the adopt reply is the fast confirmation.
- **Gate any liveness bench on the fleet being genuinely WATCHED**, not merely on a node
  showing "online" — adoption sets that status itself. `bench_w22_sla.settle()` requires both
  nodes to hold online across three consecutive sweeps.
- **The two apps do not authenticate the same way.** mymatasan accepts `Authorization: Basic`
  on everything; myseliasan needs `POST /api/auth/local-login`, a cookie jar, and the CSRF
  token echoed from the `__Host-kopiv2_csrf` cookie into `X-CSRF-Token` on every write. Both
  apps ship a must-change bootstrap admin, so rotate before anything else.
- **`requests`' `session.verify = False` is overridden by `REQUESTS_CA_BUNDLE`** in the
  environment, and the resulting certificate error reads like the app's fault. Set
  `session.trust_env = False` (the harness does).
- **Field names.** The control plane returns the fleet key as `fleetKey`; the node takes it as
  `key`; the node's claim-code reply calls the code `code`; the adopt body wants `nodeId`,
  `httpsPort` and `claimCode`.
- **Ports.** The control plane listens on 39533 (control channel) and 39534 (media).
  `pairing.mtlsPort` 39532 is not something the parent listens on — it is stamped onto nodes,
  so every node app must use it.
- **Config must be BOM-free**, or the Go loader silently falls back to ALL DEFAULTS instead of
  erroring. Write it from Python/the Write tool, never PowerShell's `Out-File -Encoding utf8`.
- **Never read a container's sqlite while the app is running** (mid-WAL over a bind mount) and
  never write it either — a seed written under a running app is discarded on restart. Stop the
  container first.
- The node's rate limiter shares one bucket per path for tunneled calls (they carry no JWT), so
  an exhaustive sweep trips it even though real traffic never would. The harness disables it.

`bench_w39_push.py` (W3-9, mobile push) needs no node image and no footage, but it does need
`openssl` on PATH and Docker Desktop's `host.docker.internal`. It stands up a **real push
service** on the host over TLS, then **restarts the control-plane container with
`SSL_CERT_FILE` pointing at that service's certificate** — the way Go trusts a CA on Linux.
That is why `start_container` takes an `env` argument: a shipped appliance must not carry a
switch that makes it accept a certificate it should not, so the trust goes in from outside.

It reads the appliance's request **off the wire**: the aes128gcm header layout byte by byte,
the VAPID assertion's audience (the endpoint's ORIGIN, never the full URL), and that the
notification text does not appear anywhere in the body. It does **not** decrypt the payload —
a second implementation of RFC 8291 written in the same sitting would only prove that two
readings of the spec agree with each other. The encryption is checked against the RFC's own
published test vector, byte for byte, in `infra/webpush/webpush_test.go`.

It runs for about seven minutes because it `docker stop`s node-a, **waits out the real liveness
grace window**, and starts it again — and it decides what should have arrived by reading the
control plane's OWN notification log rather than by assuming. That correlation is load-bearing:
the first version waited for "a new request to the phone", got one in sixteen seconds from the
other node reporting low disk on boot, and declared the fleet wiring proved before the event it
was testing had even fired.

`uicheck_push.js` deletes every device and seeds two of its own pointing at `127.0.0.1:9`
(connection refused instantly, so no twenty-second timeout), which puts the panel in the
AIR-GAPPED state — the screen that matters most on this feature. It reads the toast **2.2
seconds** after a click: a toast lives 3.5 seconds, and a four-second wait finds an empty stack
and reports a silent screen on one that spoke.

`uicheck_policy.js` needs nothing but the standing fleet. It deletes every policy first (which
is also what proves the `no policy` verdict renders), then builds one through the FORM, drives
a real drift and a real clearance against node-a over the tunnel, and deletes it again. It
writes TWO screenshots — `policy-<lang>.png` at the end and `policy-drift-<lang>.png` at the
moment the screen is actually saying something. The second one exists because the defect this
check found was only visible by looking at a frame, and the last frame of a run is usually the
least interesting one.

`bench_w37b_capacity.py` needs nothing but the standing fleet — no footage, no ffmpeg image. It
creates as many cameras as the spare says it can carry, plus five, and requires the verdict to
say so. **Every camera gets its own host** (`127.0.0.2`, `127.0.0.3`, …): saving a discovered
camera UPSERTS BY HOST, so fifty cameras at one address are one camera, and the first run of
this bench staged "1 wanted" against a fleet of five. The addresses refuse instantly because
the appliance probes a camera before saving it — a black-holed address costs a full connect
timeout per camera, which is minutes for one.

`bench_w33c_case_feed.py` and `uicheck_case_feed.js` target a NODE (`:18444`), not the control
plane. mymatasan authenticates with **Basic auth held in React state**, not a cookie — so a
plain same-origin `fetch` from the page carries nothing and answers 401 with an empty list,
which looks exactly like "the case was never created". The screen check sends the header
explicitly, the way the SPA's own client does.

`uicheck_fleet_wall.js` SEEDS A CAMERA on any appliance that has none, through the control
plane's own node proxy. That is not decoration: the one thing this screen exists to prove is a
wall built from more than one machine, and a run against a fleet where only one appliance
happens to hold cameras proves the opposite by accident — which is exactly what its first run
did. It also writes `fleetwall-live-<lang>.png` mid-run, because the last frame is an empty
screen after the cleanup and the defect this check found (tiles collapsed to 40 pixels) was
only ever visible in a picture.

`uicheck_bundle.js` needs no fleet and no database. It serves an app's built `static/` over
plain HTTP, loads it in a real browser and asserts that nothing threw while the modules
evaluated, that React mounted into `#root`, and that every design token the stylesheet declares
resolves. It exists for myidsan, myiotsan and mypintusan, which have no live instance to drive
— a frontend dependency bump changes the bundle, and a bundle can break in a way that leaves
`npm audit` clean, webpack reporting success, and the app rendering a white page.

It proves the bundle loads and the app boots. It proves nothing about whether a feature works:
there is no backend behind the page, so every API call fails and the screen reaches its
signed-out state. That is expected.
`idsan_harness.py` is a SECOND harness, not an extension of the fleet one: myidsan has no
`pairing` block (so `base_config` would fail on it) and the fleet harness force-disables SSO,
which is the one thing this benches. It stands up myidsan, a real relying app (myseliasan) and
a real redis — myidsan's shipped config names redis as the cache provider and the cache is the
SESSION AUTHORITY there, so the in-process cache would bench a deployment nobody ships.

**It uses the host's LAN address**, discovered at runtime. An authorization-code flow has three
parties that must agree on one URL per app: the browser follows a redirect to myidsan, then the
relying app calls myidsan server-to-server. `127.0.0.1` means "myself" inside a container and
`host.docker.internal` does not resolve on the host — only the LAN address works from both, and
the harness mints one certificate naming it.

It WIPES the data dirs on every run. The first version did not, and the second run tripped over
its own registrations.
