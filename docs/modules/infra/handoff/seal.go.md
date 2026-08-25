# Module: infra/handoff

## Purpose

Seal a small payload so that exactly **one named recipient** can open it — and nothing in
between can, including the process that carries it.

It exists for N+1 node failover (W3-7). Standing a spare recorder up to record another
recorder's cameras means moving that recorder's **camera credentials** to the spare, and the
only party that can arrange the move is the control plane, which talks to both. A plain
"export my cameras" endpoint relayed through myseliasan would be a **bulk credential dump**:
every camera login on a recorder, in one JSON body, readable by anything that can call the
endpoint and resident in the control plane's memory on the way past.

Stated honestly, this does **not** make credentials secret from the appliances — both ends
hold them because both have to log in to the camera. What it does:

1. the handoff endpoint emits ciphertext only, so calling it yields nothing usable;
2. the control plane never holds a plaintext camera password — it relays an envelope it has
   no key for;
3. the envelope is **bound to the recipient**, so a bundle captured in flight cannot be
   staged onto a different appliance.

## Files & responsibilities

- `seal.go`
  - `NewRecipient() (*Recipient, error)` — mints an ephemeral X25519 key pair. Ephemeral is
    the point: it lives for one exchange and is never persisted, so there is no long-lived
    key to steal and a bundle captured today cannot be opened a month from now.
  - `Recipient.PublicKey() []byte` — the 32-byte public half. Not a secret and carries no
    authority: knowing it lets you seal a bundle **to** this recipient, never open one.
  - `Seal(recipientPub, aad, plaintext) ([]byte, error)` — X25519 ECDH to a **fresh
    per-bundle** ephemeral sender key, HKDF-SHA256 (info `kopiv2-handoff-v1`) to the content
    key, AES-256-GCM to encrypt. `aad` is authenticated but not encrypted; the failover path
    puts the recipient's node id there.
  - `Recipient.Open(sealed, aad) ([]byte, error)` — the reverse. `aad` must match byte for
    byte.
  - `ErrMalformed` vs `ErrNotForYou` — deliberately distinct. The first means "this is not a
    handoff bundle" (too short, wrong version byte); the second means "this bundle was not
    meant for you, or it was altered". Same event to AES-GCM, different thing to whoever
    reads the log line.

## Wire format

`version(1) || ephemeralPublicKey(32) || nonce(12) || AES-256-GCM(ciphertext||tag)`

The header travels in the clear and is fed to GCM as additional data **together with the
caller's `aad`**, length-prefixed so moving bytes between the two cannot produce the same
authenticated string. Without the header in the AAD an attacker could substitute an
ephemeral public key they control and have the recipient derive a key they know.

`deriveKey` mixes **both** public keys into the HKDF salt alongside the raw shared secret:
the bare X25519 output is the same for several `(ephemeral, recipient)` pairings an attacker
can contrive, and binding the transcript is what makes the content key specific to this
exchange.

## Bounds

- `maxPayload` = 8 MiB on `Open` — generous for a staged camera set (a few KiB per camera)
  and still refuses to let a hostile sender make the recipient allocate arbitrarily.
- `formatVersion` is the first byte so a future construction can coexist rather than being
  told apart by length.

## Tests (`seal_test.go`)

The round trip is the least interesting assertion, and is deliberately not the only one — a
"sealing" function that returned its input unchanged passes a round-trip test perfectly.
So the suite also asserts the ciphertext does **not** contain the plaintext password or the
camera address, that a different recipient cannot open the bundle, that mismatched `aad`
fails, that a bit flipped anywhere fails, that a substituted ephemeral key fails, and that
two seals of the same plaintext differ (fresh key and nonce per call — otherwise the
ciphertext itself would leak that a staged camera set had not changed between two syncs).

Mutation-checked: removing the `aad` from the authenticated string makes
`TestOpenRefusesMismatchedAAD` fail with a useful message.

## Dependencies

Standard library only — `crypto/ecdh`, `crypto/hkdf`, `crypto/aes`, `crypto/cipher`,
`crypto/rand`, `crypto/sha256`. No new module dependency, nothing to configure, no key to
manage.

## Used by

- `apps/mymatasan/services/standby.go` — mints the recipient (on the spare) and seals the
  camera set (on the protected recorder).
- `apps/myseliasan/services/failover.go` — relays the sealed blob and cannot read it.
