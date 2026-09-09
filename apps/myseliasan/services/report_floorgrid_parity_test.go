package services

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The floor model is drawn by FOUR renderers in TWO languages: the editor canvas, the read-only
// floor view and the 3D scene (all JS, and all now reading one declaration in
// views/react-webpack/src/views/components/map/plan_objects.js), plus THIS package's PDF
// compositor, in Go.
//
// Go cannot share the JS registry, and pretending otherwise would be worse than the problem. What
// it can do is fail loudly the moment the two drift — which is the failure that actually happens
// here: someone adds an object type to the editor, every JS surface picks it up for free, and the
// PDF silently stops showing geometry the operator drew. Nothing errors; a report is just quietly
// missing the roads.
//
// So this test reads the JS registry as data and asserts the Go struct keeps up.

// planObjectsPath locates the registry relative to THIS source file, so the test does not depend on
// the working directory `go test` happens to be run from.
func planObjectsPath(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	// apps/myseliasan/services/ -> apps/myseliasan/views/react-webpack/src/views/components/map/
	return filepath.Join(filepath.Dir(self), "..", "views", "react-webpack", "src", "views",
		"components", "map", "plan_objects.js")
}

// jsArrayNames pulls every `array: '<name>'` out of the registry. A regex rather than a JS parser
// on purpose: the alternative is a Node dependency in a Go test, and the declaration is a fixed,
// single-line shape that the registry's own comments describe as THE identifier.
var arrayDeclRe = regexp.MustCompile(`(?m)^\s*array:\s*'([a-zA-Z0-9_]+)'`)

func jsArrayNames(t *testing.T) []string {
	t.Helper()
	path := planObjectsPath(t)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	matches := arrayDeclRe.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatalf("no `array:` declarations found in %s - has the registry's shape changed? "+
			"This test reads it as data; update the pattern deliberately, do not delete the test.", path)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// goGridJSONKeys returns the json tag of every field on floorGrid.
func goGridJSONKeys() map[string]bool {
	out := map[string]bool{}
	rt := reflect.TypeOf(floorGrid{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		out[strings.Split(tag, ",")[0]] = true
	}
	return out
}

// TestFloorGridCoversEveryRegistryArray is the parity gate. Adding a type to the JS registry
// without teaching this package about it fails here, with the name of the array that was missed.
func TestFloorGridCoversEveryRegistryArray(t *testing.T) {
	goKeys := goGridJSONKeys()
	var missing []string
	for _, name := range jsArrayNames(t) {
		if !goKeys[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the plan object registry declares %v, which floorGrid does not carry.\n"+
			"A new object type reaches every JS surface for free but NOT the PDF report, which then\n"+
			"silently omits geometry the operator drew. Add the field to floorGrid and draw it in\n"+
			"renderFloorGrid, or - if it is deliberately not printed - add it to floorGrid with a\n"+
			"comment saying so, which is what makes the decision visible.", missing)
	}
}

// TestFloorGridParsesARegistryShapedModel guards the other direction: a model written in the shape
// the registry describes must round-trip into floorGrid. It is the cheap check that the json tags
// are not merely PRESENT but actually match what the editor writes.
func TestFloorGridParsesARegistryShapedModel(t *testing.T) {
	model := map[string]any{"version": 2, "unit": 43.0}
	for _, name := range jsArrayNames(t) {
		model[name] = []any{}
	}
	// An unrecognised key, exactly as a NEWER editor would write it. Go must ignore it rather than
	// fail the whole report - the same forward-compatibility the JS reader provides.
	model["somethingFromTheFuture"] = []any{map[string]any{"x": 1}}

	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var g floorGrid
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("floorGrid rejected a registry-shaped model: %v", err)
	}
	if g.Version != 2 || g.Unit != 43 {
		t.Fatalf("floorGrid lost its metadata: version=%d unit=%v", g.Version, g.Unit)
	}
}

// TestRenderFloorGridSurvivesUnknownKeys is the belt to the braces above: the compositor must not
// panic or bail on a model carrying arrays it has never heard of. A future editor WILL write one.
func TestRenderFloorGridSurvivesUnknownKeys(t *testing.T) {
	raw := `{"version":3,"unit":43,"segments":[{"x1":10,"y1":10,"x2":100,"y2":10}],` +
		`"roads":[{"pts":[{"x":1,"y":2}],"width":6}],"trees":[{"x":5,"y":5,"canopy":4}]}`
	dst := image.NewRGBA(image.Rect(0, 0, 200, 200))
	// The assertion is simply that this returns: a panic here is a broken PDF for every customer
	// running an older control plane against a newer plan.
	renderFloorGrid(dst, raw, 0.0116)
}
