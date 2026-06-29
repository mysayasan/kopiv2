# Module: infra/control/control_test.go

## Purpose

End-to-end tests for the control channel over a real fleet CA, exercising mTLS identity and frame transport.

## Responsibilities

- `TestControlChannelMTLSHelloRoundTrip`: stand up a fleet CA, parent server, and node dial, then verify the node identity is taken from its verified client-cert CN, the parent's server cert is verified by CN, and a Hello frame crosses intact.
- `TestControlChannelRejectsWrongParentCN`: confirm the node refuses a server whose cert CN is not the expected parent (different parent / MITM).
- Provide helpers `freeAddr` (reserve a free localhost port) and `waitDial` (block until the server accepts connections).

## Notes

- Uses `infra/fleetca` to issue the CA, parent server leaf, and node client leaf (from a node-generated CSR), mirroring the real adoption trust path.
