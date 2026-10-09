package prometheus

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

// componentLabel is the pod target label providers set so series split by component.
const componentLabel = "core_openeverest_io_component"

var _ source.Explorer = (*Source)(nil)

type targetMetadata struct {
	Metric string `json:"metric"`
	Type   string `json:"type"`
	Help   string `json:"help"`
}

// ListMetrics implements source.Explorer: the metric families the instance's
// scrape targets expose, with the series name to chart for each.
func (s *Source) ListMetrics(ctx context.Context, target source.Target) ([]source.Metric, error) {
	jobs, err := s.jobs(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, errNotMonitored
	}
	selector := "{" + scopeSelector(target.Namespace, jobs) + "}"

	var names []string
	if err := s.getJSON(ctx, "/api/v1/label/__name__/values", url.Values{"match[]": {selector}}, &names); err != nil {
		return nil, err
	}
	var metadata []targetMetadata
	if err := s.getJSON(ctx, "/api/v1/targets/metadata", url.Values{"match_target": {selector}}, &metadata); err != nil {
		return nil, err
	}
	return catalog(names, metadata), nil
}

// catalog matches metadata families to the series that exist; series no family
// claims are listed with an unknown type.
func catalog(names []string, metadata []targetMetadata) []source.Metric {
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
	}
	claimed := map[string]bool{}
	byName := map[string]source.Metric{}
	for _, family := range metadata {
		metric, series, ok := resolveFamily(family, present)
		if !ok {
			continue
		}
		for _, name := range series {
			claimed[name] = true
		}
		if _, seen := byName[metric.Name]; !seen {
			byName[metric.Name] = metric
		}
	}
	for _, name := range names {
		if !claimed[name] && !isScrapeSeries(name) {
			byName[name] = source.Metric{Name: name, Type: source.MetricUnknown}
		}
	}

	metrics := make([]source.Metric, 0, len(byName))
	for _, metric := range byName {
		metrics = append(metrics, metric)
	}
	slices.SortFunc(metrics, func(a, b source.Metric) int { return strings.Compare(a.Name, b.Name) })
	return metrics
}

// resolveFamily returns the metric to chart for a family and the series it covers.
// Counter families may be named with or without _total depending on the scrape format.
func resolveFamily(family targetMetadata, present map[string]bool) (source.Metric, []string, bool) {
	metric := source.Metric{Name: family.Metric, Type: family.Type, Help: family.Help}
	switch family.Type {
	case source.MetricCounter:
		for _, name := range []string{family.Metric, family.Metric + "_total"} {
			if present[name] {
				metric.Name = name
				return metric, []string{name}, true
			}
		}
		return metric, nil, false
	case source.MetricHistogram:
		parts := []string{family.Metric + "_bucket", family.Metric + "_sum", family.Metric + "_count"}
		return metric, parts, present[parts[0]]
	case source.MetricSummary:
		parts := []string{family.Metric, family.Metric + "_sum", family.Metric + "_count"}
		return metric, parts, present[parts[1]] && present[parts[2]]
	case source.MetricGauge:
		return metric, []string{family.Metric}, present[family.Metric]
	default:
		metric.Type = source.MetricUnknown
		return metric, []string{family.Metric}, present[family.Metric]
	}
}

// isScrapeSeries reports the series Prometheus records about the scrape itself.
func isScrapeSeries(name string) bool {
	return name == "up" || strings.HasPrefix(name, "scrape_")
}

// ExploreMetric implements source.Explorer.
func (s *Source) ExploreMetric(ctx context.Context, target source.Target, metric source.Metric, window source.Window) (source.Exploration, error) {
	query, unit, err := exploreQuery(metric)
	if err != nil {
		return source.Exploration{}, err
	}
	series, err := s.QueryRange(ctx, target, query, window)
	if err != nil {
		return source.Exploration{}, err
	}
	return source.Exploration{Unit: unit, Series: series}, nil
}

// exploreQuery charts a metric the way its type suggests: counters as a rate,
// histograms as p99, summaries as the average, gauges as they are.
func exploreQuery(metric source.Metric) (query, unit string, err error) {
	if !source.ValidMetricName(metric.Name) {
		return "", "", fmt.Errorf("invalid metric name %q", metric.Name)
	}
	name := metric.Name
	sumBy := "sum by (" + componentLabel + ")"
	bytes := strings.Contains(name, "_bytes")

	switch metric.Type {
	case source.MetricCounter:
		query = fmt.Sprintf("%s (rate(%s{%s}[%s]))", sumBy, name, selectorPlaceholder, ratePlaceholder)
		if bytes {
			return query, "Bps", nil
		}
		return query, "ops", nil
	case source.MetricHistogram:
		query = fmt.Sprintf("histogram_quantile(0.99, sum by (le, %s) (rate(%s_bucket{%s}[%s])))",
			componentLabel, name, selectorPlaceholder, ratePlaceholder)
		query, unit = inMilliseconds(name, query)
		return query, unit, nil
	case source.MetricSummary:
		query = fmt.Sprintf("%[1]s (rate(%[2]s_sum{%[3]s}[%[4]s])) / %[1]s (rate(%[2]s_count{%[3]s}[%[4]s]))",
			sumBy, name, selectorPlaceholder, ratePlaceholder)
		query, unit = inMilliseconds(name, query)
		return query, unit, nil
	case source.MetricGauge, source.MetricUnknown:
		query = fmt.Sprintf("%s (%s{%s})", sumBy, name, selectorPlaceholder)
		if bytes {
			return query, "bytes", nil
		}
		return query, "", nil
	default:
		return "", "", fmt.Errorf("invalid metric type %q", metric.Type)
	}
}

// inMilliseconds converts durations named in seconds, the Prometheus convention,
// to milliseconds; other durations keep the exporter's own unit.
func inMilliseconds(name, query string) (string, string) {
	if strings.HasSuffix(name, "_seconds") {
		return "1000 * (" + query + ")", "ms"
	}
	return query, ""
}
