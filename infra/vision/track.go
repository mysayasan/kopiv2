package vision

import "math"

// The identity half of following an object across frames, shared by every rule that asks a
// question about TIME rather than about a single frame.
//
// It was written for line crossing and is now used by the dwell rules (loitering,
// left-behind, direction) too. Extracted rather than copied: two implementations of "is this
// the same object I saw a moment ago" would eventually disagree, and the disagreement would
// look like one rule being flaky rather than like two answers to one question.

// trackCore is what every tracked object needs regardless of what the rule is looking for.
// Rules embed it and add their own state beside it.
type trackCore struct {
	id int64
	// yoloTrackID is the stable id ByteTrack assigns in the YOLO worker; 0 when the worker
	// does not provide one. Preferred over geometry whenever it is available, because it
	// survives two people crossing paths and nearest-centre matching does not.
	yoloTrackID int64
	label       string
	center      point2D
	box         Box
	seen        int
	lastSeen    int64
	// missed counts consecutive detection passes in which nothing matched this track. It
	// is the unit the grace is measured in — see pruneTracks.
	missed int
}

func (t *trackCore) core() *trackCore { return t }

// trackedObject is anything embedding trackCore.
type trackedObject interface{ core() *trackCore }

// matchTrack finds the track a candidate belongs to, or reports that it is new.
//
// Identity first (ByteTrack), geometry second. `used` stops one track absorbing two
// candidates in the same frame, which is how two people standing together become one track
// that never leaves and loiters forever.
//
// It does not create the track: the caller does, because only the caller knows what its
// rule-specific state should start as. It returns the zero value and false when nothing
// matched.
func matchTrack[T trackedObject](tracks map[int64]T, label string, center point2D, yoloID int64, maxDistance float64, used map[int64]bool) (T, bool) {
	var zero T
	if yoloID > 0 {
		for _, track := range tracks {
			c := track.core()
			if used[c.id] || c.label != label {
				continue
			}
			if c.yoloTrackID == yoloID {
				used[c.id] = true
				return track, true
			}
		}
	}

	var best T
	found := false
	bestDistance := math.MaxFloat64
	for _, track := range tracks {
		c := track.core()
		if used[c.id] || c.label != label {
			continue
		}
		distance := pointDistance(c.center, center)
		if distance <= maxDistance && distance < bestDistance {
			best = track
			found = true
			bestDistance = distance
		}
	}
	if found {
		c := best.core()
		if yoloID > 0 {
			c.yoloTrackID = yoloID // adopt the id now that the worker has given us one
		}
		used[c.id] = true
		return best, true
	}
	return zero, false
}

// pruneTracks forgets tracks that were not matched in the last `grace` passes.
//
// THE GRACE IS COUNTED IN MISSED SAMPLES, NOT IN SECONDS, and that is the whole point of
// this function. What is being tolerated is a DROPPED DETECTION — a confidence dip, a brief
// occlusion, somebody walking in front — and detections only happen when the detector runs.
// "Not seen for eight seconds" says nothing if the camera was sampled once in that window;
// on a camera sampled every twenty seconds a seconds-based grace forgets every object
// between samples and no dwell rule can ever reach its threshold, while on a camera sampled
// four times a second the same number tolerates thirty-two consecutive misses.
//
// It is the same confusion W2-2 found in the availability numbers, pointing the other way:
// do not read a wall-clock duration as a number of observations.
//
// `seen` holds the ids matched this pass; everything else missed it.
func pruneTracks[T trackedObject](tracks map[int64]T, seen map[int64]bool, grace int) {
	if grace < 0 {
		return
	}
	for id, track := range tracks {
		c := track.core()
		if seen[id] {
			c.missed = 0
			continue
		}
		c.missed++
		if c.missed > grace {
			delete(tracks, id)
		}
	}
}
