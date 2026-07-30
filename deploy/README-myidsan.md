# MyIDSan — install & run

MyIDSan is the **identity provider / SSO hub** for the kopiv2 suite: it owns the users,
roles and RBAC policy, and every other app (myseliasan, mymatasan, myiotsan) signs
people in by redirecting to it. It also brokers enterprise sign-in — local accounts,
Google/GitHub, LDAP / Active Directory, Kerberos SSO, and generic OIDC providers.

It is a single pure-Go binary. No ffmpeg, no Python, no model weights.

## What's in this archive

```
myidsan(.exe)      the server
config.json        defaults — edit before first start (see below)
static/            the web UI
deploy/            service definitions (systemd / WinSW / launchd) + this file
```

Everything the app writes (database, logs, TLS certs, `secret/atrest.key`) is created
next to the binary on first run, unless you point `MYIDSAN_DATA` elsewhere.

## Quick start

```
./myidsan            # Linux / macOS
myidsan.exe          # Windows
```

Then open <https://localhost:3001>. It serves HTTPS immediately using a **self-signed**
certificate generated on first boot, so the browser shows a one-time trust warning. For
a trusted chain, drop your own cert/key at `tls.certPath` / `tls.keyPath`, or front the
app with a TLS-terminating reverse proxy and switch to `server.nonTlsPorts`.

Because relying apps redirect users' **browsers** to MyIDSan, the URL you reach it at
must be reachable by those browsers (not just by the apps). Use a real hostname in
production, not `localhost`.

**Behind a reverse proxy?** See [`reverse-proxy/`](reverse-proxy/) for working nginx and
Caddy configs and the rules that matter for an identity provider specifically: registered
redirect URIs are matched **exactly**, so the proxy must forward the hostname the *client*
used (`$host`, never `$proxy_host`) or every relying app breaks at the callback; and
`X-Forwarded-*` must be overwritten at the edge, with `rateLimit.trustedProxies` set to
the proxy address so the audit trail and the failed-login lockout key on the real client
rather than on the proxy. Kerberos SPNEGO additionally needs upstream keep-alive disabled,
because the handshake authenticates a connection rather than a request.

**First-run login.** A strong one-time superadmin password is generated per install,
printed to the log, and saved to `INITIAL_ADMIN_LOGIN.txt` in the data dir. You must
change it on first sign-in, after which a short **setup wizard** walks you through
registering your first relying app. To set the password yourself instead, either fill in
`localAuth.password` in `config.json` or export `LOCAL_ADMIN_PASSWORD` before the first
start.

**Locked out?** Create an empty file named `RESET_ADMIN` in the data dir (next to
`config.json`) and restart. The bootstrap superadmin password is force-reset and
announced again in the log + recovery file. The marker is consumed on start, so it never
re-runs. The Windows installer offers this as a tick-box on a reinstall.

## Configure

Edit `config.json`:

| Setting | Why it matters |
| --- | --- |
| `sso.audience` | Comma-separated list of relying apps allowed to consume MyIDSan's tokens. The stock list covers the suite (`myidsan,mymatasan,myseliasan,myiotsan`). |
| `sso.internalToken` | Guards the internal token-introspection endpoint. Set a long random value if a relying app cannot share the session cache. |
| `login.google` / `login.github` | Optional social login; needs internet egress. Secrets can come from `GOOGLE_CLIENT_SECRET` / `GITHUB_CLIENT_SECRET`. |
| `login.oidc[]` | Optional generic OIDC providers (Keycloak, Authentik, ADFS, Entra…). Secrets can come from `OIDC_<KEY>_CLIENT_SECRET`. |
| `kerberos` | Optional SPNEGO single sign-on for domain-joined machines — see the runbook below. |
| `passwordPolicy` | Password-strength rules for every human-chosen password. `minLength` defaults to `12` (was a hard-coded 8). `requireUpper`/`requireLower`/`requireDigit`/`requireSymbol` default off. `blockCommon` (embedded denylist) defaults on. Leave the block out of `config.json` entirely to take all the defaults. |
| `mfa` | MFA enforcement policy. `policy`: `off` \| `optional` (default) \| `required`. `requiredRoleIds`: narrow `required` to specific roles; empty = every role. `applyToDirectory`: also require it for LDAP-bound accounts (default `false`). See "MFA-required rollout" below before setting `policy` to `required`. |
| `db` | Defaults to SQLite (`./data/myidsan.db`), fine for a single identity server. Postgres and MariaDB are also supported. |

