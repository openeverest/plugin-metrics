package promapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

// Metadata describes a metric family, as /api/v1/targets/metadata returns it.
type Metadata struct {
	Metric string `json:"metric"`
	Type   string `json:"type"`
	Help   string `json:"help"`
}

// Catalog matches metadata families to the series names that exist; names no
// family claims are listed with an unknown type.
func Catalog(names []string, metadata []Metadata) []source.Metric {
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
func resolveFamily(family Metadata, present map[string]bool) (source.Metric, []string, bool) {
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

// ExploreQuery charts a metric the way its type suggests, one series per
// value of groupBy: counters as a rate, histograms as p99, summaries as the
// average, gauges as they are.
func ExploreQuery(metric source.Metric, groupBy string) (query, unit string, err error) {
	if !source.ValidMetricName(metric.Name) {
		return "", "", fmt.Errorf("invalid metric name %q", metric.Name)
	}
	name := metric.Name
	sumBy := "sum by (" + groupBy + ")"
	bytes := strings.Contains(name, "_bytes")

	switch metric.Type {
	case source.MetricCounter:
		query = fmt.Sprintf("%s (rate(%s{%s}[%s]))", sumBy, name, SelectorPlaceholder, RatePlaceholder)
		if bytes {
			return query, "Bps", nil
		}
		return query, "ops", nil
	case source.MetricHistogram:
		query = fmt.Sprintf("histogram_quantile(0.99, sum by (le, %s) (rate(%s_bucket{%s}[%s])))",
			groupBy, name, SelectorPlaceholder, RatePlaceholder)
		query, unit = inMilliseconds(name, query)
		return query, unit, nil
	case source.MetricSummary:
		query = fmt.Sprintf("%[1]s (rate(%[2]s_sum{%[3]s}[%[4]s])) / %[1]s (rate(%[2]s_count{%[3]s}[%[4]s]))",
			sumBy, name, SelectorPlaceholder, RatePlaceholder)
		query, unit = inMilliseconds(name, query)
		return query, unit, nil
	case source.MetricGauge, source.MetricUnknown:
		query = fmt.Sprintf("%s (%s{%s})", sumBy, name, SelectorPlaceholder)
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
