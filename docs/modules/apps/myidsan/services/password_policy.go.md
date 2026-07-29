# Module: apps/myidsan/services/password_policy.go

## Purpose

Enforces the configurable password-strength policy (`infra/config.PasswordPolicyConfigModel`,
see `infra/config/config_models.go.md`) against every **human-chosen** password, and defines
`ValidatePassword`, the single funnel all four password-setting paths now call through.

## Responsibilities

- Before this file existed, the only rule anywhere was `len >= 8`, hard-coded and applied on
  exactly two of the four paths that set a password (`ChangePassword`,
  `SetPasswordSelfService`); self-registration and admin-provisioned account creation
  (`POST /api/user-credential`) checked non-empty and nothing else — an administrator could
  create an account with the password `"a"` on the server that authenticates the whole
  suite. All four now call `ValidatePassword` (`RegisterLocal`, `POST /api/user-credential`
  via `apps/myidsan/apis/user_login.go`, `ChangePassword`, `SetPasswordSelfService`).
- `ValidatePassword(policy config.EffectivePasswordPolicy, password, identifier string) error`:
  - Rejects empty/all-whitespace passwords, but does **not** trim before the length check —
    a trailing space is a legitimate password character, and silently trimming it would mean
    the stored password differs from what was typed, surfacing later as an inexplicable
    failed login.
  - Enforces `policy.MinLength` (rune count).
  - Enforces `policy.RequireUpper`/`RequireLower`/`RequireDigit`/`RequireSymbol` when set,
    reporting **every** missing class at once rather than one failed submit at a time.
  - Rejects a password equal to `identifier` (case-folded) or to the local part of an email
    identifier used verbatim — a password matching the username satisfies every character
    rule and is the first thing an attacker tries, so it is checked unconditionally.
  - Rejects a match against the embedded `commonPasswords` denylist when `policy.BlockCommon`
    is set.
  - All rejections wrap `ErrPasswordPolicy` (`errors.Is`-checkable) with a message that names
    what to fix (e.g. the actual required length), never a bare "invalid password".
- `isCommonPassword`/`commonPasswords` — a small, deliberately curated denylist of the
  passwords that dominate leaked-credential corpora plus the substitutions people reach for
  when a policy demands a symbol or a capital (`password123`, `p@ssw0rd`, `admin123`,
  `changeme123`, `myidsan123`, etc.). Matching folds case but does not strip digits/symbols,
  so a passphrase that merely contains a common word is not penalized. This is **not** a
  substitute for a breach-corpus (HIBP) check — there is no k-anonymity lookup, since myidsan
  is positioned to run air-gapped; it is what can ship inside the binary with no egress.

## Notes

- Server-**generated** credentials (the bootstrap superadmin password, the temporary password
  issued from the operator reset queue — both `generateBootstrapPassword`'d 16-character
  CSPRNG output, see `services/user_login.go.md`) are never run through `ValidatePassword`:
  they already exceed anything expressible here, and subjecting them to e.g. a symbol
  requirement would only mean the generator had to be taught to satisfy a rule that adds
  nothing to their entropy. `password_policy_test.go`'s
  `TestGeneratedPasswordsSatisfyTheDefaultPolicy` proves generated passwords still pass the
  *default* policy regardless (so a fresh install never issues itself a credential its own
  rules would reject if it were checked).
- Covered by `password_policy_test.go`: default-floor resolution, too-short rejection
  (message states the target length), a long no-symbol passphrase accepted by default,
  common-password rejection (case-insensitive), username-equality rejection (including the
  email-local-part form), character-class enforcement + combined-missing-classes reporting,
  and the trailing-space/all-whitespace length edge cases.
- See `infra/config/config_models.go.md` for `PasswordPolicyConfigModel`/
  `EffectivePasswordPolicy` and the default values (`MinLength` 12, composition rules off,
  `BlockCommon` on). See `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 3 (§3.1).