**LDAP / Active Directory** login is configured in the UI (Federation → Directory), not
in `config.json` — the bind password is stored encrypted at rest.

**MFA-required rollout.** Switching `mfa.policy` to `required` does not lock anyone out
immediately: a user with no confirmed factor still signs in, but is pinned to the
enrollment screen on every request until they add one — the same pattern as the forced
first-login password change. The risk is operational, not a hard lockout: if your *only*
superadmin cannot complete enrollment right now (lost phone, no authenticator app handy)
they are stuck on that screen until they can. Before flipping the switch on a live
install: confirm at least one administrator has already enrolled a factor, or is present
and able to enroll immediately after the restart that picks up the config change. The
`RESET_MFA` marker file (data dir, mirrors `RESET_ADMIN`) recovers a sole superadmin who
gets stuck anyway — see "Locked out?" above.

**Per-account login lockout tradeoff.** The failed-login lockout now keys on both the
connecting IP address and the submitted account identifier (previously IP-only), so a
password spray spread across many source addresses against one account is throttled too.
The tradeoff: anyone who knows a username can now lock that account out on purpose by
repeatedly guessing its password from anywhere. This is a deliberate choice — a locked
account recovers on its own once the lockout window passes (or immediately on a
successful sign-in), whereas the unthrottled alternative let an attacker guess a known
account's password without limit. If this becomes a nuisance for a specific deployment,
`loginSecurity.maxAttempts`/`windowSeconds`/`lockoutSeconds` tune how forgiving it is.

## Ports

