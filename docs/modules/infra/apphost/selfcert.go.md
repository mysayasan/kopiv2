# Module: infra/apphost/selfcert.go

## Purpose

Generates a self-signed TLS keypair on first boot when an HTTPS listener is configured but no certificate exists yet, so a freshly packaged install serves HTTPS immediately without manual cert setup.

## Responsibilities

- `ensureSelfSignedCert(certPath, keyPath, hostnames)` — no-op when either path is empty (left for `runServer` to error on as before), and no-op when both `certPath` and `keyPath` already exist (an operator's own cert, or a file dropped in by a reverse proxy setup, is never overwritten). Otherwise generates an ECDSA P-256 key, a 128-bit random serial, and a self-signed `x509.Certificate` valid from one minute ago for 10 years, with `DNSNames`/`IPAddresses` from `certSANs`, then writes PEM-encoded cert (`0o644`) and key (`0o600`) to the given paths, creating parent directories as needed.
- `certSANs(hostnames)` — builds the SAN lists: `localhost` + `127.0.0.1`/`::1` always included; each configured `server.hostnames` entry is added as a DNS name (or IP, if it parses as one), skipping empty/`*` wildcard entries; every non-loopback local interface IP is added so the cert validates when the appliance is reached by its LAN address.
- `fileExists(path)` — a plain (non-directory) file existence check used by `ensureSelfSignedCert`.

## Notes

- Called from `Run` (`run.go`) once, before starting listeners, only when at least one `buildListenerSpecs` result has `UseTLS`.
- Certificate subject: `CommonName: "mymatasan", Organization: ["MyMataSan"]` — the field is currently hardcoded to the `mymatasan` app; a second app packaging a TLS-enabled release would need this generalized.
- Browsers show a one-time "not trusted" warning for a self-signed LAN cert — this is expected; operators can supply a real cert or front the app with a TLS-terminating reverse proxy (see `deploy/README.md`).
- `KeyUsage`/`ExtKeyUsage` are set for standard TLS server auth (`DigitalSignature | KeyEncipherment`, `ExtKeyUsageServerAuth`), and `BasicConstraintsValid` is set (self-signed, not a CA).
