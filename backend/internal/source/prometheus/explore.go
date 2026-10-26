package prometheus

import (
	"context"
	"net/url"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/promapi"
)

// componentLabel is the pod target label providers set so series split by component.
const componentLabel = "core_openeverest_io_component"

var _ source.Explorer = (*Source)(nil)

// ListMetrics implements source.Explorer: the metric families the instance's
// scrape targets expose, with the series name to chart for each.
func (s *Source) ListMetrics(ctx context.Context, target source.Target) ([]source.Metric, error) {
	selector, err := s.selector(ctx, target)
	if err != nil {
		return nil, err
	}
	match := "{" + selector + "}"

	var names []string
	if err := s.api.GetJSON(ctx, "/api/v1/label/__name__/values", url.Values{"match[]": {match}}, &names); err != nil {
		return nil, err
	}
	// Metadata only adds types and help; VictoriaMetrics doesn't serve it, so list untyped names.
	var metadata []promapi.Metadata
	_ = s.api.GetJSON(ctx, "/api/v1/targets/metadata", url.Values{"match_target": {match}}, &metadata)
	return promapi.Catalog(names, metadata), nil
}

// ExploreMetric implements source.Explorer.
func (s *Source) ExploreMetric(ctx context.Context, target source.Target, metric source.Metric, window source.Window) (source.Exploration, error) {
	query, unit, err := promapi.ExploreQuery(metric, componentLabel)
	if err != nil {
		return source.Exploration{}, err
	}
	series, err := s.QueryRange(ctx, target, query, window)
	if err != nil {
		return source.Exploration{}, err
	}
	return source.Exploration{Unit: unit, Series: series}, nil
}
