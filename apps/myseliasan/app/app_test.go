package app

import (
	"reflect"
	"testing"
)

func TestMyseliasanSharedAPIsOnlyExposeVersion(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if !cfg.Version {
		t.Fatalf("expected version API to remain enabled: %+v", cfg)
	}
	if cfg.ApiLog || cfg.AppRegistry || cfg.ApiEndpoint || cfg.ApiEndpointRbac || cfg.FileStorage || cfg.CacheService || cfg.RuntimeLog {
		t.Fatalf("expected myseliasan shared management APIs to be disabled: %+v", cfg)
	}
}

func typeName(value any) string {
	typ := reflect.TypeOf(value)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Name()
}

func TestMyseliasanEntitiesAvoidUserManagementTables(t *testing.T) {
	entities := New().(*module).Entities()
	for _, entity := range entities {
		switch entity.(type) {
		case struct{}:
			continue
		default:
			name := typeName(entity)
			if name == "UserLogin" || name == "UserRole" || name == "UserGroup" {
				t.Fatalf("myseliasan must not register user-management entity %s", name)
			}
		}
	}
}
