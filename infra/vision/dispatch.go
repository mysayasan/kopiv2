package vision

import (
	"context"
	"io"
	"strings"
)

type DispatchDetector struct {
	object      Detector
	motion      Detector
	motionTypes map[string]bool
}

type DispatchDetectorOptions struct {
	Object      Detector
	Motion      Detector
	MotionTypes []string
}

func NewDispatchDetector(opts DispatchDetectorOptions) *DispatchDetector {
	motionTypes := map[string]bool{}
	for _, value := range opts.MotionTypes {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			motionTypes[value] = true
		}
	}
	return &DispatchDetector{
		object:      opts.Object,
		motion:      opts.Motion,
		motionTypes: motionTypes,
	}
}

func (d *DispatchDetector) Detect(ctx context.Context, frame Frame, rules []DetectionRule) ([]Detection, error) {
	objectRules := make([]DetectionRule, 0, len(rules))
	motionRules := make([]DetectionRule, 0, len(rules))
	for _, rule := range rules {
		detectionType := strings.ToLower(strings.TrimSpace(rule.DetectionType))
		if d.motionTypes[detectionType] {
			motionRules = append(motionRules, rule)
			continue
		}
		objectRules = append(objectRules, rule)
	}

	detections := make([]Detection, 0)
	if len(objectRules) > 0 && d.object != nil {
		objectDetections, err := d.object.Detect(ctx, frame, objectRules)
		if err != nil {
			return nil, err
		}
		detections = append(detections, objectDetections...)
	}
	if len(motionRules) > 0 && d.motion != nil {
		motionDetections, err := d.motion.Detect(ctx, frame, motionRules)
		if err != nil {
			return nil, err
		}
		detections = append(detections, motionDetections...)
	}
	return detections, nil
}

func (d *DispatchDetector) Close() error {
	var result error
	if closer, ok := d.object.(io.Closer); ok {
		result = closer.Close()
	}
	if closer, ok := d.motion.(io.Closer); ok {
		if err := closer.Close(); result == nil {
			result = err
		}
	}
	return result
}
