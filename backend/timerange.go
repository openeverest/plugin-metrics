package main

import (
	"fmt"
	"time"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

type timeRange struct {
	span time.Duration
	step time.Duration
}

// Steps keep every range at a few hundred points per series.
var timeRanges = map[string]timeRange{
	"1h":  {span: time.Hour, step: 30 * time.Second},
	"24h": {span: 24 * time.Hour, step: 5 * time.Minute},
}

// parseWindow aligns the window to the step so that every panel of a range
// shares the same timestamps.
func parseWindow(name string, now time.Time) (source.Window, error) {
	tr, ok := timeRanges[name]
	if !ok {
		return source.Window{}, badRequest(fmt.Sprintf("invalid range %q", name))
	}
	end := now.Truncate(tr.step)
	return source.Window{Start: end.Add(-tr.span), End: end, Step: tr.step}, nil
}
