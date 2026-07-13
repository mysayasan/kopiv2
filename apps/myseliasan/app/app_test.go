package app

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
)

func TestModuleReadinessStatusAdvisoryFields(t *testing.T) {
	m := &module{}
	// No fleet listeners wired yet → no advisory fields (readiness stays db+cache only).
	if got := m.ReadinessStatus(context.Background()); len(got) != 0 {
		t.Fatalf("bare module should report no advisory fields, got %v", got)
	}

	// A never-run control server reports down / 0; a started media flag reports up.
	m.controlServer = services.NewControlServer(nil, 0, nil, nil)
	var media atomic.Bool
	media.Store(true)
	m.mediaListening = &media

	got := m.ReadinessStatus(context.Background())
	if got["controlChannel"] != "down" {
		t.Fatalf("controlChannel = %q, want down", got["controlChannel"])
	}
	if got["connectedNodes"] != "0" {
		t.Fatalf("connectedNodes = %q, want 0", got["connectedNodes"])
	}
	if got["mediaRelay"] != "up" {
		t.Fatalf("mediaRelay = %q, want up", got["mediaRelay"])
	}
}

func TestMyseliasanSharedAPIsOnlyExposeVersion(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if !cfg.Version {
		t.Fatalf("expected version API to remain enabled: %+v", cfg)
	}
	if cfg.ApiLog || cfg.AppRegistry || cfg.ApiEndpoint || cfg.FileStorage || cfg.CacheService || cfg.RuntimeLog {
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
