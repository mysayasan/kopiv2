//go:build windows

package atrest

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformDefaultProtectorName picks the native protector for "auto" on Windows: DPAPI,
// machine-scoped, so a service account can unwrap the key with no operator input.
func platformDefaultProtectorName() string { return protectorDPAPI }

func newPlatformProtector(name string, _ ProtectorConfig) (KeyProtector, error) {
	switch name {
	case protectorDPAPI:
		return dpapiProtector{}, nil
	default:
		return nil, fmt.Errorf("atrest: key protector %q is not supported on windows (use file, passphrase, or dpapi)", name)
	}
}

// dpapiProtector wraps the DEK with Windows DPAPI at machine scope (CryptProtectData). The
// KEK is derived by the OS from machine credentials, so the wrapped key is bound to this
// host: it survives reboots and service accounts but cannot be unwrapped on another
// machine. Moving the install therefore requires the passphrase escrow, not a file copy.
type dpapiProtector struct{}

func (dpapiProtector) Name() string { return protectorDPAPI }

func (dpapiProtector) Wrap(dek []byte) ([]byte, error) {
	return dpapiCrypt(dek, true)
}

func (dpapiProtector) Unwrap(blob []byte) ([]byte, error) {
	dek, err := dpapiCrypt(blob, false)
	if err != nil {
		return nil, fmt.Errorf("atrest: DPAPI unwrap failed (key file is bound to a different machine or user?): %w", err)
	}
	if len(dek) != KeySize {
		return nil, fmt.Errorf("atrest: unwrapped key has wrong size")
	}
	return dek, nil
}

// dpapiCrypt calls CryptProtectData (protect=true) or CryptUnprotectData, both with UI
// forbidden so a headless service never blocks on a prompt. Protect uses machine scope.
func dpapiCrypt(data []byte, protect bool) ([]byte, error) {
	var in windows.DataBlob
	if len(data) > 0 {
		in.Size = uint32(len(data))
		in.Data = &data[0]
	}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil,
			windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil,
			windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	res := make([]byte, out.Size)
	if out.Size > 0 {
		copy(res, unsafe.Slice(out.Data, out.Size))
	}
	return res, nil
}
