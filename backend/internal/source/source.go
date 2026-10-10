// Package source defines how the plugin reads metrics from a monitoring system.
// Each system (Prometheus today, PMM later) implements Source. Dashboards hold
// one query per source type, so adding a system needs no frontend change.
package source

import (
	"context"
	"regexp"
	"time"
)

// Reasons a source does not serve an instance; the frontend maps them to messages.
const (
	ReasonNotEnabled  = "notEnabled"
	ReasonUnavailable = "unavailable"
)

// Target is the instance whose metrics are read. Callers authorize the user
// for it before passing it to a source.
type Target struct {
	Namespace string
	Instance  string
}

// Window is the time range and resolution of a query.
type Window struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Status reports whether a source collects metrics for a target.
type Status struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// Point is one sample; V is nil when the source has no finite value.
type Point struct {
	T int64    `json:"t"` // Unix milliseconds.
	V *float64 `json:"v"`
}

// Series is one line on a chart.
type Series struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

// Source is a monitoring system the plugin can read from.
type Source interface {
	// Type keys this source's queries in dashboard panels, e.g. "prometheus".
	Type() string
	Status(ctx context.Context, target Target) (Status, error)
	// QueryRange runs a dashboard query scoped to the target.
	QueryRange(ctx context.Context, target Target, query string, window Window) ([]Series, error)
	// ValidateQuery rejects a dashboard query this source could not run safely.
	ValidateQuery(query string) error
}

// Metric types a source reports; they decide how a metric is charted.
const (
	MetricCounter   = "counter"
	MetricGauge     = "gauge"
	MetricHistogram = "histogram"
	MetricSummary   = "summary"
	MetricUnknown   = "unknown"
)

// Metric is one of the target's raw metrics a user can chart.
type Metric struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Help string `json:"help,omitempty"`
}

var metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// ValidMetricName reports a Prometheus-style metric name, which cannot carry
// query syntax into the query it is placed in.
func ValidMetricName(name string) bool {
	return metricNamePattern.MatchString(name)
}

// Exploration is a raw metric charted the way its type suggests.
type Exploration struct {
	Unit   string   `json:"unit"`
	Series []Series `json:"series"`
}

// Explorer is implemented by sources that can list a target's raw metrics and
// chart any of them outside the dashboard.
type Explorer interface {
	ListMetrics(ctx context.Context, target Target) ([]Metric, error)
	ExploreMetric(ctx context.Context, target Target, metric Metric, window Window) (Exploration, error)
}
