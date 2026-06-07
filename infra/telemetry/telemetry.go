package telemetry

// APIRequestMetric is one completed API request observation.
type APIRequestMetric struct {
	AppName    string
	Method     string
	Path       string
	StatusCode int
	DurationMs int64
}

// APIRecorder records completed API request telemetry.
type APIRecorder interface {
	ObserveAPIRequest(metric APIRequestMetric)
}

type noopRecorder struct{}

// NewNoopRecorder returns a recorder that safely ignores all observations.
func NewNoopRecorder() APIRecorder {
	return noopRecorder{}
}

func (noopRecorder) ObserveAPIRequest(APIRequestMetric) {}
