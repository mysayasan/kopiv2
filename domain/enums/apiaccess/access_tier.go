package apiaccess

type AccessTier int32

const (
	DevOnly AccessTier = iota
	AuthOnly
	Public
)

func IsValidAccessTier(value int32) bool {
	switch AccessTier(value) {
	case DevOnly, AuthOnly, Public:
		return true
	default:
		return false
	}
}
