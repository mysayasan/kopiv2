package vision

import "testing"

func TestParseZonesLegacySinglePolygon(t *testing.T) {
	zones := parseZones(`[[0,0],[0.5,0],[0.5,1],[0,1]]`)
	if len(zones) != 1 {
		t.Fatalf("legacy single polygon: want 1 zone, got %d", len(zones))
	}
	if len(zones[0]) != 4 {
		t.Fatalf("want 4 points, got %d", len(zones[0]))
	}
	// A point in the left half is inside; the right half is outside.
	if !pointInAnyZone(0.25, 0.5, zones) {
		t.Errorf("expected (0.25,0.5) inside the left-half zone")
	}
	if pointInAnyZone(0.75, 0.5, zones) {
		t.Errorf("expected (0.75,0.5) outside the left-half zone")
	}
}

func TestParseZonesMultiPolygonUnion(t *testing.T) {
	// Two disjoint zones: left strip and right strip. A point in either is a hit,
	// a point in the middle gap is not.
	zones := parseZones(`[[[0,0],[0.3,0],[0.3,1],[0,1]],[[0.7,0],[1,0],[1,1],[0.7,1]]]`)
	if len(zones) != 2 {
		t.Fatalf("multi polygon: want 2 zones, got %d", len(zones))
	}
	if !pointInAnyZone(0.15, 0.5, zones) {
		t.Errorf("expected (0.15,0.5) inside the left zone")
	}
	if !pointInAnyZone(0.85, 0.5, zones) {
		t.Errorf("expected (0.85,0.5) inside the right zone")
	}
	if pointInAnyZone(0.5, 0.5, zones) {
		t.Errorf("expected (0.5,0.5) in the gap to be outside both zones")
	}
}

func TestParseZonesEmptyAndInvalidFallBackToFullFrame(t *testing.T) {
	for _, value := range []string{"", "   ", "[]", "not json", `[[0,0]]`} {
		zones := parseZones(value)
		if len(zones) != 1 {
			t.Fatalf("value %q: want full-frame fallback (1 zone), got %d", value, len(zones))
		}
		if !pointInAnyZone(0.5, 0.5, zones) {
			t.Errorf("value %q: full-frame fallback should contain the center", value)
		}
	}
}

func TestParseZonesDropsDegeneratePolygons(t *testing.T) {
	// The second polygon has only two points and must be dropped, leaving one zone.
	zones := parseZones(`[[[0,0],[0.4,0],[0.4,0.4],[0,0.4]],[[0.6,0.6],[0.9,0.6]]]`)
	if len(zones) != 1 {
		t.Fatalf("want 1 valid zone after dropping the degenerate one, got %d", len(zones))
	}
}
