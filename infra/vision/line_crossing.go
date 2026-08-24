package vision

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
)

// lineDebug gates verbose per-sample tracing of the line-crossing pipeline.
// Enable by launching the server with MYMATASAN_LINE_DEBUG=1. Trace lines are
// written to stderr (prefix "line-debug"). Mirrors MYMATASAN_YOLO_DEBUG in the
// YOLO worker. Diagnostic only — remove the env to silence it.
var lineDebug = func() bool {
	v := strings.TrimSpace(os.Getenv("MYMATASAN_LINE_DEBUG"))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}()

func lineLog(format string, args ...any) {
	if lineDebug {
		log.Printf("line-debug "+format, args...)
	}
}

const (
	defaultLineMaxTrackDistance      = 0.25
	defaultLineTrackTTLSeconds       = 10
	defaultMaxSecondsBetweenLineStep = 20
	maxLineCrossingLines             = 5
	lineGeometryEpsilon              = 0.000001
	// lineCrossingBand is the perpendicular dead-band (normalized image units, ~2%
	// of the frame) around a line. A track only counts as "on a side" once its
	// centre is farther than this from the line; movement that merely jitters
	// within the band never registers as a crossing. Without it, sub-pixel wobble
	// on either side of the line fired in BOTH directions, defeating the arrow
	// (direction) filter — an object could trip a one-way line from the wrong side.
	lineCrossingBand = 0.02
)

type point2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type lineSegment struct {
	ID string  `json:"id"`
	A  point2D `json:"a"`
	B  point2D `json:"b"`
}

type lineCrossingConfig struct {
	Classes                []string
	Direction              string // "both" (either way through), "forward" (toward the arrow/positive side) or "reverse"
	MaxSecondsBetweenLines int
	MaxTrackDistance       float64
	TrackTTLSeconds        int
	Lines                  []lineSegment
}

type rawLineCrossingConfig struct {
	Classes                []string       `json:"classes"`
	Direction              string         `json:"direction"` // both|forward|reverse
	MaxSecondsBetweenLines int            `json:"maxSecondsBetweenLines"`
	MaxTrackDistance       float64        `json:"maxTrackDistance"`
	TrackTTLSeconds        int            `json:"trackTtlSeconds"`
	Lines                  []rawLineEntry `json:"lines"`
}

type rawLineEntry struct {
	ID     string      `json:"id"`
	Points [][]float64 `json:"points"`
}

type lineCrossingRuleState struct {
	nextTrackID int64
	tracks      map[int64]*lineTrack
}

type lineTrack struct {
	// The identity half lives in trackCore (track.go), shared with the dwell rules —
	// id, label, centre, box, seen count and lastSeen, plus the ByteTrack id when the
	// worker supplies one. Its fields are promoted, so track.center still reads as before.
	trackCore
	nextLineIndex     int
	sequenceStartedAt int64
	// sides holds the last confirmed side of each line (keyed by line ID). It gives
	// the crossing detector hysteresis: a crossing is only recognised when the track
	// moves from clearly one side of a line to clearly the other (past lineCrossingBand),
	// which both rejects jitter and correctly handles a slow crosser stepping through
	// the band over several frames.
	sides map[string]*lineSideState
}

// lineSideState records where a track was last confirmed relative to one line.
type lineSideState struct {
	side   int     // -1 negative side, +1 positive (signedArea) side, 0 = not yet confirmed / in band
	center point2D // the track centre at that last confirmation, used to test the actual segment crossing
}

type lineMatch struct {
	candidate ObjectCandidate
	center    point2D
}

func validateLineCrossingRule(rule DetectionRule) error {
	if !isLineCrossingType(rule.DetectionType) {
		return nil
	}
	cfg, err := parseLineCrossingConfig(rule)
	if err != nil {
		return err
	}
	switch normalizedDetectionType(rule.DetectionType) {
	case DetectionLineCrossing:
		if len(cfg.Lines) < 1 {
			return fmt.Errorf("ruleConfig.lines requires at least one line for %s", DetectionLineCrossing)
		}
	case DetectionMultiLineCrossing:
		if len(cfg.Lines) < 2 {
			return fmt.Errorf("ruleConfig.lines requires at least two lines for %s", DetectionMultiLineCrossing)
		}
	}
	return nil
}

