package prometheus

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// maxSeriesPerMetric bounds how many distinct label combinations one metric name may
// hold. Every series lives in memory for the life of the process, so an accidentally
// unbounded label (an error string, a file path) would be a slow memory leak on a box
// that is meant to run for months. Past the cap, new series are dropped and the metric
// is marked truncated, which is visible in the scrape — a silently short metric would be
// worse than a loud one.
const maxSeriesPerMetric = 500

type metricKind int

const (
	kindCounter metricKind = iota
	kindGauge
	kindHistogram
)

func (k metricKind) String() string {
	switch k {
	case kindGauge:
		return "gauge"
	case kindHistogram:
		return "histogram"
	default:
		return "counter"
	}
}

// customSeries is one label combination of one metric.
type customSeries struct {
	labels  telemetry.Labels
	value   float64  // counter total or gauge value
	buckets []uint64 // histogram only
	count   uint64   // histogram only
	sum     float64  // histogram only
}

// customMetric is one metric name: its kind, help text, and all its label series.
type customMetric struct {
	kind      metricKind
	help      string
	series    map[string]*customSeries
	truncated bool
}

// Describe attaches help text to a metric name. Safe to call before or after the metric
// is first observed.
func (r *Recorder) Describe(name string, help string) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.custom == nil {
		r.custom = map[string]*customMetric{}
	}
	if m := r.custom[name]; m != nil {
		m.help = help
		return
	}
	// Kind is set on first observation; a described-but-never-observed metric is not
	// emitted at all, so a placeholder here would be a lie.
	r.help[name] = help
}

// Inc adds one to a counter.
func (r *Recorder) Inc(name string, labels telemetry.Labels) {
	r.Add(name, labels, 1)
}

// Add adds delta to a counter.
func (r *Recorder) Add(name string, labels telemetry.Labels, delta float64) {
	r.withSeries(name, labels, kindCounter, func(s *customSeries) {
		s.value += delta
	})
}

// Set sets a gauge to value.
func (r *Recorder) Set(name string, labels telemetry.Labels, value float64) {
	r.withSeries(name, labels, kindGauge, func(s *customSeries) {
		s.value = value
	})
}

// Observe records a value into a histogram.
func (r *Recorder) Observe(name string, labels telemetry.Labels, value float64) {
	r.withSeries(name, labels, kindHistogram, func(s *customSeries) {
		if s.buckets == nil {
			s.buckets = make([]uint64, len(r.buckets))
		}
		s.count++
		s.sum += value
		for i, bucket := range r.buckets {
			if value <= bucket {
				s.buckets[i]++
			}
		}
	})
}

// withSeries resolves (creating if needed) the series for name+labels and applies fn.
// A metric name is bound to the kind it was first observed with; a later observation of
// a different kind is dropped rather than corrupting the exposition (a name cannot be
// both a counter and a gauge in Prometheus).
func (r *Recorder) withSeries(name string, labels telemetry.Labels, kind metricKind, fn func(*customSeries)) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.custom == nil {
		r.custom = map[string]*customMetric{}
	}
	metric := r.custom[name]
	if metric == nil {
		metric = &customMetric{kind: kind, help: r.help[name], series: map[string]*customSeries{}}
		r.custom[name] = metric
	}
	if metric.kind != kind {
		return
	}

	key := seriesKey(labels)
	entry := metric.series[key]
	if entry == nil {
		if len(metric.series) >= maxSeriesPerMetric {
			metric.truncated = true
			return
		}
		entry = &customSeries{labels: cloneLabels(labels)}
		metric.series[key] = entry
	}
	fn(entry)
}

// seriesKey renders a label set into a stable, order-independent identity.
func seriesKey(labels telemetry.Labels) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('\x00')
		b.WriteString(labels[name])
		b.WriteByte('\x00')
	}
	return b.String()
}

func cloneLabels(labels telemetry.Labels) telemetry.Labels {
	if len(labels) == 0 {
		return nil
	}
	out := make(telemetry.Labels, len(labels))
	for name, value := range labels {
		out[name] = value
	}
	return out
}

// customLabels renders a label set in Prometheus exposition syntax, with an optional
// extra `le` bucket bound appended.
func customLabels(labels telemetry.Labels, le string) string {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names)+1)
	for _, name := range names {
		parts = append(parts, name+`="`+escapeLabel(labels[name])+`"`)
	}
	if le != "" {
		parts = append(parts, `le="`+escapeLabel(le)+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// collectCustom appends every app-defined metric in Prometheus text format. The caller
// holds r.mu.
func (r *Recorder) collectCustom(b *strings.Builder) {
	if len(r.custom) == 0 {
		return
	}
	names := make([]string, 0, len(r.custom))
	for name := range r.custom {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		metric := r.custom[name]
		if len(metric.series) == 0 {
			continue
		}
		if help := strings.TrimSpace(metric.help); help != "" {
			fmt.Fprintf(b, "# HELP %s %s\n", name, sanitizeHelp(help))
		}
		fmt.Fprintf(b, "# TYPE %s %s\n", name, metric.kind)

		keys := make([]string, 0, len(metric.series))
		for key := range metric.series {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			entry := metric.series[key]
			switch metric.kind {
			case kindHistogram:
				for i, bucket := range r.buckets {
					fmt.Fprintf(b, "%s_bucket%s %d\n", name, customLabels(entry.labels, formatBucket(bucket)), entry.buckets[i])
				}
				fmt.Fprintf(b, "%s_bucket%s %d\n", name, customLabels(entry.labels, "+Inf"), entry.count)
				fmt.Fprintf(b, "%s_sum%s %g\n", name, customLabels(entry.labels, ""), entry.sum)
				fmt.Fprintf(b, "%s_count%s %d\n", name, customLabels(entry.labels, ""), entry.count)
			default:
				fmt.Fprintf(b, "%s%s %g\n", name, customLabels(entry.labels, ""), entry.value)
			}
		}
		if metric.truncated {
			// Surfaced as a metric, not just a log line: a scrape that is quietly missing
			// series is worse than one that says so.
			fmt.Fprintf(b, "# HELP %s_series_truncated Label cardinality exceeded %d; new series are being dropped.\n", name, maxSeriesPerMetric)
			fmt.Fprintf(b, "# TYPE %s_series_truncated gauge\n", name)
			fmt.Fprintf(b, "%s_series_truncated 1\n", name)
		}
	}
}

// sanitizeHelp keeps help text on one line so the exposition stays parseable.
func sanitizeHelp(help string) string {
	help = strings.ReplaceAll(help, `\`, `\\`)
	help = strings.ReplaceAll(help, "\n", " ")
	return help
}

var _ telemetry.Metrics = (*Recorder)(nil)
