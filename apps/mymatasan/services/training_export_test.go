package services

import (
	"strings"
	"testing"
)

func TestBuildDataYAMLListsClasses(t *testing.T) {
	yaml := buildDataYAML([]string{"courier", "van"}, ".")
	for _, want := range []string{"train: images/train", "val: images/val", "nc: 2", "- courier", "- van"} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("data.yaml missing %q:\n%s", want, yaml)
		}
	}
}

func TestUnionClassesDedupesAndLowercases(t *testing.T) {
	got := unionClasses(`["person","Courier"]`, []string{"courier", "VAN", "person"})
	want := []string{"person", "courier", "van"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unionClasses = %v, want %v", got, want)
	}
}

func TestTrainingClassNamesDropsUnusedDeclared(t *testing.T) {
	// "family" is declared but never labeled, so it must NOT become a model class.
	declared := []string{"family", "papa"}
	used := map[string]bool{"papa": true, "oven": true, "bottle": true}
	got := trainingClassNames(declared, used)
	want := []string{"papa", "bottle", "oven"} // declared-used first, then extras sorted
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("trainingClassNames = %v, want %v", got, want)
	}
}

func TestCappedBackgroundCount(t *testing.T) {
	cases := []struct {
		labeled, backgrounds, want int
	}{
		{100, 30, 15}, // capped to 15% of labeled
		{100, 5, 5},   // under the cap → keep all
		{0, 0, 0},     // nothing
		{3, 4, 1},     // tiny dataset → at least one background survives
		{100, 0, 0},   // no backgrounds → none
	}
	for _, c := range cases {
		if got := cappedBackgroundCount(c.labeled, c.backgrounds); got != c.want {
			t.Fatalf("cappedBackgroundCount(%d,%d) = %d, want %d", c.labeled, c.backgrounds, got, c.want)
		}
	}
}

func TestParseAnnotationsJSONDropsDegenerate(t *testing.T) {
	in := `[{"className":"Courier","x":0.1,"y":0.1,"w":0.2,"h":0.2},{"className":"x","x":0,"y":0,"w":0,"h":0.1},{"className":"","x":0,"y":0,"w":0.3,"h":0.3}]`
	out := parseAnnotationsJSON(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 valid annotation, got %d (%#v)", len(out), out)
	}
	if out[0].ClassName != "courier" {
		t.Fatalf("className not lowercased: %q", out[0].ClassName)
	}
}