func parseLineCrossingConfig(rule DetectionRule) (lineCrossingConfig, error) {
	var raw rawLineCrossingConfig
	if strings.TrimSpace(rule.RuleConfig) != "" {
		if err := json.Unmarshal([]byte(rule.RuleConfig), &raw); err != nil {
			return lineCrossingConfig{}, fmt.Errorf("ruleConfig must be valid line crossing JSON: %w", err)
		}
	}

	cfg := lineCrossingConfig{
		Classes:                normalizeStringList(raw.Classes),
		MaxSecondsBetweenLines: raw.MaxSecondsBetweenLines,
		MaxTrackDistance:       raw.MaxTrackDistance,
		TrackTTLSeconds:        raw.TrackTTLSeconds,
		Lines:                  make([]lineSegment, 0, len(raw.Lines)),
	}
	if cfg.MaxSecondsBetweenLines <= 0 {
		cfg.MaxSecondsBetweenLines = defaultMaxSecondsBetweenLineStep
	}
	if cfg.MaxTrackDistance <= 0 {
		cfg.MaxTrackDistance = defaultLineMaxTrackDistance
	}
	if cfg.TrackTTLSeconds <= 0 {
		cfg.TrackTTLSeconds = defaultLineTrackTTLSeconds
	}
	if len(raw.Lines) > maxLineCrossingLines {
		return lineCrossingConfig{}, fmt.Errorf("ruleConfig.lines supports at most %d lines", maxLineCrossingLines)
	}
	for index, rawLine := range raw.Lines {
		if len(rawLine.Points) < 2 || len(rawLine.Points[0]) < 2 || len(rawLine.Points[1]) < 2 {
			return lineCrossingConfig{}, fmt.Errorf("ruleConfig.lines[%d].points requires two [x,y] points", index)
		}
		line := lineSegment{
			ID: strings.TrimSpace(rawLine.ID),
			A:  point2D{X: clamp(rawLine.Points[0][0]), Y: clamp(rawLine.Points[0][1])},
			B:  point2D{X: clamp(rawLine.Points[1][0]), Y: clamp(rawLine.Points[1][1])},
		}
		if line.ID == "" {
			line.ID = fmt.Sprintf("line-%d", index+1)
		}
		if pointDistance(line.A, line.B) <= lineGeometryEpsilon {
			return lineCrossingConfig{}, fmt.Errorf("ruleConfig.lines[%d] must have two distinct points", index)
		}
		cfg.Lines = append(cfg.Lines, line)
	}

	// Direction filter is a simple side check: "both" fires on a crossing either way,
	// "forward" only when the object ends up on the arrow (positive signedArea) side,
	// "reverse" only on the other side.
	cfg.Direction = normalizeLineDirection(raw.Direction)
	if cfg.Direction == "" {
		cfg.Direction = "both"
	}
	return cfg, nil
}

