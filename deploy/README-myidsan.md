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
| `db` | Defaults to SQLite (`./data/myidsan.db`), fine for a single identity server. Postgres and MariaDB are also supported. |

**LDAP / Active Directory** login is configured in the UI (Federation → Directory), not
in `config.json` — the bind password is stored encrypted at rest.

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
login page. Check the service log and the `myidsan_federated_login_total{result=...}`
metric (`KRB_AP_ERR_MODIFIED` → wrong SPN/keytab; `KRB_AP_ERR_SKEW` → clock skew).

## Running as a service

Use the matching file in `deploy/`:

- **Linux** — `deploy/myidsan.service` (systemd). The `.deb`/`.rpm` packages install and
  enable this for you under a dedicated `myidsan` user.
- **Windows** — `deploy/myidsan.winsw.xml` (WinSW or NSSM). The `myidsan-setup-*.exe`
  installer registers the service for you; this file is only for the portable `.zip`.
- **macOS** — `deploy/com.mysayasan.myidsan.plist` (launchd).

All of them set `KOPIV2_SUPERVISED=1`, so an in-app restart exits cleanly and the
supervisor relaunches it.

## Back up the database and `secret/atrest.key`

The database holds **every user, role and registered SSO client** in the suite — every
relying app authenticates against it. `secret/atrest.key` (in the data dir) encrypts the
LDAP directory **bind password** stored in it.

Back up the database and the key together, as a pair. Losing the database means
re-registering every relying app and re-creating every account.

## Upgrading

- **deb/rpm** — `apt install ./myidsan_*.deb` / `dnf install ./myidsan-*.rpm` over the
  top. Data under `/opt/myidsan` is preserved.
- **Portable archive** — stop the service, replace the binary and `static/`, restart.
  Keep your `config.json`, `data/`, `certs/` and `secret/`.
- **Docker** — pull the new tag; keep the `/data` volume.
