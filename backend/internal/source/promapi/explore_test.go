package promapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

func TestCatalog(t *testing.T) {
	names := []string{
		"process_cpu_seconds_total",
		"milvus_proxy_sq_latency_bucket", "milvus_proxy_sq_latency_sum", "milvus_proxy_sq_latency_count",
		"go_gc_duration_seconds", "go_gc_duration_seconds_sum", "go_gc_duration_seconds_count",
		"process_resident_memory_bytes",
		"no_metadata_metric",
		"up", "scrape_duration_seconds",
	}
	metadata := []Metadata{
		// OpenMetrics family name: the series carries _total.
		{Metric: "process_cpu_seconds", Type: "counter", Help: "CPU time"},
		{Metric: "milvus_proxy_sq_latency", Type: "histogram"},
		{Metric: "go_gc_duration_seconds", Type: "summary"},
		{Metric: "process_resident_memory_bytes", Type: "gauge"},
		{Metric: "process_resident_memory_bytes", Type: "gauge"},
		{Metric: "gone_without_series", Type: "gauge"},
	}

	assert.Equal(t, []source.Metric{
		{Name: "go_gc_duration_seconds", Type: "summary"},
		{Name: "milvus_proxy_sq_latency", Type: "histogram"},
		{Name: "no_metadata_metric", Type: "unknown"},
		{Name: "process_cpu_seconds_total", Type: "counter", Help: "CPU time"},
		{Name: "process_resident_memory_bytes", Type: "gauge"},
	}, Catalog(names, metadata))
}

func TestExploreQuery(t *testing.T) {
	tests := []struct {
		metric source.Metric
		query  string
		unit   string
	}{
		{
			source.Metric{Name: "milvus_proxy_req_count", Type: "counter"},
			`sum by (component) (rate(milvus_proxy_req_count{${selector}}[${rate}]))`, "ops",
		},
		{
			source.Metric{Name: "milvus_proxy_receive_bytes_count", Type: "counter"},
			`sum by (component) (rate(milvus_proxy_receive_bytes_count{${selector}}[${rate}]))`, "Bps",
		},
		{
			source.Metric{Name: "milvus_proxy_sq_latency", Type: "histogram"},
			`histogram_quantile(0.99, sum by (le, component) (rate(milvus_proxy_sq_latency_bucket{${selector}}[${rate}])))`, "",
		},
		{
			source.Metric{Name: "go_gc_duration_seconds", Type: "summary"},
			`1000 * (sum by (component) (rate(go_gc_duration_seconds_sum{${selector}}[${rate}])) / sum by (component) (rate(go_gc_duration_seconds_count{${selector}}[${rate}])))`, "ms",
		},
		{
			source.Metric{Name: "process_resident_memory_bytes", Type: "gauge"},
			`sum by (component) (process_resident_memory_bytes{${selector}})`, "bytes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.metric.Name, func(t *testing.T) {
			query, unit, err := ExploreQuery(tt.metric, "component")
			require.NoError(t, err)
			assert.Equal(t, tt.query, query)
			assert.Equal(t, tt.unit, unit)
		})
	}

	_, _, err := ExploreQuery(source.Metric{Name: `x{job="other"}`, Type: "gauge"}, "component")
	assert.Error(t, err, "a name must not smuggle in its own selector")
}
