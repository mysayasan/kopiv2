# Running MyIDSan behind a reverse proxy

Nearly every real deployment terminates TLS upstream. This directory has working
starting points:

| File | Proxy |
|---|---|
| [`nginx-myidsan.conf`](nginx-myidsan.conf) | nginx |
| [`Caddyfile.myidsan`](Caddyfile.myidsan) | Caddy |

They are written for MyIDSan, but the `X-Forwarded-*` and public-URL rules below apply
to any of the four apps put behind a proxy. What makes the identity provider stricter is
that its public URL is part of a protocol other applications have already been configured
against, so changing it breaks them rather than just looking wrong.

## The app-side half

```jsonc
{
  "server": {
    "hostnames": ["*"],
    "enableTLS": false,
    "nonTlsPorts": [3001],
    "tlsPorts": []
  }
}
```

Bind to loopback (or a private interface) so the only route in is the proxy. If the app
is reachable directly on `:3001`, every rule below can be bypassed by talking to it
instead of to the proxy — the headers are only trustworthy because a client cannot set
them.

## The public URL must not drift

**Registered redirect URIs are matched exactly.** `https://sso.example.com/callback`,
`https://sso.example.com:443/callback`, and `https://sso.example.com/callback/` are three
different strings, and two of them will be rejected.

The practical consequence is that the proxy must forward the hostname the *client* used,
not the upstream's:

- nginx: `proxy_set_header Host $host;` — **not** `$proxy_host`, which is the upstream
  and would make the app generate links pointing at `127.0.0.1`.
- Caddy: `header_up Host {host}`.

Once relying apps are registered against a public URL, changing it — adding a port,
moving from a path prefix to a subdomain, switching hostnames — invalidates every
registered redirect URI at once, and every relying app fails at the *callback*, after the
user has already signed in. Treat the public URL as part of the contract, not a
deployment detail.

Self-service password-reset links are built from the same reconstruction (`requestOrigin`
in `apps/myidsan/apis/login.go`), so a wrong `Host` also sends users a link to the wrong
place.

## `X-Forwarded-*` is a claim, not a fact

These headers are ordinary request headers. Anything a client sends under those names
arrives looking exactly like something a proxy added. The proxy's job is to **overwrite**
them, never to append to whatever came in:

- nginx: `proxy_set_header` assigns, so the sample configs already discard client input.
  `$proxy_add_x_forwarded_for` deliberately appends the *peer* address to the inbound
  chain — that is correct for a chain of proxies you control, and it is why the app takes
  the right-most untrusted entry rather than the left-most.
- Caddy replaces these by default. Do **not** add `trusted_proxies` at the outermost
  edge: that directive makes Caddy start *trusting* inbound `X-Forwarded-*`, which is the
  opposite of what an internet-facing hop should do. It belongs only on an inner Caddy
  that really is behind another proxy.

### Tell the app which hops to trust

Client IP resolution is allow-listed. Set `rateLimit.trustedProxies` to the proxy's
address, or the app treats the proxy itself as the client — and then a per-IP lockout
counts every user in the building as one address:

```jsonc
{
  "rateLimit": {
    "trustedProxies": ["127.0.0.1", "10.0.0.0/8"]
  }
}
```

This list feeds `middlewares.ClientIP`, which is what the audit trail, the failed-login
lockout and the rate limiter all record and key on. Leaving it empty behind a proxy is
the single most common way to make the lockout either useless or indiscriminate.

### Known inconsistency

`middlewares.IsSecureRequest` (`domain/utils/middlewares/auth.go`) honours
`X-Forwarded-Proto: https` from **any** caller — its comment says "trusted upstream proxy"
but there is no trust check, unlike `ClientIP`, which does consult the allow-list above.

Assessed impact, so this is neither ignored nor over-read: the check is
`r.TLS != nil || XFP == https`, so the header can only make the app consider a request
*more* secure, never less. A genuine HTTPS request cannot be downgraded. What it selects
is the cookie **name and `Secure` flag**, so a caller who asserts the header over
cleartext causes the app to set `Secure` cookies that their own browser then refuses to
store. That is a denial of service against oneself, not a privilege escalation or a
session-fixation vector.

It still deserves fixing for consistency, and the fix is to thread the same trusted-proxy
allow-list into `IsSecureRequest` (five call sites across the shared middleware, myidsan
and myseliasan). Until then: **strip `X-Forwarded-Proto` at the edge**, which the sample
configs do, and do not expose the app's plain-HTTP port directly.

## Kerberos SPNEGO needs connection affinity

SPNEGO authenticates a **connection**, not a request. If the proxy multiplexes requests
from different clients onto one pooled upstream connection, the handshake completed for
one user can be attributed to another's request, and sign-in fails intermittently in a
way that looks random.

If `kerberos.enabled` is true, disable upstream keep-alive — the sample configs carry the
exact directives, commented out. Leave them commented when Kerberos is off; connection
reuse is worth having.

## Air-gapped installs

Caddy's automatic HTTPS needs to reach an ACME CA. On an isolated network use an explicit
certificate or Caddy's internal CA:

```
sso.example.internal {
	tls /etc/ssl/certs/sso.crt /etc/ssl/private/sso.key
	# or, for a local CA whose root you distribute to clients:
	# tls internal
	reverse_proxy 127.0.0.1:3001
}
```

## Checking it worked

After starting the proxy, confirm the app sees what you think it does:

1. Sign in through the public URL. If the browser lands on `127.0.0.1` at any point, the
   `Host` header is wrong.
2. Open **Audit log** and look at a `login.success` row's client IP. If it shows the
   proxy's address rather than yours, `rateLimit.trustedProxies` is not set.
3. Check the boot log for the `deployment:` line. Behind a load balancer with more than
   one instance it must **not** say `SINGLE-INSTANCE ONLY` — see
   [`../README-myidsan.md`](../README-myidsan.md).
