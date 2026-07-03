//go:build linux

package atrest

import (
	"bytes"
	"fmt"
	"os/exec"
)

// systemdCredsName is bound into the ciphertext by systemd-creds; decrypt must use the same.
const systemdCredsName = "kopiv2-atrest"

// platformDefaultProtectorName picks the native protector for "auto" on Linux. Bare-metal
// hosts with systemd get host-bound systemd-creds (TPM2-backed when present); everything
// else (notably containers, where systemd-creds is absent) falls back to the plaintext
// file protector, which is why Docker deployments should set keyProtector=passphrase.
func platformDefaultProtectorName() string {
	if systemdCredsAvailable() {
		return protectorSystemd
	}
	return protectorFile
}

func newPlatformProtector(name string, _ ProtectorConfig) (KeyProtector, error) {
	switch name {
	case protectorSystemd:
		if !systemdCredsAvailable() {
			return nil, fmt.Errorf("atrest: systemd-creds not found on PATH (needs systemd >= 250, and a host key or TPM2)")
		}
		return systemdCredsProtector{}, nil
	default:
		return nil, fmt.Errorf("atrest: key protector %q is not supported on linux (use file, passphrase, or systemd-creds)", name)
	}
}

func systemdCredsAvailable() bool {
	_, err := exec.LookPath("systemd-creds")
	return err == nil
}

// systemdCredsProtector wraps the DEK by shelling out to `systemd-creds encrypt`, binding
// the ciphertext to this host (host key and/or TPM2). Like DPAPI it is machine-bound, so
// moving the install relies on the passphrase escrow rather than copying the key file.
type systemdCredsProtector struct{}

func (systemdCredsProtector) Name() string { return protectorSystemd }

func (systemdCredsProtector) Wrap(dek []byte) ([]byte, error) {
	out, err := runSystemdCreds("encrypt", dek)
	if err != nil {
		return nil, fmt.Errorf("atrest: systemd-creds encrypt failed: %w", err)
	}
	return out, nil
}

func (systemdCredsProtector) Unwrap(blob []byte) ([]byte, error) {
	dek, err := runSystemdCreds("decrypt", blob)
	if err != nil {
		return nil, fmt.Errorf("atrest: systemd-creds decrypt failed (key bound to a different host?): %w", err)
	}
	if len(dek) != KeySize {
		return nil, fmt.Errorf("atrest: unwrapped key has wrong size")
	}
	return dek, nil
}

// runSystemdCreds pipes in via stdin and reads the result from stdout, using "-" for both
// paths so nothing transits the filesystem. --name binds/checks the credential name.
func runSystemdCreds(mode string, in []byte) ([]byte, error) {
	cmd := exec.Command("systemd-creds", "--name="+systemdCredsName, mode, "-", "-")
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