func (d *ObjectRuleDetector) detectLineCrossing(rule DetectionRule, candidates []ObjectCandidate, state *objectRuleState, now int64) ([]Detection, error) {
	cfg, err := parseLineCrossingConfig(rule)
	if err != nil {
		return nil, err
	}
	if len(cfg.Lines) == 0 {
		return nil, nil
	}
	if state.lineRules == nil {
		state.lineRules = map[int64]*lineCrossingRuleState{}
	}
	ruleState := state.lineRules[rule.Id]
	if ruleState == nil {
		ruleState = &lineCrossingRuleState{tracks: map[int64]*lineTrack{}}
		state.lineRules[rule.Id] = ruleState
	}
	ruleState.cleanup(now, cfg.TrackTTLSeconds)

	matches := d.lineMatches(rule, cfg, candidates)
	lineLog("cam=%d rule=%d(%q) candidates=%d matches=%d tracks=%d dir=%s maxDist=%.3f ttl=%ds",
		rule.CameraId, rule.Id, rule.Name, len(candidates), len(matches), len(ruleState.tracks), cfg.Direction, cfg.MaxTrackDistance, cfg.TrackTTLSeconds)
	if len(matches) == 0 {
		return nil, nil
	}

	cooldown := rule.CooldownSeconds
	if cooldown <= 0 {
		cooldown = DefaultDetectionCooldown
	}

	usedTracks := map[int64]bool{}
	detections := make([]Detection, 0)
	for _, match := range matches {
		track, isNew := ruleState.matchOrCreate(match, cfg.MaxTrackDistance, now, usedTracks)
		previous := track.center
		track.label = match.candidate.Label
		track.center = match.center
		track.box = match.candidate.Box
		track.lastSeen = now
		track.seen++
		if isNew {
			// Record which side of each line the track starts on so the next sample
			// that reaches the far side registers as a crossing.
			track.seedLineSides(cfg.Lines)
			lineLog("cam=%d rule=%d NEW track=%d label=%s conf=%.2f center=(%.3f,%.3f) — need a 2nd matched sample on the other side of the line to fire",
				rule.CameraId, rule.Id, track.id, match.candidate.Label, match.candidate.Confidence, match.center.X, match.center.Y)
			continue
		}
		lineLog("cam=%d rule=%d track=%d label=%s conf=%.2f prev=(%.3f,%.3f) curr=(%.3f,%.3f) moved=%.3f",
			rule.CameraId, rule.Id, track.id, match.candidate.Label, match.candidate.Confidence,
			previous.X, previous.Y, match.center.X, match.center.Y, pointDistance(previous, match.center))

		detection, crossed := d.lineCrossingDetection(rule, cfg, state, track, match.candidate, now, cooldown)
		if crossed {
			detections = append(detections, detection)
		}
	}
	return detections, nil
}

func (d *ObjectRuleDetector) lineMatches(rule DetectionRule, cfg lineCrossingConfig, candidates []ObjectCandidate) []lineMatch {
	zones := parseZones(rule.ZonePolygon)
	minConfidence := rule.Threshold
	if minConfidence <= 0 {
		minConfidence = DefaultDetectionThreshold
	}
	if d.minObjectConfidence > 0 {
		minConfidence = math.Max(minConfidence, d.minObjectConfidence)
	}

	result := make([]lineMatch, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.Label = strings.ToLower(strings.TrimSpace(candidate.Label))
		if candidate.Label == "" || candidate.Confidence < minConfidence {
			lineLog("cam=%d rule=%d REJECT label=%q conf=%.2f < min=%.2f (raise nothing — lower the rule threshold or move closer)",
				rule.CameraId, rule.Id, candidate.Label, candidate.Confidence, minConfidence)
			continue
		}
		if !d.lineLabelAllowed(rule, cfg, candidate.Label) {
			lineLog("cam=%d rule=%d REJECT label=%q not in allowed classes %v", rule.CameraId, rule.Id, candidate.Label, cfg.Classes)
			continue
		}
		candidate.Box = normalizeBox(candidate.Box)
		center := boxCenter(candidate.Box)
		if !pointInAnyZone(center.X, center.Y, zones) {
			lineLog("cam=%d rule=%d REJECT label=%q center=(%.3f,%.3f) outside zone polygon", rule.CameraId, rule.Id, candidate.Label, center.X, center.Y)
			continue
		}
		result = append(result, lineMatch{candidate: candidate, center: center})
	}
	return result
}

func (d *ObjectRuleDetector) lineLabelAllowed(rule DetectionRule, cfg lineCrossingConfig, label string) bool {
	if len(cfg.Classes) > 0 {
		for _, class := range cfg.Classes {
			if class == "*" || label == class {
				return true
			}
		}
		return false
	}
	return d.labelAllowed(rule.DetectionType, label)
}

