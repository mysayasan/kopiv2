# Module: infra/control/client.go

## Purpose

Node-side (mymatasan) dialer that opens the control-channel connection to the parent.

## Responsibilities

- `Dial`: connect to the parent's `wss://host:port/control` endpoint, presenting the node's mTLS material via `tlsCfg` and verifying the parent's server cert through that config's `VerifyPeerCertificate`.
- Return a ready `*Conn` on success, or the dial error (closing the response body) on failure.

## Notes

- The node always initiates the connection; see [frame.go.md](frame.go.md) for why the dial direction is node→parent.
- Parent identity verification (expected CN) is configured in the supplied `tls.Config`, built by the `fleetca` package.
