package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawConfig is representative of a real app config: the blocks the callers edit, plus
// blocks they must never touch (db, server) written with INLINE arrays, so a test can
// prove untouched bytes survive verbatim.
const rawConfig = `{
  "jwt": {
    "secret": "0123456789abcdef0123"
  },
  "localAuth": {
    "enabled": true,
    "username": "admin",
    "password": "admin123"
  },
  "rateLimit": {
    "enabled": true,
    "endpointCacheTtlSeconds": 30,
    "devOnly": {
      "enabled": true,
      "requests": 300
    }
  },
  "allowOrigins": "*",
  "server": {
    "hostnames": ["*"],
    "tlsPorts": [3002],
    "nonTlsPorts": []
  },
  "db": {
    "engine": "postgres",
    "host": "localhost",
    "port": 5433
  }
}`

func TestPatchBytesPreservesUntouchedBlocksAndOrder(t *testing.T) {
	out, err := PatchBytes([]byte(rawConfig), []Patch{
		{Path: []string{"localAuth", "password"}, Value: "newsecret"},
		{Path: []string{"rateLimit", "devOnly", "requests"}, Value: float64(500)},
		{Path: []string{"allowOrigins"}, Value: "https://example.test"},
	})
	if err != nil {
		t.Fatalf("PatchBytes: %v", err)
	}

	if got, want := TopLevelKeyOrder(out), TopLevelKeyOrder([]byte(rawConfig)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("top-level order changed:\n got %v\nwant %v", got, want)
	}
	for _, verbatim := range []string{`"hostnames": ["*"]`, `"tlsPorts": [3002]`, `"nonTlsPorts": []`, `"engine": "postgres"`} {
		if !strings.Contains(string(out), verbatim) {
			t.Fatalf("untouched formatting lost: %q not found in\n%s", verbatim, out)
		}
	}

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got := leaf(t, parsed, "localAuth", "password"); got != "newsecret" {
		t.Fatalf("localAuth.password = %v", got)
	}
	if got := leaf(t, parsed, "rateLimit", "devOnly", "requests"); got != float64(500) {
		t.Fatalf("rateLimit.devOnly.requests = %v", got)
	}
	if got := leaf(t, parsed, "allowOrigins"); got != "https://example.test" {
		t.Fatalf("allowOrigins = %v", got)
	}
	// A sibling inside a patched block, and a whole unpatched block, must survive.
	if got := leaf(t, parsed, "rateLimit", "endpointCacheTtlSeconds"); got != float64(30) {
		t.Fatalf("unedited sibling lost: %v", got)
	}
	if got := leaf(t, parsed, "db", "port"); got != float64(5433) {
		t.Fatalf("unpatched db block mutated: %v", got)
	}
	if got := leaf(t, parsed, "localAuth", "username"); got != "admin" {
		t.Fatalf("unedited sibling in patched block lost: %v", got)
	}
}

// A key the document does not have yet must be created, not dropped: the setup wizard
// stamps "setup": {"completed": true} into configs that ship without the block.
func TestPatchBytesCreatesMissingBlock(t *testing.T) {
	out, err := PatchBytes([]byte(rawConfig), []Patch{
		{Path: []string{"setup", "completed"}, Value: true},
		{Path: []string{"db", "ssl_mode"}, Value: "disable"},
	})
	if err != nil {
		t.Fatalf("PatchBytes: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got := leaf(t, parsed, "setup", "completed"); got != true {
		t.Fatalf("setup.completed = %v", got)
	}
	// A new leaf in an existing block must not discard that block's other keys.
	if got := leaf(t, parsed, "db", "host"); got != "localhost" {
		t.Fatalf("db.host lost when adding db.ssl_mode: %v", got)
	}
}

func TestMaterializeNoPatchesIsNoop(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := Materialize(missing, nil); err != nil {
		t.Fatalf("empty patch set should not touch the file: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("empty patch set created a file")
	}
}

func TestMaterializeWritesAtomicallyAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(rawConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Materialize(path, []Patch{{Path: []string{"db", "host"}, Value: "db.internal"}}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid JSON after write: %v", err)
	}
	if got := leaf(t, parsed, "db", "host"); got != "db.internal" {
		t.Fatalf("db.host = %v", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestFlattenProducesLeafPatches(t *testing.T) {
	patches := Flatten(nil, map[string]any{
		"cache": map[string]any{"provider": "redis", "redis": map[string]any{"db": 2}},
		"top":   "value",
	})
	got := map[string]any{}
	for _, p := range patches {
		got[strings.Join(p.Path, ".")] = p.Value
	}
	want := map[string]any{"cache.provider": "redis", "cache.redis.db": 2, "top": "value"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func leaf(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	var cur any = doc
	for _, seg := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not an object", path, seg)
		}
		cur = obj[seg]
	}
	return cur
}
