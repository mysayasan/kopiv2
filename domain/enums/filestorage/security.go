package filestorage

type SecurityLevel int32

const (
	SystemOnly SecurityLevel = iota
	Group
	Role
	Public
)

func IsValidSecurityLevel(value int32) bool {
	switch SecurityLevel(value) {
	case SystemOnly, Group, Role, Public:
		return true
	default:
		return false
	}
}
