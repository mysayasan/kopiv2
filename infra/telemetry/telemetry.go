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

// Labels are the dimensions of one metric series. Keep the value set BOUNDED — every
// distinct combination is a separate series held in memory forever. Camera ids and
// outcome enums are fine; error strings, file paths and timestamps are not.
type Labels map[string]string

// Metrics records app-defined runtime metrics: the numbers an operator needs to
// diagnose a site they cannot log into. It is deliberately small and untyped, so an app
// can add a metric without touching shared infra.
//
// Implementations must be safe for concurrent use and must never block — these are
// called from hot paths (per frame, per segment, per delivery).
type Metrics interface {
	// Describe attaches help text to a metric name. Optional, but it is what makes a
	// scrape readable by someone who did not write the code. Call it once at startup.
	Describe(name string, help string)
	// Inc adds one to a counter.
	Inc(name string, labels Labels)
	// Add adds delta to a counter.
	Add(name string, labels Labels, delta float64)
	// Set sets a gauge to value.
	Set(name string, labels Labels, value float64)
	// Observe records a value into a histogram. Durations are in milliseconds.
	Observe(name string, labels Labels, value float64)
}

// Recorder records all shared runtime telemetry.
type Recorder interface {
	APIRecorder
	CoordinationRecorder
	Metrics
}

type noopRecorder struct{}

// NewNoopRecorder returns a recorder that safely ignores all observations. It is what an
// app gets when telemetry is disabled, so instrumentation call sites never need a nil check.
func NewNoopRecorder() Recorder {
	return noopRecorder{}
}

func (noopRecorder) ObserveAPIRequest(APIRequestMetric) {}

func (noopRecorder) ObserveCoordination(CoordinationMetric) {}

func (noopRecorder) Describe(string, string)          {}
func (noopRecorder) Inc(string, Labels)               {}
func (noopRecorder) Add(string, Labels, float64)      {}
func (noopRecorder) Set(string, Labels, float64)      {}
func (noopRecorder) Observe(string, Labels, float64)  {}
