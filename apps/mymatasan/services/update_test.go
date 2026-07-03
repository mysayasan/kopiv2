package services

import "testing"

func TestVersionGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.75.0", "1.74.0", true},
		{"1.74.1", "1.74.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.74.0", "1.74.0", false},
		{"1.73.0", "1.74.0", false},
		{"v1.75.0", "v1.74.0", true}, // leading v tolerated
		{"bad", "1.0.0", false},      // non-semver never "greater"
		{"1.0.0", "bad", false},
	}
	for _, c := range cases {
		if got := versionGreater(c.a, c.b); got != c.want {
			t.Errorf("versionGreater(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
