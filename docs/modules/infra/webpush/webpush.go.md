# Module: infra/webpush/webpush.go

## Purpose

Send one Web Push message: encrypt it to a browser's subscription (RFC 8291), sign an
assertion identifying this install (RFC 8292), POST it to the push service, and say what
happened. Standard library only — no dependency, and nothing in here reaches a network except
`Send`.

## The four answers, and why they are four

The point of this package is a distinction a single `error` cannot make:

| Outcome | What it means | What an operator should do |
|---|---|---|
| `delivered` | The push service accepted the message. | Nothing. |
| `gone` | 404/410 — this subscription will never work again. | Nothing; the caller deletes the row. |
| `rejected` | A service answered and refused (bad token, rate limit, clock skew). | Fix something here. |
| `unreachable` | Nothing answered at all. | Usually nothing: this is an intranet install. |

`unreachable` is the one this package exists for. Reporting it as a refusal sends an operator
on an air-gapped site hunting a fault in a product that is working exactly as designed.

`Send` returns a meaningful `Result` **even when it also returns an error** — `err` is for a
caller that cannot be bothered to look, `Result` for one that can.

## `classifyTransport`, and the defect the live bench found

The two context errors are not the same fact, and treating them alike cost this package its
most important answer:

- **`context.Canceled`** — the CALLER gave up (browser disconnected, process shutting down).
  Nothing was learned about the network, so claiming no egress would be an invention →
  `rejected`.
- **`context.DeadlineExceeded`** — OUR OWN allowance expired with no response. That is what
  "could not be reached" means → `unreachable`.

The first version returned `rejected` for both. The unit test that drove it used an
**already-cancelled** context, so it never exercised a real timeout and stayed green; the W3-9
bench sent a real message to an address nothing answers on and the appliance reported that the
push service had refused it. Both halves now have their own test, and the fix is
mutation-checked in both directions.

## The encryption, and how it is verified

`aes128gcm`, one record, no padding beyond the `0x02` delimiter. Two-stage HKDF per RFC 8291:

```
prkKey = HKDF-Extract(sha256, salt=authSecret, ikm=ECDH(as, ua))
ikm    = HKDF-Expand(prkKey, "WebPush: info\0" || uaPublic || asPublic, 32)
prk    = HKDF-Extract(sha256, salt=randomSalt, ikm)
cek    = HKDF-Expand(prk, "Content-Encoding: aes128gcm\0", 16)
nonce  = HKDF-Expand(prk, "Content-Encoding: nonce\0", 12)
```

The header is positional and a browser parses it by offset: `salt(16) || recordSize(4, BE) ||
keyLen(1)=65 || asPublic(65)`. Getting any offset wrong produces something that decrypts to
nothing, with no error anyone can read.

**`encryptWith` exists so the RFC's published §5 worked example can be reproduced byte for
byte** — it takes the salt and sender key as parameters instead of generating them. A
round-trip against a decryptor written in the same sitting would prove only that two readings
of the spec agree with each other, which is also true of two matching misreadings that no
browser would accept. The vector is the only evidence available here that this interoperates.

Salt and sender key are fresh per MESSAGE (`encrypt`); reusing them would reuse a content key
and nonce, the one thing AES-GCM must never do. There is a test for that too.

## VAPID

`vapidToken` builds an ES256 JWT with a **raw `r||s` 64-byte signature** — not the ASN.1
encoding `ecdsa.SignASN1` produces, which is accepted by nothing. The audience is the
**ORIGIN** of the endpoint, never the full URL, whose path identifies one device; getting this
wrong fails at exactly one vendor rather than all of them, which is the worst way to be wrong.

`GenerateKeys` mints the identity; `PublicKeyOf` recovers the public half from a stored private
key, so a caller can persist one value.

## Limits

`maxPayloadBytes` is 4096 minus the header and the GCM tag. Oversize is refused HERE, with the
size named, rather than by a vendor returning 413 without saying which notification was too
long.

## Not claimed

- **No retry, no queue, no batching.** One call, one message. Scheduling belongs to the caller,
  which is the only thing that knows what a duplicate buzz costs.
- **No delivery guarantee.** `delivered` means a push service ACCEPTED the message. What
  happens between there and a phone is not observable from here and is not claimed.
- **No subscription management.** This package neither stores nor validates who owns what; see
  `apps/myseliasan/services/push.go`.
