package vision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Rules about TIME, not about a frame (W3-4).
//
// Every rule before these answers a question a single frame can answer: is there a person,
// are there six of them, did this box cross that line. The three here cannot be answered by
// a frame at all:
//
//	loitering    someone has been in this area for half a minute
//	left_behind  that bag has been sitting there, and nobody is with it
//	direction    that vehicle is going the wrong way up the ramp
//
// They are evaluators over the tracker that already exists (track.go), not a new pipeline:
// the detector's candidates and the ByteTrack ids are the same ones line crossing uses, and
// no extra inference is run for any of them.
//
// THE THING THAT MAKES THEM DIFFERENT FROM EVERY OTHER RULE, and the thing to keep straight
// while reading this file: a frame rule can be wrong for one frame and right on the next,
// and the min-frames streak absorbs it. A time rule that is wrong for one frame LOSES ITS
// TIMER, and a thirty-second threshold then never fires at all on a real camera, where
// confidence dips and people walk behind pillars. Almost every decision below is about that.

const (
	// DetectionLoitering fires when a tracked object stays inside the zone for long enough.
	DetectionLoitering = "loitering"
	// DetectionLeftBehind fires when a tracked object stops moving and stays put, with
	// nobody beside it.
	DetectionLeftBehind = "left_behind"
	// DetectionDirection fires when a tracked object travels far enough in a heading the
	// rule cares about — the wrong-way rule.
	DetectionDirection = "direction"
)

const (
	defaultDwellSeconds       = 30
	defaultStillSeconds       = 60
	defaultDriftTolerance     = 0.05
	defaultPersonRadius       = 0.18
	defaultDirectionTolerance = 45.0
	defaultMinTravel          = 0.15
	defaultDwellMaxDistance   = 0.25
	// defaultDwellGraceSamples is how many consecutive detection passes may miss a track
	// before it is forgotten along with its timers.
	//
	// IN SAMPLES, NOT SECONDS. A track that vanishes for one pass and comes back as a NEW
	// track resets its dwell to zero, and a thirty-second rule then never fires on a camera
	// where anybody walks behind anything. Counting seconds instead would make the tolerance
	// depend on the camera's sampling interval — forgetting everything between samples on a
	// slow camera, and absorbing half a minute of real absence on a fast one. See
	// pruneTracks. Leaving the ZONE is a different event entirely and resets immediately.
	defaultDwellGraceSamples = 3
	// dwellMaxSeconds bounds the configurable thresholds. An hour is already far past what
	// anybody watches for, and the larger number is usually a misplaced millisecond.
	dwellMaxSeconds = 3600
)

func isDwellType(value string) bool {
	switch normalizedDetectionType(value) {
	case DetectionLoitering, DetectionLeftBehind, DetectionDirection:
		return true
	}
	return false
}

type dwellConfig struct {
	Classes           []string
	DwellSeconds      int
	StillSeconds      int
	DriftTolerance    float64
	RequireUnattended bool
	PersonRadius      float64
	Heading           float64 // degrees, 0 = up/north, clockwise
	HeadingSet        bool
	ToleranceDegrees  float64
	MinTravel         float64
	MaxTrackDistance  float64
	GraceSamples      int
}

type rawDwellConfig struct {
	Classes           []string `json:"classes"`
	DwellSeconds      int      `json:"dwellSeconds"`
	StillSeconds      int      `json:"stillSeconds"`
	DriftTolerance    float64  `json:"driftTolerance"`
	RequireUnattended *bool    `json:"requireUnattended"`
	PersonRadius      float64  `json:"personRadius"`
	Heading           string   `json:"heading"`
	ToleranceDegrees  float64  `json:"toleranceDegrees"`
	MinTravel         float64  `json:"minTravel"`
	MaxTrackDistance  float64  `json:"maxTrackDistance"`
	GraceSamples      *int     `json:"graceSamples"`
}

// headings maps the compass words the UI offers onto degrees, with 0 = up the frame.
//
// "North" means UP THE IMAGE, not magnetic north: the rule is drawn on a picture, and an
// operator setting "wrong way" on a corridor is thinking about the picture. Saying so here
// because the word invites the other reading.
var headings = map[string]float64{
	"up": 0, "north": 0,
	"right": 90, "east": 90,
	"down": 180, "south": 180,
	"left": 270, "west": 270,
}

