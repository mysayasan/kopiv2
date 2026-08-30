package vision

import "testing"

// TestFaceThresholdNotStricterThanWorker pins the relationship that was broken: ONE decision — "is
// this the enrolled person?" — must not be made twice against different numbers.
//
// The worker attaches a name at cosine >= 0.40 (MYMATASAN_FACE_MIN_COS in yolo_worker.py) and SFace's
// documented same-identity point is ~0.36. This side said 0.60, so every genuine match the worker had
// already named in the 0.40-0.60 band was thrown away here: no alert at all on a "known" rule, and —
// worse — an enrolled person reported as a STRANGER on an "unknown" rule.
//
// If somebody raises this constant again, this test is where they find out what it costs.
const workerFaceNameFloor = 0.40

func TestFaceThresholdNotStricterThanWorker(t *testing.T) {
	if defaultMinFaceConfidence > workerFaceNameFloor {
		t.Fatalf("defaultMinFaceConfidence = %v is stricter than the worker's naming floor (%v): "+
			"every match between them is named by the worker and discarded here",
			defaultMinFaceConfidence, workerFaceNameFloor)
	}
}

// TestFaceMatchModes covers the three questions a face rule can ask, against the candidate shape the
// worker actually emits. The unknown-mode case is the one with teeth: a recognized person must never
// be reported as a stranger.
func TestFaceMatchModes(t *testing.T) {
	candidate := func(name string, id float64, confidence float64) ObjectCandidate {
		return ObjectCandidate{
			Label:      "face",
			Confidence: 0.9,
			Box:        Box{X: 0.4, Y: 0.4, W: 0.2, H: 0.2},
			Metadata: map[string]any{
				"personName": name,
				"personId":   id,
				"confidence": confidence,
			},
		}
	}
	// A face the worker named, at a similarity typical of a live view of somebody enrolled from a
	// passport photo. It sat in the discarded band before the floors were aligned.
	alice := candidate("Alice", 7, 0.52)
	stranger := candidate("", 0, 0.0)

	det := &ObjectRuleDetector{}
	cases := []struct {
		name       string
		ruleConfig string
		candidates []ObjectCandidate
		wantMatch  bool
		wantPerson string
		wantKnown  bool
	}{
		{"known fires on an enrolled person", `{"matchMode":"known"}`, []ObjectCandidate{alice}, true, "Alice", true},
		{"known ignores a stranger", `{"matchMode":"known"}`, []ObjectCandidate{stranger}, false, "", false},
		{"unknown fires on a stranger", `{"matchMode":"unknown"}`, []ObjectCandidate{stranger}, true, "", false},
		{"unknown must NOT fire on an enrolled person", `{"matchMode":"unknown"}`, []ObjectCandidate{alice}, false, "", false},
		{"include fires on a listed person", `{"matchMode":"include","people":["alice"]}`, []ObjectCandidate{alice}, true, "Alice", true},
		{"include ignores an unlisted person", `{"matchMode":"include","people":["bob"]}`, []ObjectCandidate{alice}, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := DetectionRule{DetectionType: "face", RuleConfig: tc.ruleConfig}
			cfg, err := parseFaceConfig(rule)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, match, matched := det.faceMatch(rule, cfg, tc.candidates)
			if matched != tc.wantMatch {
				t.Fatalf("matched = %v, want %v", matched, tc.wantMatch)
			}
			if !matched {
				return
			}
			if match.PersonName != tc.wantPerson {
				t.Errorf("person = %q, want %q", match.PersonName, tc.wantPerson)
			}
			if match.Recognized != tc.wantKnown {
				t.Errorf("recognized = %v, want %v", match.Recognized, tc.wantKnown)
			}
		})
	}
}

// TestFaceRuleWithNoZoneCoversTheWholeFrame pins what an empty zonePolygon means. The People screen
// creates face rules without a zone on purpose — a doorway camera watches all of the doorway — and
// an empty zone that matched NOTHING would make every one of those rules silently inert.
func TestFaceRuleWithNoZoneCoversTheWholeFrame(t *testing.T) {
	det := &ObjectRuleDetector{}
	rule := DetectionRule{DetectionType: "face", RuleConfig: `{"matchMode":"known"}`, ZonePolygon: ""}
	cfg, err := parseFaceConfig(rule)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, corner := range []Box{
		{X: 0.02, Y: 0.02, W: 0.06, H: 0.06},
		{X: 0.90, Y: 0.90, W: 0.06, H: 0.06},
		{X: 0.45, Y: 0.45, W: 0.10, H: 0.10},
	} {
		c := ObjectCandidate{Label: "face", Confidence: 0.9, Box: corner,
			Metadata: map[string]any{"personName": "Alice", "personId": 7.0, "confidence": 0.71}}
		if _, _, matched := det.faceMatch(rule, cfg, []ObjectCandidate{c}); !matched {
			t.Errorf("a face at %v was not matched by a zone-less rule", corner)
		}
	}
}
