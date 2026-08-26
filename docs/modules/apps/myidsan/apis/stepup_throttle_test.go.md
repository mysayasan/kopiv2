# Module: apps/myidsan/apis/stepup_throttle_test.go

## Purpose

Locks in the fix found by a live bench (`tools/fleetbench/bench_idsan_mfa.py`): a live run
made twelve wrong `POST /api/step-up` password guesses in 0.6 seconds, all refused, none
throttled. `POST /api/step-up` takes a password and reports whether it was right — a
password-checking endpoint in every sense that matters — and it was the only one on the
server with no lockout behind it. An attacker holding a stolen session cookie cannot sign in
(no password) but could grind candidates against it at wire speed, since it is the one
credential the whole step-up control rests on.

## Coverage

- `stubStepUp` scripts `IStepUpService.Verify` to return a scripted `(usedRecovery, err)`
  and counts calls, so a test can assert the credential path was or was not reached.
- `stepUpRequest()` builds an authenticated `POST /api/step-up` with claims for `userId 1`.
- `stepUpApiForTest(stub)` wires a `*stepUpApi` to a **real** `sharedapis.LoginGuard`
  configured with `MaxAttempts: 3` and no artificial delay (the point under test is that the
  counter exists and engages, not the shipped tuning — a real `FailedDelay` would just make
  the test slow) and a `recordingAudit`.
- `TestStepUpLocksOutRepeatedWrongPasswords` — three wrong passwords each get `401`; the
  fourth gets `429` with a non-empty `Retry-After`. The response body is checked for tone,
  not just status: it must say "re-authentication" and "seconds", and must **not** contain
  "login attempts" — the SPA shows this text verbatim to a signed-in operator, and telling
  them there were too many failed *login* attempts describes something they did not do.
- `TestStepUpLockoutRefusesWithoutCheckingTheCredential` — once locked out, `stub.calls`
  does not increase on a further attempt: the lockout is checked **before** any credential
  work, or a locked-out caller still costs a bcrypt comparison per attempt and the throttle
  becomes the load it exists to prevent.
- `TestStepUpSuccessClearsTheLockoutCounters` — two wrong passwords followed by a correct
  one, followed by three more fresh wrong ones, never reaches `429` — a success clears the
  counters so an operator who fat-fingers twice and then succeeds is not left locked out of
  their own re-authentication.
- `TestStepUpRecordsARecoveryCodeBurn` — a step-up whose second factor was a recovery code
  (`stubStepUp.usedRecovery: true`) records `services.ActionMfaRecovery` with
  `Metadata["method"] == services.MethodRecovery` — the step-up-success entry alone looks
  like a routine re-authentication.
- `TestStepUpDoesNotClaimARecoveryBurnForAnOrdinaryCode` — the mirror image: an ordinary
  (non-recovery) step-up records zero `ActionMfaRecovery` entries.
