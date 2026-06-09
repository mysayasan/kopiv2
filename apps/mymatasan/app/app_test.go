package app

import "testing"

func TestMymatasanSharedAPIsExposeOnlyPublicVersion(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if !cfg.Version {
		t.Fatalf("expected mymatasan version API to remain enabled: %+v", cfg)
	}
	if cfg.AppRegistry || cfg.ApiEndpoint || cfg.ApiEndpointRbac || cfg.FileStorage || cfg.CacheService || cfg.ApiLog || cfg.RuntimeLog {
		t.Fatalf("expected mymatasan shared APIs that require Auth/RBAC to be disabled: %+v", cfg)
	}
}
