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
