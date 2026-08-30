package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestFaceConfidenceDefaultsAgree pins one number that lives in two files and must not drift.
//
// THE FAILURE IT PREVENTS ALREADY HAPPENED ONCE. The face-match floor — below which a face is
// treated as unknown rather than risk naming the WRONG person — is `defaultMinFaceConfidence` in
// infra/vision/face.go. The frontend has its own copy because the People screen and the rules editor
// WRITE an explicit `minConfidence` into every rule they save, and an explicit value beats a default:
// the server's number never applies to a rule that carries its own. So when the server floor was
// lowered to the model's real same-identity point (after measuring that the old one discarded
// genuine matches and reported enrolled people as strangers), every rule the UI had already written
// still pinned the old value and the fix reached nothing anybody had created.
//
// Two numbers, one decision, and the UI's copy silently wins. This test reads both files so a change
// to either fails the build here rather than in somebody's lobby.
func TestFaceConfidenceDefaultsAgree(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	goSrc := readSourceFile(t, filepath.Join(repo, "infra", "vision", "face.go"))
	jsSrc := readSourceFile(t, filepath.Join(repo, "apps", "mymatasan", "views", "react-webpack",
		"src", "views", "lib", "helpers.js"))

	goVal := extractFloat(t, goSrc, `defaultMinFaceConfidence\s*=\s*([0-9.]+)`,
		"defaultMinFaceConfidence in infra/vision/face.go")
	jsVal := extractFloat(t, jsSrc, `FACE_MIN_CONFIDENCE\s*=\s*([0-9.]+)`,
		"FACE_MIN_CONFIDENCE in the frontend helpers")

	if goVal != jsVal {
		t.Fatalf("the face confidence floor disagrees: infra/vision/face.go says %v, the frontend "+
			"writes %v into every rule it saves. The frontend's value wins, because a rule that "+
			"carries an explicit minConfidence never sees the server default.", goVal, jsVal)
	}

	// A floor of 0 would mean "name anybody who looks vaguely similar", and 1 would mean "never name
	// anyone". Neither is a value somebody would choose on purpose; both are what a bad edit leaves.
	if goVal <= 0 || goVal >= 1 {
		t.Fatalf("face confidence floor %v is outside the useful range (0,1)", goVal)
	}
}

func readSourceFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func extractFloat(t *testing.T, src, pattern, what string) float64 {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if len(m) < 2 {
		t.Fatalf("could not find %s — was it renamed? This test is the only thing keeping the two "+
			"copies of this number in step.", what)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("%s is not a number: %q", what, m[1])
	}
	return v
}
