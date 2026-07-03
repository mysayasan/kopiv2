//go:build !windows && !linux

package atrest

import "fmt"

// platformDefaultProtectorName: platforms without a wired host-bound keystore (e.g. macOS
// builds of this app, *BSD) default "auto" to the plaintext file protector. Keychain
// support can be added here later as its own protector.
func platformDefaultProtectorName() string { return protectorFile }

func newPlatformProtector(name string, _ ProtectorConfig) (KeyProtector, error) {
	return nil, fmt.Errorf("atrest: key protector %q is not supported on this platform (use file or passphrase)", name)
}