| Port | Proto | Direction | Purpose |
| --- | --- | --- | --- |
| 3001 | TCP/TLS | inbound | Web UI + API + SSO endpoints (users' browsers redirect here) |

## Kerberos SPNEGO SSO (optional)

Zero-prompt sign-in for domain-joined machines. Set `kerberos.enabled`, point
`keytabPath` at a keytab exported for `servicePrincipal`, then:

1. **Register the SPN + export the keytab.**
   - Active Directory: `setspn -S HTTP/myidsan.corp.local svc-myidsan`, then
     `ktpass -princ HTTP/myidsan.corp.local@CORP.LOCAL -mapuser svc-myidsan -pass * -crypto AES256-SHA1 -ptype KRB5_NT_PRINCIPAL -out myidsan.keytab`.
   - Samba AD: `samba-tool spn add HTTP/myidsan.corp.local svc-myidsan`, then
     `samba-tool domain exportkeytab myidsan.keytab --principal=HTTP/myidsan.corp.local`.
2. Copy the keytab to the host (perms `0600`, readable by the service user) and set
   `keytabPath` to it. `servicePrincipal` must match the SPN exactly.
3. **Clock skew < 5 minutes** between the host, the KDC and the client (Kerberos hard
   requirement).
4. **Browser trust** — browsers only send the Negotiate ticket to trusted sites:
   - Edge/Chrome: add the FQDN to the Local Intranet zone, or set the
     `AuthNegotiateDelegateAllowlist` / `AuthServerAllowlist` policy.
   - Firefox: add it to `network.negotiate-auth.trusted-uris`.
   - Use the **FQDN** in the URL bar, not an IP.

A misconfiguration never breaks password login — the SSO button just fails back to the
login page. Check the service log, the `myidsan_federated_login_total{result=...}` metric
(`KRB_AP_ERR_MODIFIED` → wrong SPN/keytab; `KRB_AP_ERR_SKEW` → clock skew), and the
**Audit log** — a rejected ticket (a forged/replayed token, or a realm outside
`kerberos.onlyRealms`) now writes a `login.failure` entry there too, not just the metric;
the no-token challenge that starts every SPNEGO handshake deliberately does not appear in
the audit trail.

## Running as a service

Use the matching file in `deploy/`:

- **Linux** — `deploy/myidsan.service` (systemd). The `.deb`/`.rpm` packages install and
  enable this for you under a dedicated `myidsan` user.
- **Windows** — `deploy/myidsan.winsw.xml` (WinSW or NSSM). The `myidsan-setup-*.exe`
  installer registers the service for you; this file is only for the portable `.zip`.
- **macOS** — `deploy/com.mysayasan.myidsan.plist` (launchd).

All of them set `KOPIV2_SUPERVISED=1`, so an in-app restart exits cleanly and the
supervisor relaunches it.

## Backup and disaster recovery

myidsan is the one component whose loss takes the whole suite down at once: its database
holds **every user and password hash, every role and permission, every registered SSO
client, the SSO certificate authority's private key, all two-factor secrets, and the LDAP
bind password.** Every relying app authenticates against it.

### Taking a backup

**Backup & restore** in the side nav (superadmin only) produces a single encrypted
`.idbackup` file. Choose which sections to include, set a passphrase of at least 12
characters, and download it.

The passphrase is the **only** thing protecting the file — it contains password hashes,
TOTP secrets and a CA private key in a form that a restore can use. Store the file and the
passphrase separately, and treat the file with the same care as the database itself. There
is no recovery path if the passphrase is lost.

Export and restore both require a **recent re-authentication** ("step-up"): re-enter the
password (plus a TOTP code, if you have one enrolled) at the prompt before the action —
`POST /api/step-up` with `{password, code}` — then proceed within its 5-minute window. This
closes the gap where a stolen session cookie alone could export or replace the whole
identity store.

The same thing from the API:

```bash
# Step up first (skip if your session already re-authenticated in the last 5 minutes):
curl -sS -X POST https://myidsan.example.com/api/step-up \
  -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -b cookies.txt \
  -d '{"password":"'"$PASSWORD"'","code":"'"$TOTP_CODE"'"}'

curl -sS -X POST https://myidsan.example.com/api/backup/export \
  -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -b cookies.txt \
  -d '{"passphrase":"'"$PASSPHRASE"'"}' \
  | jq -r .result.dataBase64 | base64 -d > myidsan-backup.idbackup
```

Schedule that from cron or a scheduled task and keep the output off the myidsan host.
`TOTP_CODE` can be omitted (or left empty) for a scripted account that has no MFA factor
enrolled.

### What is and is not in the file

Included: accounts and groups, roles and permissions, two-factor enrolments and recovery
codes, registered apps with their client configuration and redirect URIs, the directory
configuration and its group→role mappings, and the SSO CA.

Deliberately excluded: `config.json`, TLS certificates, `secret/atrest.key`, the API and
runtime logs, pending password-reset requests, live sessions, and the **audit log**. Those
are host-local — copying them onto a second machine would clone that machine's identity
rather than restore its data. The audit log's exclusion is also what makes it useful as a
record of the restore itself: a restore drops every session and rewrites every account, but
the entry it writes for having done so is not touched by the operation it describes. See
"Audit trail" below for its own export path.

The at-rest key not being included is the important one, and it is not a gap. The two
sealed values (TOTP secrets, the LDAP bind password) are **unsealed when the backup is
written and re-sealed with the destination host's own key when it is restored.** That is
what lets a backup restore onto a brand-new machine and still have everyone's authenticator
app work. Carrying the sealed bytes instead would produce a file that restores without
error and then fails every second-factor check.

### Restoring onto a rebuilt server

1. Install myidsan on the new host and start it. It generates a fresh TLS certificate,
   at-rest key and one-time superadmin password as usual.
2. Sign in as that superadmin and change the password when prompted.
3. On the setup wizard's first screen choose **Restore from a backup instead**; or, if
   setup was already completed, go to **Backup & restore**.
4. Upload the `.idbackup`, enter the passphrase, and press **Open and inspect** — this only
   reads the manifest and writes nothing, so you can confirm the version and contents first.
5. Choose **Replace what is here** (the correct choice when rebuilding) and restore.

Everyone is signed out, including you: the restore drops every live session, because a
session issued a moment earlier would still carry pre-restore authority. Sign in again with
an account from the backup — its password, role and second factor are exactly as they were.

`config.json` is not restored, so re-apply any host settings by hand: listener ports, TLS
paths, SMTP, Kerberos and any `login.oidc[]` providers. A superadmin-only `GET`/`PUT
/api/settings/{section}` API now covers a safe subset of that file (`localAuth`, `sso`,
`security`, `storage`, `logging`; each save still requires a restart) — but there is no
Settings *page* for it yet, so today it is reachable only by direct API call, not from the
UI.

### Verify a backup before you need it

A backup nobody has restored is a hypothesis. At least once, restore into a throwaway
instance and confirm you can sign in with an account that has two-factor enabled — that
single check exercises the password hash, the role assignment and the secret re-sealing
together.

## Audit trail

The **Audit log** page (superadmin only) is an append-only record of every security-relevant
event on this server — sign-ins and failures (including refused federated/SSO sign-ins, not
just local/LDAP ones), lockouts, MFA changes, account and directory changes, password-reset
resolutions, session revocations, backup export/restore, and step-up attempts. There is no
update route for it and no way to delete a chosen entry: nothing in the product, including a
superadmin session, can edit or selectively remove what is already written.

The one removal path is **age-based retention**, off by default and reachable only from
`config.json` — it is deliberately not an in-product control. Add an `audit` block to turn
it on:

```json
{
  "audit": {
    "retention": {
      "enabled": true,
      "maxRetentionDays": 365,
      "frequencyHours": 24,
      "archiveDir": "audit-archive"
    }
  }
}
```

`maxRetentionDays` has a hard 30-day floor (anything lower is raised, with a startup
warning); expired rows are archived to a JSON-lines file under `archiveDir`
(data-directory-relative) and fsynced/renamed into place **before** they are deleted from
the table, so a failed run costs a retention cycle, never history. Every purge records
itself back into the trail it just trimmed, naming the cutoff, row count and archive file.
The archive files are plaintext (0600, not sealed) and hold emails, IPs and User-Agent
strings — back that directory up with the same care as the database. See `docs/HOWTO.md`'s
"MyIDSan Audit Log Retention" section.

For compliance review or offline retention, export it as CSV rather than screen-scraping:

```bash
curl -sS "https://myidsan.example.com/api/audit/export.csv?from=2026-07-01&to=2026-07-31" \
  -H "X-CSRF-Token: $CSRF" -b cookies.txt \
  -o myidsan-audit-2026-07.csv
```

The export is capped at 50,000 rows per request — narrow the `from`/`to` range for a busy
server rather than expecting one file to hold everything. Values that could be misread as a
spreadsheet formula (a failed-login "username" or a User-Agent starting with `=`/`+`/`-`/`@`)
are quoted before export, so opening the CSV in Excel or Sheets is safe.

The audit table is **not** part of an `.idbackup` archive and is never cleared by a restore —
it is host-local history, the same category as the API/runtime logs, and it is what lets you
confirm afterwards that a restore actually happened and who ran it.

## Upgrading

- **deb/rpm** — `apt install ./myidsan_*.deb` / `dnf install ./myidsan-*.rpm` over the
  top. Data under `/opt/myidsan` is preserved.
- **Portable archive** — stop the service, replace the binary and `static/`, restart.
  Keep your `config.json`, `data/`, `certs/` and `secret/`.
- **Docker** — pull the new tag; keep the `/data` volume.
