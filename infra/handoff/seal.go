// Package handoff seals a small payload so that exactly ONE named recipient can open it,
// and nothing in between can — including the process that carries it.
//
// It exists for node failover (W3-7). Standing a spare appliance up to record another
// appliance's cameras means moving that appliance's camera credentials to the spare, and
// the only party that can arrange the move is the control plane, which talks to both. A
// plain "export the cameras" endpoint would therefore be a BULK CREDENTIAL DUMP: every
// camera login on a recorder, in one JSON body, readable by anything that can call the
// endpoint, and resident in the control plane's memory on the way past.
//
// What this changes, stated honestly: it does NOT make the credentials secret from the
// appliances. Both ends hold them, because both ends have to log in to the camera. What it
// does is (a) stop the handoff endpoint from being readable by whoever calls it, (b) keep
// plaintext credentials out of the control plane entirely — it relays an envelope it has no
// key for — and (c) BIND the envelope to the recipient, so a bundle captured in flight
// cannot be replayed onto a different node.
//
// The construction is deliberately boring and entirely standard library: X25519 ECDH to an
// ephemeral sender key, HKDF-SHA256 to derive the content key, AES-256-GCM to encrypt, and
// the caller's associated data authenticated alongside. No new dependency, nothing to
// configure, and no key to manage: the recipient key pair lives for one exchange.
package handoff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// Format version, carried as the first byte so a future construction can coexist with
// this one rather than being told apart by length.
const formatVersion = 1

// hkdfInfo domain-separates this key derivation from every other use of HKDF in the
// product. Two protocols deriving from the same shared secret with the same info string
// is how a key ends up doing double duty.
const hkdfInfo = "kopiv2-handoff-v1"

const (
	keySize   = 32 // AES-256
	nonceSize = 12 // GCM standard nonce
	pubSize   = 32 // X25519 public key
)

// maxPayload bounds what Open will attempt to decrypt. A staged camera set is a few
// kilobytes per camera; this is generous by orders of magnitude and still refuses to let a
// hostile sender make the recipient allocate arbitrarily.
const maxPayload = 8 << 20

// ErrMalformed is returned for a sealed blob that is not this format at all — too short,
// wrong version byte. Distinguished from an authentication failure on purpose: one means
// "this is not a handoff bundle", the other means "this bundle was not meant for you, or
// it was altered", and an operator reading a log line deserves to know which.
var ErrMalformed = errors.New("not a handoff bundle")

// ErrNotForYou is returned when the bundle is well-formed but will not open with this
// recipient's key, or its associated data does not match what the caller expects. Both are
// the same event as far as AES-GCM is concerned, and both mean the same thing to the
// operator: this envelope is not yours.
var ErrNotForYou = errors.New("this bundle was sealed for a different recipient, or has been altered")

// Recipient is one ephemeral key pair, held by the party that will OPEN the bundle.
//
// Ephemeral is the point. The recipient mints a pair, publishes the public half for this
// one exchange, and drops it when the exchange is over. Nothing is persisted, so a bundle
// captured today cannot be opened by anything a month from now — there is no long-lived
// key to steal, and no key rotation to get wrong.
type Recipient struct {
	priv *ecdh.PrivateKey
}

// NewRecipient mints a fresh recipient key pair.
func NewRecipient() (*Recipient, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Recipient{priv: priv}, nil
}

// PublicKey is the 32-byte X25519 public key to hand to the sender. It is not a secret and
// carries no authority: knowing it lets you seal a bundle TO this recipient, never open one.
func (r *Recipient) PublicKey() []byte {
	if r == nil || r.priv == nil {
		return nil
	}
	return r.priv.PublicKey().Bytes()
}

// Seal encrypts plaintext so only the holder of recipientPub's private half can read it.
//
// aad is authenticated but NOT encrypted, and it is what binds the bundle to its purpose:
// the failover path puts the RECIPIENT NODE ID in it, so a bundle intercepted on its way to
// node B cannot be staged onto node C. Whatever the sender puts here, the opener must
// supply byte-for-byte or the open fails.
func Seal(recipientPub []byte, aad []byte, plaintext []byte) ([]byte, error) {
	if len(recipientPub) != pubSize {
		return nil, fmt.Errorf("recipient public key must be %d bytes, got %d", pubSize, len(recipientPub))
	}
	pub, err := ecdh.X25519().NewPublicKey(recipientPub)
	if err != nil {
		return nil, fmt.Errorf("recipient public key is not a valid X25519 point: %w", err)
	}
	// A fresh sender key per bundle. Reusing one would make two bundles to the same
	// recipient share a content key, and then the nonce is the only thing standing between
	// them — which is exactly the mistake this construction is boring in order to avoid.
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(pub)
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(shared, eph.PublicKey().Bytes(), recipientPub)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// The header — version, ephemeral public key, nonce — travels in the clear and is fed
	// to GCM as additional data along with the caller's aad, so an attacker cannot swap the
	// ephemeral key for one they control and have the recipient derive a key they know.
	header := make([]byte, 0, 1+pubSize+nonceSize)
	header = append(header, formatVersion)
	header = append(header, eph.PublicKey().Bytes()...)
	header = append(header, nonce...)

	out := make([]byte, 0, len(header)+len(plaintext)+gcm.Overhead())
	out = append(out, header...)
	return gcm.Seal(out, nonce, plaintext, bindAAD(header, aad)), nil
}

// Open reverses Seal. aad must be exactly what the sender sealed with.
func (r *Recipient) Open(sealed []byte, aad []byte) ([]byte, error) {
	if r == nil || r.priv == nil {
		return nil, ErrMalformed
	}
	const headerLen = 1 + pubSize + nonceSize
	if len(sealed) < headerLen+16 || len(sealed) > maxPayload {
		return nil, ErrMalformed
	}
	if sealed[0] != formatVersion {
		return nil, ErrMalformed
	}
	header := sealed[:headerLen]
	ephPubBytes := sealed[1 : 1+pubSize]
	nonce := sealed[1+pubSize : headerLen]
	body := sealed[headerLen:]

	ephPub, err := ecdh.X25519().NewPublicKey(ephPubBytes)
	if err != nil {
		return nil, ErrMalformed
	}
	shared, err := r.priv.ECDH(ephPub)
	if err != nil {
		return nil, ErrNotForYou
	}
	key, err := deriveKey(shared, ephPubBytes, r.priv.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, body, bindAAD(header, aad))
	if err != nil {
		return nil, ErrNotForYou
	}
	return plain, nil
}

// deriveKey mixes BOTH public keys into the derivation alongside the shared secret. The
// raw X25519 output is the same for several (ephemeral, recipient) pairings an attacker can
// contrive; binding the transcript is what makes the content key specific to this exchange.
func deriveKey(shared, ephPub, recipientPub []byte) ([]byte, error) {
	salt := make([]byte, 0, len(ephPub)+len(recipientPub))
	salt = append(salt, ephPub...)
	salt = append(salt, recipientPub...)
	return hkdf.Key(sha256.New, shared, salt, hkdfInfo, keySize)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// bindAAD concatenates the wire header with the caller's associated data. A length prefix
// separates them so that moving bytes from one into the other cannot produce the same
// authenticated string — without it, a caller whose aad happened to start with the header's
// last byte would authenticate a different split than it thinks.
func bindAAD(header, aad []byte) []byte {
	out := make([]byte, 0, len(header)+8+len(aad))
	out = append(out, header...)
	out = append(out, byte(len(aad)>>24), byte(len(aad)>>16), byte(len(aad)>>8), byte(len(aad)))
	out = append(out, aad...)
	return out
}