func parseDwellConfig(rule DetectionRule) (dwellConfig, error) {
	var raw rawDwellConfig
	if strings.TrimSpace(rule.RuleConfig) != "" {
		if err := json.Unmarshal([]byte(rule.RuleConfig), &raw); err != nil {
			return dwellConfig{}, fmt.Errorf("ruleConfig must be valid JSON for %s: %w", rule.DetectionType, err)
		}
	}
	cfg := dwellConfig{
		Classes:           normalizeStringList(raw.Classes),
		DwellSeconds:      raw.DwellSeconds,
		StillSeconds:      raw.StillSeconds,
		DriftTolerance:    raw.DriftTolerance,
		RequireUnattended: true,
		PersonRadius:      raw.PersonRadius,
		ToleranceDegrees:  raw.ToleranceDegrees,
		MinTravel:         raw.MinTravel,
		MaxTrackDistance:  raw.MaxTrackDistance,
		GraceSamples:      defaultDwellGraceSamples,
	}
	if raw.GraceSamples != nil && *raw.GraceSamples >= 0 {
		cfg.GraceSamples = *raw.GraceSamples
	}
	if raw.RequireUnattended != nil {
		cfg.RequireUnattended = *raw.RequireUnattended
	}
	if cfg.DwellSeconds <= 0 {
		cfg.DwellSeconds = defaultDwellSeconds
	}
	if cfg.StillSeconds <= 0 {
		cfg.StillSeconds = defaultStillSeconds
	}
	if cfg.DriftTolerance <= 0 {
		cfg.DriftTolerance = defaultDriftTolerance
	}
	if cfg.PersonRadius <= 0 {
		cfg.PersonRadius = defaultPersonRadius
	}
	if cfg.ToleranceDegrees <= 0 {
		cfg.ToleranceDegrees = defaultDirectionTolerance
	}
	if cfg.MinTravel <= 0 {
		cfg.MinTravel = defaultMinTravel
	}
	if cfg.MaxTrackDistance <= 0 {
		cfg.MaxTrackDistance = defaultDwellMaxDistance
	}

	heading := strings.ToLower(strings.TrimSpace(raw.Heading))
	if heading != "" {
		if deg, ok := headings[heading]; ok {
			cfg.Heading, cfg.HeadingSet = deg, true
		} else {
			var deg float64
			if _, err := fmt.Sscanf(heading, "%g", &deg); err != nil {
				return dwellConfig{}, fmt.Errorf("ruleConfig.heading must be one of up/down/left/right (or north/south/east/west) or a number of degrees, got %q", raw.Heading)
			}
			cfg.Heading, cfg.HeadingSet = math.Mod(deg+360, 360), true
		}
	}
	return cfg, nil
}

// validateDwellRule refuses a rule that could never fire.
//
// The class list is REQUIRED, which is the important one. `ruleLabelAllowed` falls back to a
// static per-detection-type class map for legacy rules, and these three types have no entry
// in it — so a rule saved with no classes would match nothing, forever, silently. A rule that
// cannot fire is worse than no rule: somebody believes an area is being watched.
func validateDwellRule(rule DetectionRule) error {
	if !isDwellType(rule.DetectionType) {
		return nil
	}
	cfg, err := parseDwellConfig(rule)
	if err != nil {
		return err
	}
	if len(cfg.Classes) == 0 {
		return fmt.Errorf("ruleConfig.classes requires at least one object class for %s — a rule with no classes would never fire", rule.DetectionType)
	}
	switch normalizedDetectionType(rule.DetectionType) {
	case DetectionLoitering:
		if cfg.DwellSeconds > dwellMaxSeconds {
			return fmt.Errorf("ruleConfig.dwellSeconds must be between 1 and %d", dwellMaxSeconds)
		}
	case DetectionLeftBehind:
		if cfg.StillSeconds > dwellMaxSeconds {
			return fmt.Errorf("ruleConfig.stillSeconds must be between 1 and %d", dwellMaxSeconds)
		}
		if cfg.DriftTolerance > 0.5 {
			return fmt.Errorf("ruleConfig.driftTolerance is a fraction of the frame; %.2f is half the picture", cfg.DriftTolerance)
		}
	case DetectionDirection:
		if !cfg.HeadingSet {
			return fmt.Errorf("ruleConfig.heading is required for %s — without it there is no wrong way", DetectionDirection)
		}
		if cfg.ToleranceDegrees > 180 {
			return fmt.Errorf("ruleConfig.toleranceDegrees must be 180 or less; %.0f accepts every direction", cfg.ToleranceDegrees)
		}
		if cfg.MinTravel > 1 {
			return fmt.Errorf("ruleConfig.minTravel is a fraction of the frame; %.2f is wider than the picture", cfg.MinTravel)
		}
	}
	return nil
}

// dwellRuleState is one rule's tracks on one camera.
type dwellRuleState struct {
	nextTrackID int64
	tracks      map[int64]*dwellTrack
}

