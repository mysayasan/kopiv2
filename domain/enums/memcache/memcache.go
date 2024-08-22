package memcache

import (
	"fmt"
)

type Address int

const (
	Mware_Rbac_GetApiEpByUserRole_Result Address = iota + 1 // EnumIndex = 1
)

func GetString(address Address) string {
	return fmt.Sprintf("%06d", address)
}