func (d *ObjectRuleDetector) lineCrossingDetection(rule DetectionRule, cfg lineCrossingConfig, state *objectRuleState, track *lineTrack, candidate ObjectCandidate, now int64, cooldown int) (Detection, bool) {
	// Evaluate every line so each one's hysteresis state stays warm even when the
	// track is not currently crossing it. crossings[i] is the side the track just moved
	// TO on line i (+1 arrow side, -1 the other), or 0 for no confirmed crossing.
	crossings := make([]int, len(cfg.Lines))
	for i, line := range cfg.Lines {
		crossed, side := track.evaluateLineCrossing(line, track.center)
		if crossed {
			crossings[i] = side
		}
		lineLog("cam=%d rule=%d evalLine id=%s crossed=%v side=%d allowed=%v dir=%s",
			rule.CameraId, rule.Id, line.ID, crossed, side, crossed && lineDirectionAllows(cfg.Direction, side), cfg.Direction)
	}

	switch normalizedDetectionType(rule.DetectionType) {
	case DetectionLineCrossing:
		for index, line := range cfg.Lines {
			if crossings[index] == 0 || !lineDirectionAllows(cfg.Direction, crossings[index]) {
				continue
			}
			if !ruleCooldownElapsed(state, rule, now, cooldown) {
				lineLog("cam=%d rule=%d CROSSED but suppressed by cooldown (%ds)", rule.CameraId, rule.Id, cooldown)
				return Detection{}, false
			}
			lineLog("cam=%d rule=%d *** CROSSED line=%s — firing alert ***", rule.CameraId, rule.Id, line.ID)
			state.lastTriggered[rule.Id] = now
			return buildLineCrossingDetection(rule, candidate, track, line, index, 1, "line-crossing-detector"), true
		}
	case DetectionMultiLineCrossing:
		if track.nextLineIndex < 0 || track.nextLineIndex >= len(cfg.Lines) {
			track.nextLineIndex = 0
		}
		if track.sequenceStartedAt > 0 && now-track.sequenceStartedAt > int64(cfg.MaxSecondsBetweenLines) {
			track.nextLineIndex = 0
			track.sequenceStartedAt = 0
		}
		line := cfg.Lines[track.nextLineIndex]
		if crossings[track.nextLineIndex] == 0 || !lineDirectionAllows(cfg.Direction, crossings[track.nextLineIndex]) {
			return Detection{}, false
		}
		if track.nextLineIndex == 0 {
			track.sequenceStartedAt = now
		}
		track.nextLineIndex++
		if track.nextLineIndex < len(cfg.Lines) {
			return Detection{}, false
		}
		track.nextLineIndex = 0
		track.sequenceStartedAt = 0
		if !ruleCooldownElapsed(state, rule, now, cooldown) {
			return Detection{}, false
		}
		state.lastTriggered[rule.Id] = now
		return buildLineCrossingDetection(rule, candidate, track, line, len(cfg.Lines)-1, len(cfg.Lines), "multi-line-crossing-detector"), true
	}
	return Detection{}, false
}

// seedLineSides records the track's current side for each line without reporting a
// crossing, so a freshly created track has a "from" side for its first real move.
func (t *lineTrack) seedLineSides(lines []lineSegment) {
	for _, line := range lines {
		t.evaluateLineCrossing(line, t.center)
	}
}