type dwellTrack struct {
	trackCore
	// inZoneSince is when this track entered the zone, or 0 while it is outside.
	inZoneSince int64
	// anchor / anchorAt are where the object settled and when — the left-behind timer.
	anchor   point2D
	anchorAt int64
	// origin / originAt are where the track was first seen, for the direction rule.
	origin   point2D
	originAt int64
	// fired stops one object alerting over and over. The rule's cooldown limits how often
	// the RULE speaks; this limits how often one OBJECT does, which is not the same thing:
	// without it, a person who sits down for an hour re-fires on every cooldown expiry.
	fired bool
}

func (d *ObjectRuleDetector) detectDwell(rule DetectionRule, candidates []ObjectCandidate, state *objectRuleState, now int64) ([]Detection, error) {
	cfg, err := parseDwellConfig(rule)
	if err != nil {
		return nil, err
	}
	if state.dwellRules == nil {
		state.dwellRules = map[int64]*dwellRuleState{}
	}
	ruleState := state.dwellRules[rule.Id]
	if ruleState == nil {
		ruleState = &dwellRuleState{tracks: map[int64]*dwellTrack{}}
		state.dwellRules[rule.Id] = ruleState
	}

	zones := parseZones(rule.ZonePolygon)
	minConfidence := rule.Threshold
	if minConfidence <= 0 {
		minConfidence = DefaultDetectionThreshold
	}
	if d.minObjectConfidence > 0 {
		minConfidence = math.Max(minConfidence, d.minObjectConfidence)
	}

	// People are collected regardless of the rule's own classes: the left-behind rule needs
	// to know whether anybody is standing next to the bag, and "person" is rarely one of the
	// classes such a rule is watching for.
	people := make([]point2D, 0, 4)
	for _, candidate := range candidates {
		if strings.ToLower(strings.TrimSpace(candidate.Label)) != "person" {
			continue
		}
		if candidate.Confidence < minConfidence {
			continue
		}
		people = append(people, boxCenter(normalizeBox(candidate.Box)))
	}

	cooldown := rule.CooldownSeconds
	if cooldown <= 0 {
		cooldown = DefaultDetectionCooldown
	}

	used := map[int64]bool{}
	detections := make([]Detection, 0)
	for _, raw := range candidates {
		candidate := raw
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		if candidate.Label == "" || candidate.Confidence < minConfidence {
			continue
		}
		if !d.ruleLabelAllowed(rule, candidate.Label) {
			continue
		}
		candidate.Box = normalizeBox(candidate.Box)
		center := boxCenter(candidate.Box)
		inZone := boxCenterInAnyZone(candidate.Box, zones)

		yoloID := yoloTrackIDFromMetadata(candidate.Metadata)
		track, matched := matchTrack(ruleState.tracks, candidate.Label, center, yoloID, cfg.MaxTrackDistance, used)
		if !matched {
			// A track is only born INSIDE the zone. Starting one outside and letting it
			// walk in would date the dwell from wherever the object first appeared on
			// camera, which for a loitering rule on a doorway means the timer starts in
			// the car park.
			if !inZone {
				continue
			}
			ruleState.nextTrackID++
			track = &dwellTrack{
				trackCore: trackCore{
					id: ruleState.nextTrackID, yoloTrackID: yoloID,
					label: candidate.Label, center: center, box: candidate.Box, lastSeen: now,
				},
				inZoneSince: now,
				anchor:      center,
				anchorAt:    now,
				origin:      center,
				originAt:    now,
			}
			ruleState.tracks[track.id] = track
			used[track.id] = true
		}

		track.center = center
		track.box = candidate.Box
		track.lastSeen = now
		track.seen++

		if !inZone {
			// LEAVING IS AN EVENT, and it resets the timers immediately. This is the
			// opposite of the grace given to a track that merely vanishes: not being seen
			// is missing information, whereas being seen outside the zone is information.
			// Conflating the two either resets on every flicker or lets somebody who walks
			// out and back accumulate a dwell they never had.
			track.inZoneSince = 0
			track.anchorAt = now
			track.anchor = center
			track.fired = false
			continue
		}
		if track.inZoneSince == 0 {
			track.inZoneSince = now
			track.origin = center
			track.originAt = now
		}
		if pointDistance(track.anchor, center) > cfg.DriftTolerance {
			// It moved: it is not "left behind" any more, and the still-timer restarts
			// from here.
			track.anchor = center
			track.anchorAt = now
		}
		if track.fired {
			continue
		}

		var detection Detection
		var fire bool
		switch normalizedDetectionType(rule.DetectionType) {
		case DetectionLoitering:
			dwell := now - track.inZoneSince
			if dwell >= int64(cfg.DwellSeconds) {
				detection, fire = dwellDetection(rule, candidate, track, map[string]any{
					"dwellSeconds": dwell,
					// WHEN IT STARTED, not only when we noticed. An alert that says
					// "loitering at 14:05" sends somebody to 14:05 in the footage, and
					// the person arrived at 14:04:30. Same trap W2-2 found in the SLA
					// numbers: a threshold between the event and the notice is a bias,
					// and here it is also a wrong timestamp on a piece of evidence.
					"dwellStartedAt": track.inZoneSince,
				}, fmt.Sprintf("%s loitering for %ds", candidate.Label, dwell)), true
			}
		case DetectionLeftBehind:
			still := now - track.anchorAt
			if still >= int64(cfg.StillSeconds) {
				attended := cfg.RequireUnattended && personWithin(people, center, cfg.PersonRadius)
				if !attended {
					detection, fire = dwellDetection(rule, candidate, track, map[string]any{
						"stillSeconds":      still,
						"stillSince":        track.anchorAt,
						"requireUnattended": cfg.RequireUnattended,
						"peopleNearby":      countWithin(people, center, cfg.PersonRadius),
					}, fmt.Sprintf("%s left unattended for %ds", candidate.Label, still)), true
				}
			}
		case DetectionDirection:
			travel := pointDistance(track.origin, center)
			if travel >= cfg.MinTravel {
				bearing := bearingDegrees(track.origin, center)
				if angularDistance(bearing, cfg.Heading) <= cfg.ToleranceDegrees {
					detection, fire = dwellDetection(rule, candidate, track, map[string]any{
						"headingDegrees": math.Round(bearing),
						"wantedHeading":  cfg.Heading,
						"travel":         math.Round(travel*1000) / 1000,
						"since":          track.originAt,
					}, fmt.Sprintf("%s travelling %s", candidate.Label, headingWord(cfg.Heading))), true
				}
			}
		}

		if !fire {
			continue
		}
		// The rule's cooldown is checked LAST, so a suppressed alert does not also mark the
		// track as fired — otherwise the one object the rule exists for goes unreported for
		// as long as it stays, because its single chance landed inside a cooldown.
		if cooldownActive(state.lastTriggered, rule, now, cooldown) {
			continue
		}
		state.lastTriggered[rule.Id] = now
		track.fired = true
		detections = append(detections, detection)
	}

	// Ageing happens after matching, because it is defined in terms of what this pass
	// matched: `used` is exactly the set of tracks that were seen.
	pruneTracks(ruleState.tracks, used, cfg.GraceSamples)
	return detections, nil
}

