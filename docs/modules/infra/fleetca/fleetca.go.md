# Module: infra/fleetca/fleetca.go

## Purpose

A tiny on-prem certificate authority (CA) used by the `myseliasan` control plane to issue per-node and control-plane client certificates for the pairing mTLS channel. It is self-contained and intentionally avoids CRL/OCSP: certificates are short-lived and renewed over the existing mTLS channel; revocation is enforced by refusing to re-issue for a revoked node ID (see `apps/myseliasan/services/fleet_ca.go`).

## Responsibilities

- `NewCA(commonName, validFor)` — create a fresh self-signed ECDSA P-256 CA (10-year default at the control-plane layer); returns a `*CA` with PEM accessors.
- `LoadCA(certPEM, keyPEM)` — reconstruct a `*CA` from previously persisted PEM bytes.
- `CA.IssueFromCSR(csrPEM, cn, validFor, serial)` — sign a leaf certificate for `cn` using the public key in `csrPEM`. The CSR's own subject is ignored; the CA sets the CN authoritatively so a node cannot pick its own identity.
- `CA.IssueLeaf(cn, validFor, serial)` — generate a fresh ECDSA P-256 key and sign a leaf for `cn` in one step. Used for the control plane's own client certificate.
- `GenerateKeyAndCSR(cn)` — generate a private key + CSR for `cn`. Called by the node side so the private key never leaves the node (only the CSR is sent to the CA).
- `ServerTLSConfig(certPEM, keyPEM, caPEM)` — build a `tls.Config` for the node's mTLS management listener: presents `certPEM`/`keyPEM` as the server cert and requires a client certificate signed by `caPEM` (`RequireAndVerifyClientCert`).
- `ClientTLSConfig(certPEM, keyPEM, caPEM, expectServerCN)` — build a `tls.Config` for the control plane dialing a node: presents `certPEM`/`keyPEM` as the client cert, verifies the peer's leaf chains to `caPEM`, and asserts the server cert CN equals `expectServerCN`. Standard hostname/SAN check is disabled (`InsecureSkipVerify: true`) on purpose; the manual `VerifyPeerCertificate` hook enforces CA chain trust and CN identity (appliances are dialed by IP, not by DNS name).
- `PeerCommonName(state)` — extract the CN of the verified peer certificate from a TLS connection state. Node handlers use this to confirm caller identity.

## Identity Model

Identity lives in the certificate Common Name (the node ID or parent ID), not in DNS/IP Subject Alternative Names. This is a deliberate choice: appliances are dialed by IP address, and SAN-based hostname checks would be brittle in a LAN environment where IPs may change. The chain verification + CN check in `verifyChainCN` provides equivalent trust for this use case.

## Revocation Model

No CRL or OCSP is implemented. Revocation is handled at the issuance layer: the control-plane service (`fleet_ca.go`) maintains a denylist of revoked node IDs and refuses to re-sign CSRs from revoked nodes. Since certificates have a short TTL (default 7 days), a revoked node's existing cert will age out naturally.

## Notes

- All keys use ECDSA P-256; RSA is not supported.
- Leaf certificates include both `ExtKeyUsageServerAuth` and `ExtKeyUsageClientAuth` so the same per-node cert can serve both roles.
- Serials are 128-bit random integers (`randSerial`); a `nil` serial passed to `signLeaf` causes a fresh one to be generated.
- Keys are serialised as PKCS#8 PEM (`PRIVATE KEY` block).
