package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"sync"
	"time"
)

const (
	defaultMotionPixelDelta = 35
	defaultMotionStride     = 8
)

type motionState struct {
	previous      *grayFrame
	hitsByRule    map[int64]int
	lastTriggered map[int64]int64
}

type grayFrame struct {
	width  int
	height int
	pixels []uint8
}

// MotionDetector compares consecutive frames and raises detections for movement inside rule zones.
type MotionDetector struct {
	mu         sync.Mutex
	byCamera   map[int64]*motionState
	pixelDelta int
	stride     int
	now        func() time.Time
}

func NewMotionDetector() *MotionDetector {
	return &MotionDetector{
		byCamera:   map[int64]*motionState{},
		pixelDelta: defaultMotionPixelDelta,
		stride:     defaultMotionStride,
		now:        time.Now,
	}
}

func (d *MotionDetector) Detect(ctx context.Context, frame Frame, rules []DetectionRule) ([]Detection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := decodeGrayFrame(frame.Data)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	state := d.byCamera[frame.CameraId]
	if state == nil {
		state = &motionState{
			hitsByRule:    map[int64]int{},
			lastTriggered: map[int64]int64{},
		}
		d.byCamera[frame.CameraId] = state
	}
	if state.previous == nil || state.previous.width != current.width || state.previous.height != current.height {
		state.previous = current
		return nil, nil
	}

	now := d.now().UTC().Unix()
	detections := make([]Detection, 0)
	for _, rule := range rules {
		if !rule.IsEnabled {
			continue
		}
		changedRatio := motionRatio(state.previous, current, parseZone(rule.ZonePolygon), d.stride, d.pixelDelta)
		confidence := math.Min(1, changedRatio*20)
		if confidence >= rule.Threshold {
			state.hitsByRule[rule.Id]++
		} else {
			state.hitsByRule[rule.Id] = 0
		}
		minFrames := rule.MinFrames
		if minFrames <= 0 {
			minFrames = DefaultDetectionMinFrames
		}
		cooldown := rule.CooldownSeconds
		if cooldown <= 0 {
			cooldown = DefaultDetectionCooldown
		}
		if state.hitsByRule[rule.Id] < minFrames {
			continue
		}
		if last := state.lastTriggered[rule.Id]; last > 0 && now-last < int64(cooldown) {
			continue
		}
		state.lastTriggered[rule.Id] = now
		metadata, _ := json.Marshal(map[string]any{
			"source":       "motion-detector",
			"changedRatio": changedRatio,
		})
		detections = append(detections, Detection{
			RuleId:        rule.Id,
			CameraId:      rule.CameraId,
			DetectionType: rule.DetectionType,
			Label:         fmt.Sprintf("Motion in %s zone", rule.DetectionType),
			Confidence:    confidence,
			ZonePolygon:   rule.ZonePolygon,
			Metadata:      string(metadata),
		})
	}
	state.previous = current
	return detections, nil
}

func decodeGrayFrame(data []byte) (*grayFrame, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	pixels := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			pixels[y*width+x] = uint8(((r>>8)*30 + (g>>8)*59 + (b>>8)*11) / 100)
		}
	}
	return &grayFrame{width: width, height: height, pixels: pixels}, nil
}

func motionRatio(previous *grayFrame, current *grayFrame, polygon [][2]float64, stride int, pixelDelta int) float64 {
	if stride <= 0 {
		stride = defaultMotionStride
	}
	total := 0
	changed := 0
	for y := 0; y < current.height; y += stride {
		ny := float64(y) / float64(max(1, current.height-1))
		for x := 0; x < current.width; x += stride {
			nx := float64(x) / float64(max(1, current.width-1))
			if !pointInPolygon(nx, ny, polygon) {
				continue
			}
			total++
			idx := y*current.width + x
			if absInt(int(current.pixels[idx])-int(previous.pixels[idx])) >= pixelDelta {
				changed++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(changed) / float64(total)
}

func parseZone(value string) [][2]float64 {
	var raw [][]float64
	if err := json.Unmarshal([]byte(value), &raw); err != nil || len(raw) < 3 {
		return [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	}
	points := make([][2]float64, 0, len(raw))
	for _, point := range raw {
		if len(point) < 2 {
			continue
		}
		points = append(points, [2]float64{clamp(point[0]), clamp(point[1])})
	}
	if len(points) < 3 {
		return [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	}
	return points
}

func pointInPolygon(x float64, y float64, polygon [][2]float64) bool {
	inside := false
	j := len(polygon) - 1
	for i := range polygon {
		xi, yi := polygon[i][0], polygon[i][1]
		xj, yj := polygon[j][0], polygon[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

func clamp(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