func dwellDetection(rule DetectionRule, candidate ObjectCandidate, track *dwellTrack, extra map[string]any, label string) Detection {
	boundingBox, _ := json.Marshal(candidate.Box)
	meta := map[string]any{
		"objectLabel": candidate.Label,
		"objectMeta":  candidate.Metadata,
		"trackId":     track.id,
	}
	for k, v := range extra {
		meta[k] = v
	}
	metadata, _ := json.Marshal(meta)
	return Detection{
		RuleId:        rule.Id,
		CameraId:      rule.CameraId,
		DetectionType: rule.DetectionType,
		Label:         label,
		Confidence:    candidate.Confidence,
		ZonePolygon:   rule.ZonePolygon,
		BoundingBox:   string(boundingBox),
		Metadata:      string(metadata),
	}
}

func personWithin(people []point2D, at point2D, radius float64) bool {
	return countWithin(people, at, radius) > 0
}

func countWithin(people []point2D, at point2D, radius float64) int {
	n := 0
	for _, p := range people {
		if pointDistance(p, at) <= radius {
			n++
		}
	}
	return n
}

// bearingDegrees is the compass-style heading from a to b, with 0 = UP THE IMAGE and angles
// increasing clockwise. Image y grows downward, so it is negated: without that, "up" and
// "down" are swapped and a wrong-way rule fires on exactly the traffic it should ignore.
func bearingDegrees(a point2D, b point2D) float64 {
	dx := b.X - a.X
	dy := -(b.Y - a.Y)
	deg := math.Atan2(dx, dy) * 180 / math.Pi
	return math.Mod(deg+360, 360)
}

// angularDistance is the shortest angle between two headings, so 350° and 10° are 20° apart
// rather than 340°.
func angularDistance(a float64, b float64) float64 {
	diff := math.Mod(math.Abs(a-b), 360)
	if diff > 180 {
		return 360 - diff
	}
	return diff
}

func headingWord(deg float64) string {
	switch {
	case deg < 45 || deg >= 315:
		return "up"
	case deg < 135:
		return "right"
	case deg < 225:
		return "down"
	default:
		return "left"
	}
}
