package telemetry

// APIRequestMetric is one completed API request observation.
type APIRequestMetric struct {
	AppName    string
	Method     string
	Path       string
	StatusCode int
	DurationMs int64
}

// CoordinationMetric is one lock/queue coordination observation.
type CoordinationMetric struct {
	AppName  string
	Provider string
	Resource string
	Outcome  string
	WaitMs   int64
}

// APIRecorder records completed API request telemetry.
type APIRecorder interface {
	ObserveAPIRequest(metric APIRequestMetric)
}

// CoordinationRecorder records transaction lock and queue telemetry.
type CoordinationRecorder interface {
	ObserveCoordination(metric CoordinationMetric)
}

// Recorder records all shared runtime telemetry.
type Recorder interface {
	APIRecorder
	CoordinationRecorder
}

type noopRecorder struct{}

// NewNoopRecorder returns a recorder that safely ignores all observations.
func NewNoopRecorder() Recorder {
	return noopRecorder{}
}

func (noopRecorder) ObserveAPIRequest(APIRequestMetric) {}

func (noopRecorder) ObserveCoordination(CoordinationMetric) {}
