package app

import "testing"

func TestMymatasanSharedAPIsDisablePolicyManagement(t *testing.T) {
	cfg := New().(*module).SharedAPIs()
	if cfg.AppRegistry || cfg.ApiEndpoint || cfg.ApiEndpointRbac {
		t.Fatalf("expected mymatasan policy-management shared APIs to be disabled: %+v", cfg)
	}
	if !cfg.Version || !cfg.FileStorage || !cfg.CacheService || !cfg.ApiLog || !cfg.RuntimeLog {
		t.Fatalf("expected mymatasan operational shared APIs to remain enabled: %+v", cfg)
	}
}
