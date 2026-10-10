package prometheus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

var target = source.Target{Namespace: "db", Instance: "vectors"}

func podMonitor(name string, labels map[string]string) *unstructured.Unstructured {
	pm := &unstructured.Unstructured{}
	pm.SetAPIVersion("monitoring.coreos.com/v1")
	pm.SetKind("PodMonitor")
	pm.SetNamespace("db")
	pm.SetName(name)
	pm.SetLabels(labels)
	return pm
}

var instanceLabels = map[string]string{"app.kubernetes.io/managed-by": "everest", "app.kubernetes.io/instance": "vectors"}

func fakeKube(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			podMonitorGVR:     "PodMonitorList",
			serviceMonitorGVR: "ServiceMonitorList",
			serviceGVR:        "ServiceList",
		}, objects...)
}

func TestStatus(t *testing.T) {
	t.Run("monitored when the provider created a PodMonitor", func(t *testing.T) {
		s := New("http://prometheus", fakeKube(podMonitor("vectors-metrics", instanceLabels)))
		status, err := s.Status(context.Background(), target)
		require.NoError(t, err)
		assert.Equal(t, source.Status{Enabled: true}, status)
	})

	t.Run("ignores PodMonitors of other instances and unmanaged ones", func(t *testing.T) {
		s := New("http://prometheus", fakeKube(
			podMonitor("other", map[string]string{"app.kubernetes.io/managed-by": "everest", "app.kubernetes.io/instance": "other"}),
			podMonitor("hand-made", map[string]string{"app.kubernetes.io/instance": "vectors"}),
		))
		status, err := s.Status(context.Background(), target)
		require.NoError(t, err)
		assert.Equal(t, source.Status{Reason: source.ReasonNotEnabled}, status)
	})

	t.Run("unavailable without the Prometheus Operator", func(t *testing.T) {
		kube := fakeKube()
		for _, gvr := range []schema.GroupVersionResource{podMonitorGVR, serviceMonitorGVR} {
			kube.PrependReactor("list", gvr.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewNotFound(gvr.GroupResource(), "")
			})
		}
		status, err := New("http://prometheus", kube).Status(context.Background(), target)
		require.NoError(t, err)
		assert.Equal(t, source.Status{Reason: source.ReasonUnavailable}, status)
	})
}

func TestRenderQuery(t *testing.T) {
	got, err := renderQuery(`sum(rate(x{${selector},a="b"}[${rate}]))`, "db", []string{"db/vectors.metrics", "db/other"}, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, `sum(rate(x{namespace="db",job=~"db/vectors\\.metrics|db/other",a="b"}[120s]))`, got)

	got, err = renderQuery(`rate(x{${selector}}[${rate}])`, "db", []string{"db/vectors"}, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, `rate(x{namespace="db",job=~"db/vectors"}[300s])`, got)

	_, err = renderQuery(`sum(x)`, "db", []string{"db/vectors"}, time.Minute)
	assert.ErrorContains(t, err, "must be scoped")
}

func TestQueryRange(t *testing.T) {
	var gotQuery url.Values
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query_range", r.URL.Path)
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"core_openeverest_io_component":"proxy"},"values":[[1700000000,"0.5"],[1700000030,"NaN"]]},
			{"metric":{"__name__":"up","core_openeverest_io_component":"dataNode"},"values":[[1700000000.5,"2"]]}
		]}}`))
	}))
	defer prom.Close()

	s := New(prom.URL, fakeKube(podMonitor("vectors-metrics", instanceLabels)))
	window := source.Window{Start: time.Unix(1700000000, 0), End: time.Unix(1700003600, 0), Step: 30 * time.Second}
	series, err := s.QueryRange(context.Background(), target, `sum by (c) (x{${selector}})`, window)
	require.NoError(t, err)

	assert.Equal(t, `sum by (c) (x{namespace="db",job=~"db/vectors-metrics"})`, gotQuery.Get("query"))
	assert.Equal(t, "1700000000", gotQuery.Get("start"))
	assert.Equal(t, "1700003600", gotQuery.Get("end"))
	assert.Equal(t, "30", gotQuery.Get("step"))

	require.Len(t, series, 2)
	assert.Equal(t, "dataNode", series[0].Name)
	assert.Equal(t, int64(1700000000500), series[0].Points[0].T)
	assert.Equal(t, "proxy", series[1].Name)
	require.NotNil(t, series[1].Points[0].V)
	assert.Equal(t, 0.5, *series[1].Points[0].V)
	assert.Nil(t, series[1].Points[1].V, "NaN must not reach JSON")
}

func TestQueryRangeErrors(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer prom.Close()
	window := source.Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: time.Minute}

	_, err := New(prom.URL, fakeKube(podMonitor("vectors-metrics", instanceLabels))).
		QueryRange(context.Background(), target, `x{${selector}}`, window)
	assert.ErrorContains(t, err, "parse error")

	_, err = New(prom.URL, fakeKube()).QueryRange(context.Background(), target, `x{${selector}}`, window)
	assert.ErrorIs(t, err, errNotMonitored)
}
