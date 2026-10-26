package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

var target = source.Target{Namespace: "db", Instance: "orders"}

func testPod(name, instance, component string, limits ...map[string]any) *unstructured.Unstructured {
	containers := []any{}
	for _, l := range limits {
		containers = append(containers, map[string]any{"name": "c", "resources": map[string]any{"limits": l}})
	}
	p := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"containers": containers}}}
	p.SetAPIVersion("v1")
	p.SetKind("Pod")
	p.SetNamespace("db")
	p.SetName(name)
	p.SetLabels(map[string]string{instanceLabel: instance, componentLabel: component})
	return p
}

func fakeKube(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{podGVR: "PodList"}, objects...)
}

// fakePrometheus answers instant queries with the vector and range queries
// with one series per pod, recording the queries.
func fakePrometheus(t *testing.T, vector string, queries *[]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*queries = append(*queries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":` + vector + `}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"pod":"orders-0"},"values":[[60,"0.5"],[120,"0.7"]]},
			{"metric":{"pod":"orders-1"},"values":[[60,"0.2"],[120,"0.3"]]}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

var window = source.Window{Start: time.Unix(0, 0), End: time.Unix(120, 0), Step: time.Minute}

func TestPanelsNeedPodsWithContainerMetrics(t *testing.T) {
	var queries []string
	withMetrics := New(fakePrometheus(t, `[{"metric":{},"value":[1,"3"]}]`, &queries), fakeKube(testPod("orders-0", "orders", "engine")))
	got, err := withMetrics.Panels(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, []string{"pod-cpu", "pod-memory"}, []string{got[0].ID, got[1].ID})
	assert.Equal(t, []string{`count(container_memory_working_set_bytes{namespace="db",pod=~"orders-0",container!="",container!="POD"})`}, queries)

	withoutMetrics := New(fakePrometheus(t, `[]`, &queries), fakeKube(testPod("orders-0", "orders", "engine")))
	got, err = withoutMetrics.Panels(context.Background(), target)
	require.NoError(t, err)
	assert.Empty(t, got, "a Prometheus that doesn't scrape kubelets has nothing to chart")

	queries = nil
	withoutPods := New(fakePrometheus(t, `[{"metric":{},"value":[1,"3"]}]`, &queries), fakeKube(testPod("other-0", "other", "engine")))
	got, err = withoutPods.Panels(context.Background(), target)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, queries, "without pods there is nothing to ask Prometheus")
}

func TestQueryRangeScopesToTheInstancePodsAndAddsTheLimit(t *testing.T) {
	var queries []string
	u := New(fakePrometheus(t, `[]`, &queries), fakeKube(
		testPod("orders-0", "orders", "engine", map[string]any{"cpu": "1", "memory": "4Gi"}, map[string]any{"cpu": "500m"}),
		testPod("orders-1", "orders", "engine", map[string]any{"cpu": "1500m", "memory": "4Gi"}),
		testPod("orders-2.x", "other", "engine", map[string]any{"cpu": "8"}),
	))

	series, err := u.QueryRange(context.Background(), target, "pod-cpu", window)
	require.NoError(t, err)
	assert.Equal(t, []string{
		`sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="db",pod=~"orders-0|orders-1",container!="",container!="POD"}[120s]))`,
	}, queries)
	require.Len(t, series, 3)
	limit := series[2]
	assert.Equal(t, source.Series{Name: "limit", Kind: source.SeriesLimit, Points: []source.Point{{T: 60000, V: ptr(1.5)}, {T: 120000, V: ptr(1.5)}}}, limit,
		"pods with the same limit share one line spanning the usage samples")

	_, err = u.QueryRange(context.Background(), target, "pod-disk", window)
	assert.ErrorIs(t, err, ErrUnknownPanel)
}

func TestLimitLinesPerComponentOrPod(t *testing.T) {
	times := []int64{1}
	gi := float64(1 << 30)
	pods := []pod{
		{name: "orders-cfg-0", component: "config", limits: map[string]float64{"memory": gi}},
		{name: "orders-cfg-1", component: "config", limits: map[string]float64{"memory": gi}},
		{name: "orders-rs0-0", component: "engine", limits: map[string]float64{"memory": 4 * gi}},
		{name: "orders-rs0-1", component: "engine", limits: map[string]float64{"memory": 8 * gi}},
		{name: "orders-mongos-0", component: "proxy", limits: map[string]float64{}},
	}
	var names []string
	for _, s := range limitSeries(pods, "memory", times) {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"limit · config", "limit · orders-rs0-0", "limit · orders-rs0-1"}, names,
		"pods without a limit get no line")

	assert.Empty(t, limitSeries(pods, "cpu", times), "no pod has a CPU limit")
	assert.Empty(t, limitSeries(pods, "memory", nil), "no usage samples, nothing to span")
}

func TestPodSelectorQuotesPodNames(t *testing.T) {
	assert.Equal(t, `namespace="db",pod=~"orders\\.0|orders-1"`, podSelector("db", []pod{{name: "orders.0"}, {name: "orders-1"}}))
}

func TestPanelsReportPrometheusErrors(t *testing.T) {
	broken := New((&url.URL{Scheme: "http", Host: "127.0.0.1:1"}).String(), fakeKube(testPod("orders-0", "orders", "engine")))
	_, err := broken.Panels(context.Background(), target)
	assert.Error(t, err)
}

func ptr(v float64) *float64 { return &v }
