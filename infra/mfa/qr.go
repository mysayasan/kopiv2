package mfa

import qrcode "github.com/skip2/go-qrcode"

// QRPNG renders an otpauth:// provisioning URI as a PNG QR code, sized `px` pixels
// square. Rendering happens fully on-box (pure-Go encoder, no network) so an
// air-gapped install can still show a scannable code. The manual-entry base32
// secret is always offered alongside as a fallback.
func QRPNG(otpauthURI string, px int) ([]byte, error) {
	if px <= 0 {
		px = 256
	}
	// Medium recovery level: enough redundancy for a phone camera without bloating
	// the module count for a short otpauth URI.
	return qrcode.Encode(otpauthURI, qrcode.Medium, px)
}
