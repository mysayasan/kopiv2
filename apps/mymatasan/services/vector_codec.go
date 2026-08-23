package services

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/mysayasan/kopiv2/infra/atrest"
)

// Storage codec for embedding vectors, shared by the face gallery and by appearance search.
//
// Both features store fixed-length float32 vectors and both need the same three properties,
// so they use one implementation rather than two that drift:
//
//   - PORTABLE. base64 TEXT persists identically on sqlite, MariaDB and Postgres. Matching
//     never happens in SQL — every consumer loads vectors and compares them in memory — so
//     the database's only job is durable, engine-agnostic storage.
//   - ENCRYPTED AT REST. The float bytes pass through infra/atrest before encoding, so a
//     stolen database file yields neither biometric templates nor a searchable index of who
//     was where. Deleting the owning row crypto-shreds the vector with it.
//   - LITTLE-ENDIAN, EXPLICITLY. Not Go's native order, which would make a database written
//     on one architecture decode to noise on another.

// encodeVectorAtRest turns a vector into the stored form: float32 little-endian bytes,
// encrypted when a cipher is configured, then base64.
func encodeVectorAtRest(cipher *atrest.Cipher, vec []float32) (string, error) {
	buf := make([]byte, len(vec)*4)
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	if cipher != nil {
		enc, err := cipher.EncryptBytes(buf)
		if err != nil {
			return "", err
		}
		buf = enc
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// decodeVectorAtRest reverses encodeVectorAtRest.
func decodeVectorAtRest(cipher *atrest.Cipher, enc string) ([]float32, error) {
	buf, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if cipher != nil {
		dec, err := cipher.DecryptBytes(buf)
		if err != nil {
			return nil, err
		}
		buf = dec
	}
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("corrupt embedding length %d", len(buf))
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return vec, nil
}

// cosineSimilarity scores two vectors in [-1, 1].
//
// It normalises rather than assuming unit length. The producers do normalise — but a vector
// that has been through storage, a model swap or a hand-written test is not guaranteed to,
// and an unnormalised pair silently returns a similarity above 1 that then sorts above every
// honest match. Length mismatch returns 0 rather than panicking or comparing the overlap:
// two different feature spaces have no meaningful similarity, and inventing one is how a
// model swap turns into confident nonsense instead of an empty result.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
