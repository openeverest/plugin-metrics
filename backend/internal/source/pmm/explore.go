package pmm

import (
	"context"
	"net/url"
	"strings"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/promapi"
)

var _ source.Explorer = (*Source)(nil)

// metricMetadata is one entry of VictoriaMetrics' /api/v1/metadata.
type metricMetadata struct {
	Type string `json:"type"`
	Help string `json:"help"`
}

// ListMetrics implements source.Explorer: the metrics the instance pushes to
// PMM, typed from the metadata VictoriaMetrics keeps, if any.
func (s *Source) ListMetrics(ctx context.Context, target source.Target) ([]source.Metric, error) {
	api, err := s.scopedClient(ctx, target)
	if err != nil {
		return nil, err
	}
	var names []string
	match := url.Values{"match[]": {"{" + scopeSelector(target) + "}"}}
	if err := api.GetJSON(ctx, "/api/v1/label/__name__/values", match, &names); err != nil {
		return nil, err
	}
	// VictoriaMetrics keeps metadata only with -enableMetadata and older
	// versions lack the endpoint; without it metrics are typed by name.
	var byName map[string][]metricMetadata
	_ = api.GetJSON(ctx, "/api/v1/metadata", nil, &byName)

	var metadata []promapi.Metadata
	for _, name := range names {
		if entries := byName[name]; len(entries) > 0 {
			metadata = append(metadata, promapi.Metadata{Metric: name, Type: entries[0].Type, Help: entries[0].Help})
		}
	}
	metrics := promapi.Catalog(names, metadata)
	for i, metric := range metrics {
		if metric.Type == source.MetricUnknown && strings.HasSuffix(metric.Name, "_total") {
			metrics[i].Type = source.MetricCounter
		}
	}
	return metrics, nil
}

// ExploreMetric implements source.Explorer: one series per database pod.
func (s *Source) ExploreMetric(ctx context.Context, target source.Target, metric source.Metric, window source.Window) (source.Exploration, error) {
	query, unit, err := promapi.ExploreQuery(metric, serviceLabel)
	if err != nil {
		return source.Exploration{}, err
	}
	series, err := s.QueryRange(ctx, target, query, window)
	if err != nil {
		return source.Exploration{}, err
	}
	return source.Exploration{Unit: unit, Series: series}, nil
}