// evaluateLineCrossing updates the track's confirmed side for one line and reports
// whether it just crossed the segment and the side it moved TO (+1 arrow/positive
// side, -1 the other). It applies the lineCrossingBand dead-band so jitter within the
// band never registers, and only confirms a crossing when the straight path between
// the last confirmed centre and the current centre actually intersects the (finite)
// segment.
func (t *lineTrack) evaluateLineCrossing(line lineSegment, current point2D) (bool, int) {
	if t.sides == nil {
		t.sides = map[string]*lineSideState{}
	}
	st := t.sides[line.ID]
	if st == nil {
		st = &lineSideState{}
		t.sides[line.ID] = st
	}

	side := lineSideOf(line, current)
	if side == 0 {
		return false, 0 // inside the dead-band: keep the prior confirmed side
	}
	if st.side == 0 {
		st.side = side
		st.center = current
		return false, 0
	}
	if side == st.side {
		st.center = current // still on the same side; remember the latest position
		return false, 0
	}

	from := st.center
	st.side = side
	st.center = current
	if !segmentsIntersect(from, current, line.A, line.B) {
		return false, 0 // moved around the segment's endpoints, not through it
	}
	return true, side
}

// lineSideOf returns +1 when the point is clearly on the positive signedArea side of
// the line, -1 when clearly on the negative side, and 0 when within lineCrossingBand.
func lineSideOf(line lineSegment, p point2D) int {
	perp := perpendicularDistance(line, p)
	switch {
	case perp > lineCrossingBand:
		return 1
	case perp < -lineCrossingBand:
		return -1
	default:
		return 0
	}
}

// perpendicularDistance is the signed distance from p to the line, in normalized
// image units (positive on the signedArea/arrow side).
func perpendicularDistance(line lineSegment, p point2D) float64 {
	length := pointDistance(line.A, line.B)
	if length <= lineGeometryEpsilon {
		return 0
	}
	return signedArea(line.A, line.B, p) / length
}

// lineDirectionAllows reports whether a crossing that moved the track to `side`
// (+1 arrow/positive side, -1 the other) satisfies the rule's direction filter.
// "both" accepts either side; "forward" only +1; "reverse" only -1.
func lineDirectionAllows(direction string, side int) bool {
	switch normalizeLineDirection(direction) {
	case "forward":
		return side > 0
	case "reverse":
		return side < 0
	default:
		return true
	}
}

func buildLineCrossingDetection(rule DetectionRule, candidate ObjectCandidate, track *lineTrack, line lineSegment, lineIndex int, lineCount int, source string) Detection {
	boundingBox, _ := json.Marshal(candidate.Box)
	metadata, _ := json.Marshal(map[string]any{
		"source":      source,
		"objectLabel": candidate.Label,
		"objectMeta":  candidate.Metadata,
		"trackId":     track.id,
		"lineId":      line.ID,
		"lineIndex":   lineIndex,
		"lineCount":   lineCount,
	})
	return Detection{
		RuleId:        rule.Id,
		CameraId:      rule.CameraId,
		DetectionType: rule.DetectionType,
		Label:         detectionLabel(rule.DetectionType, candidate.Label),
		Confidence:    candidate.Confidence,
		ZonePolygon:   rule.ZonePolygon,
		BoundingBox:   string(boundingBox),
		Metadata:      string(metadata),
	}
}

func (s *lineCrossingRuleState) matchOrCreate(match lineMatch, maxDistance float64, now int64, used map[int64]bool) (*lineTrack, bool) {
	yoloID := yoloTrackIDFromMetadata(match.candidate.Metadata)

	// Identity first (ByteTrack), geometry second — the shared matcher in track.go.
	if existing, ok := matchTrack(s.tracks, match.candidate.Label, match.center, yoloID, maxDistance, used); ok {
		return existing, false
	}

	s.nextTrackID++
	track := &lineTrack{
		trackCore: trackCore{
			id:          s.nextTrackID,
			yoloTrackID: yoloID,
			label:       match.candidate.Label,
			center:      match.center,
			box:         match.candidate.Box,
			lastSeen:    now,
		},
	}
	s.tracks[track.id] = track
	used[track.id] = true
	return track, true
}

