package vision

// PresenceGate is the Teach capture engine's frame gate: it keeps a rolling
// previous frame and, for each new JPEG, reports how much changed inside the
// configured zone(s), the normalized bounding box of the change, and a
// perceptual hash for near-duplicate rejection. It reuses the motion detector's
// gray-frame differencing so "something is being shown to the camera" is judged
// the same way motion detection judges it.
type PresenceGate struct {
	zones [][][2]float64
	prev  *grayFrame
}

// PresenceResult is one Feed outcome. Primed is false for the very first frame
// (no previous frame to diff against — the gate is just warming up); Hash is
// always set so dedup can consider even non-motion frames.
type PresenceResult struct {
	Primed      bool    `json:"primed"`
	MotionRatio float64 `json:"motionRatio"`
	HasMotion   bool    `json:"hasMotion"`
	Box         Box     `json:"box"`
	Hash        uint64  `json:"-"`
}

// NewPresenceGate builds a gate for a zone-polygon value (same format as
// DetectionRule.ZonePolygon; empty means the whole frame).
func NewPresenceGate(zonePolygon string) *PresenceGate {
	return &PresenceGate{zones: parseZones(zonePolygon)}
}

// Feed diffs the frame against the previous one within the gate's zones.
func (g *PresenceGate) Feed(jpegData []byte) (PresenceResult, error) {
	current, err := decodeGrayFrame(jpegData)
	if err != nil {
		return PresenceResult{}, err
	}
	result := PresenceResult{Hash: dhashGray(current)}
	if g.prev == nil || g.prev.width != current.width || g.prev.height != current.height {
		g.prev = current
		return result, nil
	}
	result.Primed = true
	ratio, box, ok := motionBounds(g.prev, current, g.zones, defaultMotionStride, defaultMotionPixelDelta)
	g.prev = current
	result.MotionRatio = ratio
	result.HasMotion = ok
	result.Box = box
	return result, nil
}

// motionBounds is motionStats with a bounding box: it walks the sampled grid,
// counts changed cells inside the zones, and tracks the min/max normalized
// coordinates of the change so the caller gets a tight box around whatever
// moved (padded by one stride so edges aren't clipped).
func motionBounds(previous *grayFrame, current *grayFrame, zones [][][2]float64, stride int, pixelDelta int) (float64, Box, bool) {
	if stride <= 0 {
		stride = defaultMotionStride
	}
	total := 0
	changed := 0
	minX, minY := 1.0, 1.0
	maxX, maxY := 0.0, 0.0
	for y := 0; y < current.height; y += stride {
		ny := float64(y) / float64(max(1, current.height-1))
		for x := 0; x < current.width; x += stride {
			nx := float64(x) / float64(max(1, current.width-1))
			if !pointInAnyZone(nx, ny, zones) {
				continue
			}
			total++
			idx := y*current.width + x
			if absInt(int(current.pixels[idx])-int(previous.pixels[idx])) >= pixelDelta {
				changed++
				if nx < minX {
					minX = nx
				}
				if ny < minY {
					minY = ny
				}
				if nx > maxX {
					maxX = nx
				}
				if ny > maxY {
					maxY = ny
				}
			}
		}
	}
	if total == 0 || changed == 0 {
		return 0, Box{}, false
	}
	pad := float64(stride) / float64(max(1, current.width))
	box := Box{
		X: clamp(minX - pad),
		Y: clamp(minY - pad),
		W: clamp(maxX+pad) - clamp(minX-pad),
		H: clamp(maxY+pad) - clamp(minY-pad),
	}
	if box.W <= 0 || box.H <= 0 {
		return float64(changed) / float64(total), Box{}, false
	}
	return float64(changed) / float64(total), box, true
}

// dhashGray computes a 64-bit difference hash (9x8 grid, row-wise gradient) of
// the frame — the standard near-duplicate fingerprint.
func dhashGray(f *grayFrame) uint64 {
	const cols, rows = 9, 8
	var cells [rows][cols]int
	for r := 0; r < rows; r++ {
		y := (r*2 + 1) * f.height / (rows * 2)
		for c := 0; c < cols; c++ {
			x := (c*2 + 1) * f.width / (cols * 2)
			cells[r][c] = int(f.pixels[y*f.width+x])
		}
	}
	var hash uint64
	bit := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols-1; c++ {
			if cells[r][c] > cells[r][c+1] {
				hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	return hash
}

// ZoneBounds returns the normalized bounding box of a zone-polygon value (the
// union bbox across polygons; the full frame when the value is empty/invalid).
// The Teach engine uses it as the annotation-box fallback when the motion box
// is degenerate — for inspection skills the ROI typically frames the product.
func ZoneBounds(zonePolygon string) Box {
	zones := parseZones(zonePolygon)
	minX, minY := 1.0, 1.0
	maxX, maxY := 0.0, 0.0
	for _, zone := range zones {
		for _, p := range zone {
			if p[0] < minX {
				minX = p[0]
			}
			if p[1] < minY {
				minY = p[1]
			}
			if p[0] > maxX {
				maxX = p[0]
			}
			if p[1] > maxY {
				maxY = p[1]
			}
		}
	}
	if maxX <= minX || maxY <= minY {
		return Box{X: 0, Y: 0, W: 1, H: 1}
	}
	return Box{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// HammingDistance counts differing bits between two hashes (dedup metric).
func HammingDistance(a, b uint64) int {
	x := a ^ b
	count := 0
	for x != 0 {
		x &= x - 1
		count++
	}
	return count
}
