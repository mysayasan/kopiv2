package vision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	defaultLineMaxTrackDistance      = 0.25
	defaultLineTrackTTLSeconds       = 10
	defaultMaxSecondsBetweenLineStep = 20
	maxLineCrossingLines             = 5
	lineGeometryEpsilon              = 0.000001
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
	Classes                []string      `json:"classes"`
	Direction              string        `json:"direction"`
	MaxSecondsBetweenLines int           `json:"maxSecondsBetweenLines"`
	MaxTrackDistance       float64       `json:"maxTrackDistance"`
	TrackTTLSeconds        int           `json:"trackTtlSeconds"`
	Lines                  []lineSegment `json:"lines"`
}

type rawLineCrossingConfig struct {
	Classes                []string       `json:"classes"`
	Direction              string         `json:"direction"`
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
	id                int64
	yoloTrackID       int64 // stable ID assigned by ByteTrack in the YOLO worker; 0 when unavailable
	label             string
	center            point2D
	box               Box
	seen              int
	lastSeen          int64
	nextLineIndex     int
	sequenceStartedAt int64
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
		Direction:              normalizeLineDirection(raw.Direction),
		MaxSecondsBetweenLines: raw.MaxSecondsBetweenLines,
		MaxTrackDistance:       raw.MaxTrackDistance,
		TrackTTLSeconds:        raw.TrackTTLSeconds,
		Lines:                  make([]lineSegment, 0, len(raw.Lines)),
	}
	if cfg.Direction == "" {
		cfg.Direction = "both"
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
			continue
		}

		detection, crossed := d.lineCrossingDetection(rule, cfg, state, track, match.candidate, previous, now, cooldown)
		if crossed {
			detections = append(detections, detection)
		}
	}
	return detections, nil
}

func (d *ObjectRuleDetector) lineMatches(rule DetectionRule, cfg lineCrossingConfig, candidates []ObjectCandidate) []lineMatch {
	zone := parseZone(rule.ZonePolygon)
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
			continue
		}
		if !d.lineLabelAllowed(rule, cfg, candidate.Label) {
			continue
		}
		candidate.Box = normalizeBox(candidate.Box)
		center := boxCenter(candidate.Box)
		if !pointInPolygon(center.X, center.Y, zone) {
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

func (d *ObjectRuleDetector) lineCrossingDetection(rule DetectionRule, cfg lineCrossingConfig, state *objectRuleState, track *lineTrack, candidate ObjectCandidate, previous point2D, now int64, cooldown int) (Detection, bool) {
	switch normalizedDetectionType(rule.DetectionType) {
	case DetectionLineCrossing:
		for index, line := range cfg.Lines {
			if !crossedLine(previous, track.center, line, cfg.Direction) {
				continue
			}
			if !ruleCooldownElapsed(state, rule.Id, now, cooldown) {
				return Detection{}, false
			}
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
		if !crossedLine(previous, track.center, line, cfg.Direction) {
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
		if !ruleCooldownElapsed(state, rule.Id, now, cooldown) {
			return Detection{}, false
		}
		state.lastTriggered[rule.Id] = now
		return buildLineCrossingDetection(rule, candidate, track, line, len(cfg.Lines)-1, len(cfg.Lines), "multi-line-crossing-detector"), true
	}
	return Detection{}, false
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

	// Prefer matching by stable ByteTrack ID when the YOLO worker provides one.
	if yoloID > 0 {
		for _, track := range s.tracks {
			if used[track.id] || track.label != match.candidate.Label {
				continue
			}
			if track.yoloTrackID == yoloID {
				used[track.id] = true
				return track, false
			}
		}
	}

	// Fall back to nearest-centre matching for workers without ByteTrack.
	var best *lineTrack
	bestDistance := math.MaxFloat64
	for _, track := range s.tracks {
		if used[track.id] || track.label != match.candidate.Label {
			continue
		}
		distance := pointDistance(track.center, match.center)
		if distance <= maxDistance && distance < bestDistance {
			best = track
			bestDistance = distance
		}
	}
	if best != nil {
		if yoloID > 0 {
			best.yoloTrackID = yoloID // adopt the YOLO ID now that we have one
		}
		used[best.id] = true
		return best, false
	}

	s.nextTrackID++
	track := &lineTrack{
		id:          s.nextTrackID,
		yoloTrackID: yoloID,
		label:       match.candidate.Label,
		center:      match.center,
		box:         match.candidate.Box,
		lastSeen:    now,
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

func crossedLine(previous point2D, current point2D, line lineSegment, direction string) bool {
	if pointDistance(previous, current) <= lineGeometryEpsilon {
		return false
	}
	if !segmentsIntersect(previous, current, line.A, line.B) {
		return false
	}
	return directionMatches(previous, current, line, direction)
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

func ruleCooldownElapsed(state *objectRuleState, ruleID int64, now int64, cooldown int) bool {
	if last := state.lastTriggered[ruleID]; last > 0 && now-last < int64(cooldown) {
		return false
	}
	return true
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