func yoloTrackIDFromMetadata(metadata map[string]any) int64 {
	if metadata == nil {
		return 0
	}
	val, ok := metadata["trackId"]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func (s *lineCrossingRuleState) cleanup(now int64, ttlSeconds int) {
	if ttlSeconds <= 0 {
		ttlSeconds = defaultLineTrackTTLSeconds
	}
	for id, track := range s.tracks {
		if now-track.lastSeen > int64(ttlSeconds) {
			delete(s.tracks, id)
		}
	}
}

// crossedLine reports whether the straight move previous→current passes through the
// segment in a direction the rule accepts. Used by the motion-centroid fallback
// detector, which has no per-object tracking; the object detector uses the
// hysteresis path in evaluateLineCrossing instead.
func crossedLine(previous point2D, current point2D, line lineSegment, direction string) bool {
	if pointDistance(previous, current) <= lineGeometryEpsilon {
		return false
	}
	if !segmentsIntersect(previous, current, line.A, line.B) {
		return false
	}
	return directionMatches(previous, current, line, direction)
}

// directionMatches reports whether the move previous→current crosses the line in the
// accepted direction (both = either way; forward = onto the positive signedArea side;
// reverse = onto the other side).
func directionMatches(previous point2D, current point2D, line lineSegment, direction string) bool {
	direction = normalizeLineDirection(direction)
	if direction == "" || direction == "both" {
		return true
	}
	prevSide := signedArea(line.A, line.B, previous)
	currSide := signedArea(line.A, line.B, current)
	switch direction {
	case "forward":
		return prevSide < -lineGeometryEpsilon && currSide > lineGeometryEpsilon
	case "reverse":
		return prevSide > lineGeometryEpsilon && currSide < -lineGeometryEpsilon
	default:
		return true
	}
}

func segmentsIntersect(a point2D, b point2D, c point2D, d point2D) bool {
	o1 := signedArea(a, b, c)
	o2 := signedArea(a, b, d)
	o3 := signedArea(c, d, a)
	o4 := signedArea(c, d, b)
	if oppositeSigns(o1, o2) && oppositeSigns(o3, o4) {
		return true
	}
	return onSegment(a, c, b, o1) || onSegment(a, d, b, o2) || onSegment(c, a, d, o3) || onSegment(c, b, d, o4)
}

func normalizeLineDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "both", "any":
		return "both"
	case "forward", "positive", "start_to_end":
		return "forward"
	case "reverse", "negative", "end_to_start":
		return "reverse"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func oppositeSigns(a float64, b float64) bool {
	return (a > lineGeometryEpsilon && b < -lineGeometryEpsilon) || (a < -lineGeometryEpsilon && b > lineGeometryEpsilon)
}

func onSegment(a point2D, p point2D, b point2D, orientation float64) bool {
	if math.Abs(orientation) > lineGeometryEpsilon {
		return false
	}
	return p.X >= math.Min(a.X, b.X)-lineGeometryEpsilon &&
		p.X <= math.Max(a.X, b.X)+lineGeometryEpsilon &&
		p.Y >= math.Min(a.Y, b.Y)-lineGeometryEpsilon &&
		p.Y <= math.Max(a.Y, b.Y)+lineGeometryEpsilon
}

func signedArea(a point2D, b point2D, c point2D) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func pointDistance(a point2D, b point2D) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return math.Sqrt(dx*dx + dy*dy)
}

func boxCenter(box Box) point2D {
	return point2D{
		X: clamp(box.X + box.W/2),
		Y: clamp(box.Y + box.H/2),
	}
}

// ruleCooldownElapsed reports whether rule is clear of its cooldown window. It takes the
// whole rule (not just its id) so the persisted LastTriggeredAt can seed the in-process
// cooldown on first sight — see cooldown.go.
func ruleCooldownElapsed(state *objectRuleState, rule DetectionRule, now int64, cooldown int) bool {
	return !cooldownActive(state.lastTriggered, rule, now, cooldown)
}

func isLineCrossingType(value string) bool {
	switch normalizedDetectionType(value) {
	case DetectionLineCrossing, DetectionMultiLineCrossing:
		return true
	default:
		return false
	}
}

func normalizedDetectionType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
