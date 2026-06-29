# Module: infra/control/server.go

## Purpose

Parent-side (myseliasan) control-channel listener that accepts node-initiated WebSocket connections over fleet mTLS.

## Responsibilities

- Define `ConnectHandler`, invoked per accepted node with the node identity (cert CN) and the `*Conn`; it must block for the connection's lifetime (run the read loop).
- `NewServer`: build a `Server` bound to an address, presenting a `tls.Config` that must require and verify the node client cert against the fleet CA.
- `Run`: serve `/control` over TLS until the context is cancelled, returning nil on clean shutdown.
- `handle`: derive the node identity from the verified client cert via `fleetca.PeerCommonName`, reject connections without one, upgrade to WebSocket, and hand off to the `ConnectHandler`.

## Notes

- mTLS is the authentication — only a node holding a fleet-CA-signed cert can connect — so the WebSocket `Origin` check is disabled.
- The server cert/key live in `TLSConfig.Certificates`, so `ListenAndServeTLS` is called with empty file arguments.
