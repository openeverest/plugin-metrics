// Package source defines how the plugin reads metrics from a monitoring system.
// Each system (Prometheus today, PMM later) implements Source. Dashboards hold
// one query per source type, so adding a system needs no frontend change.
package source

import (
	"context"
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
}
