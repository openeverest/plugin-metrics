package prometheus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	metadata := []targetMetadata{
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
	}, catalog(names, metadata))
}

func TestExploreQuery(t *testing.T) {
	tests := []struct {
		metric source.Metric
		query  string
		unit   string
	}{
		{
			source.Metric{Name: "milvus_proxy_req_count", Type: "counter"},
			`sum by (core_openeverest_io_component) (rate(milvus_proxy_req_count{${selector}}[${rate}]))`, "ops",
		},
		{
			source.Metric{Name: "milvus_proxy_receive_bytes_count", Type: "counter"},
			`sum by (core_openeverest_io_component) (rate(milvus_proxy_receive_bytes_count{${selector}}[${rate}]))`, "Bps",
		},
		{
			source.Metric{Name: "milvus_proxy_sq_latency", Type: "histogram"},
			`histogram_quantile(0.99, sum by (le, core_openeverest_io_component) (rate(milvus_proxy_sq_latency_bucket{${selector}}[${rate}])))`, "",
		},
		{
			source.Metric{Name: "go_gc_duration_seconds", Type: "summary"},
			`1000 * (sum by (core_openeverest_io_component) (rate(go_gc_duration_seconds_sum{${selector}}[${rate}])) / sum by (core_openeverest_io_component) (rate(go_gc_duration_seconds_count{${selector}}[${rate}])))`, "ms",
		},
		{
			source.Metric{Name: "process_resident_memory_bytes", Type: "gauge"},
			`sum by (core_openeverest_io_component) (process_resident_memory_bytes{${selector}})`, "bytes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.metric.Name, func(t *testing.T) {
			query, unit, err := exploreQuery(tt.metric)
			require.NoError(t, err)
			assert.Equal(t, tt.query, query)
			assert.Equal(t, tt.unit, unit)
		})
	}

	_, _, err := exploreQuery(source.Metric{Name: `x{job="other"}`, Type: "gauge"})
	assert.Error(t, err, "a name must not smuggle in its own selector")
}

func TestListMetricsIsScopedToTheInstance(t *testing.T) {
	var matches []string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/label/__name__/values":
			matches = append(matches, r.URL.Query().Get("match[]"))
			_, _ = w.Write([]byte(`{"status":"success","data":["process_open_fds"]}`))
		case "/api/v1/targets/metadata":
			matches = append(matches, r.URL.Query().Get("match_target"))
			_, _ = w.Write([]byte(`{"status":"success","data":[{"metric":"process_open_fds","type":"gauge","help":"fds"}]}`))
		}
	}))
	defer prom.Close()

	metrics, err := New(prom.URL, fakeKube(podMonitor("vectors-metrics", instanceLabels))).ListMetrics(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, []source.Metric{{Name: "process_open_fds", Type: "gauge", Help: "fds"}}, metrics)
	scope := `{namespace="db",job=~"db/vectors-metrics"}`
	assert.Equal(t, []string{scope, scope}, matches)
}

func TestExploreMetricRunsTheScopedQuery(t *testing.T) {
	var gotQuery string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer prom.Close()

	window := source.Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: 30 * time.Second}
	got, err := New(prom.URL, fakeKube(podMonitor("vectors-metrics", instanceLabels))).
		ExploreMetric(context.Background(), target, source.Metric{Name: "process_open_fds", Type: "gauge"}, window)
	require.NoError(t, err)
	assert.Equal(t, `sum by (core_openeverest_io_component) (process_open_fds{namespace="db",job=~"db/vectors-metrics"})`, gotQuery)
	assert.Equal(t, source.Exploration{Unit: "", Series: []source.Series{}}, got)
}
