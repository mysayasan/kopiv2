// Package fleetnode is the NODE side of the fleet: how an appliance discovers a control
// plane, gets adopted by it, enrolls for an mTLS certificate, holds open a control channel,
// and pushes its events up.
//
// It lives here because it is not camera-specific and never was. mymatasan grew it first, and
// myiotsan needs exactly the same thing — so this is extracted rather than copied. A second
// copy of a pairing/enrollment stack is a second copy of a security protocol, and the two
// would drift the first time one of them was fixed.
//
// What stays per-app is only what genuinely differs: the app's NAME and its KIND (a camera
// node or a sensor node), which the node reports so the control plane knows what it adopted.
package fleetnode

import "strings"

// isNoResultFoundErr reports the repo's "row not present" sentinel, which it signals by
// message rather than by a typed error.
func isNoResultFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no result found") || strings.Contains(msg, "total affected: 0")
}
