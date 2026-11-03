package pmm

import (
	"context"
	"encoding/base64"
	"encoding/pem"
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

var target = source.Target{Namespace: "db", Instance: "orders", MonitoringConfig: "pmm-prod"}

const scopeFilter = `{namespace="db",core_openeverest_io_instance="orders"}`

func monitoringConfig(name string, spec map[string]any) *unstructured.Unstructured {
	mc := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	mc.SetAPIVersion("monitoring.openeverest.io/v1alpha1")
	mc.SetKind("MonitoringConfig")
	mc.SetNamespace("db")
	mc.SetName(name)
	return mc
}

func pmmConfig(pmmURL string, extra map[string]any) *unstructured.Unstructured {
	pmm := map[string]any{"url": pmmURL, "credentialsSecretRef": map[string]any{"name": "pmm-creds"}}
	for key, value := range extra {
		pmm[key] = value
	}
	return monitoringConfig("pmm-prod", map[string]any{"type": "pmm", "pmm": pmm})
}

func secret(name string, data map[string]string) *unstructured.Unstructured {
	encoded := map[string]any{}
	for key, value := range data {
		encoded[key] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	s := &unstructured.Unstructured{Object: map[string]any{"data": encoded}}
	s.SetAPIVersion("v1")
	s.SetKind("Secret")
	s.SetNamespace("db")
	s.SetName(name)
	return s
}

var credentials = secret("pmm-creds", map[string]string{"apiKey": "glsa_token"})

func fakeKube(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			monitoringConfigGVR:      "MonitoringConfigList",
			monitoringDestinationGVR: "MonitoringDestinationList",
			monitoringClassGVR:       "MonitoringClassList",
			secretGVR:                "SecretList",
		}, objects...)
}

// The PMM class is found by the integration fulfilling it, not by its name.
var (
	pmmClass        = monitoringClass("percona-pmm", map[string]any{"providerManaged": map[string]any{"integration": "pmm"}})
	prometheusClass = monitoringClass("prometheus", map[string]any{"extensionManaged": map[string]any{}})
	tokenSecret     = secret("pmm-token", map[string]string{"token": "glsa_destination"})
)

func monitoringClass(name string, spec map[string]any) *unstructured.Unstructured {
	c := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	c.SetAPIVersion("monitoring.openeverest.io/v1alpha1")
	c.SetKind("MonitoringClass")
	c.SetName(name)
	return c
}

func destination(name, class string, extra map[string]any) *unstructured.Unstructured {
	spec := map[string]any{
		"classRef":       map[string]any{"name": class},
		"authentication": map[string]any{"mode": "ServiceAccountToken", "credentialsSecretRef": map[string]any{"name": "pmm-token"}},
	}
	for key, value := range extra {
		spec[key] = value
	}
	d := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	d.SetAPIVersion("monitoring.openeverest.io/v1alpha1")
	d.SetKind("MonitoringDestination")
	d.SetNamespace("db")
	d.SetName(name)
	return d
}

func pmmDestination(pmmURL string, extra map[string]any) *unstructured.Unstructured {
	spec := map[string]any{"parameters": map[string]any{"url": pmmURL}}
	for key, value := range extra {
		spec[key] = value
	}
	return destination("pmm", "percona-pmm", spec)
}

var destinationTarget = source.Target{Namespace: "db", Instance: "orders", MonitoringDestinations: []string{"prometheus", "pmm"}}

func scopeOf(t *testing.T, status source.Status) scope {
	t.Helper()
	require.True(t, status.Enabled)
	sc, ok := status.Scope.(scope)
	require.True(t, ok, "Status must resolve a scope")
	return sc
}

func TestStatus(t *testing.T) {
	t.Run("monitored when the instance references a PMM MonitoringConfig", func(t *testing.T) {
		s := New(fakeKube(pmmConfig("https://pmm.example.com/", nil), credentials))
		status, err := s.Status(context.Background(), target)
		require.NoError(t, err)

		api := scopeOf(t, status).api
		assert.Equal(t, "https://pmm.example.com/prometheus", api.BaseURL)
		assert.Equal(t, "glsa_token", api.Token)
		assert.Equal(t, []string{scopeFilter}, api.ExtraFilters)
		assert.Same(t, s.verifiedHTTP, api.HTTP)
	})

	t.Run("skips TLS verification only when the MonitoringConfig opts out", func(t *testing.T) {
		s := New(fakeKube(pmmConfig("https://pmm.example.com", map[string]any{"verifyTLS": false}), credentials))
		status, err := s.Status(context.Background(), target)
		require.NoError(t, err)
		assert.Same(t, s.insecureHTTP, scopeOf(t, status).api.HTTP)
	})

	t.Run("not enabled without a MonitoringConfig, without reading any", func(t *testing.T) {
		kube := fakeKube()
		unmonitored := target
		unmonitored.MonitoringConfig = ""
		status, err := New(kube).Status(context.Background(), unmonitored)
		require.NoError(t, err)
		assert.Equal(t, source.Status{Reason: source.ReasonNotEnabled}, status)
		assert.Empty(t, kube.Actions())
	})

	t.Run("not enabled for a MonitoringConfig of another type", func(t *testing.T) {
		s := New(fakeKube(monitoringConfig("pmm-prod", map[string]any{"type": "other"})))
		status, err := s.Status(context.Background(), target)
		require.NoError(t, err)
		assert.Equal(t, source.Status{Reason: source.ReasonNotEnabled}, status)
	})

	unavailable := map[string]*dynamicfake.FakeDynamicClient{
		"MonitoringConfig missing": fakeKube(credentials),
		"Secret missing":           fakeKube(pmmConfig("https://pmm.example.com", nil)),
		"Secret without apiKey":    fakeKube(pmmConfig("https://pmm.example.com", nil), secret("pmm-creds", map[string]string{"other": "x"})),
	}
	for name, kube := range unavailable {
		t.Run("unavailable: "+name, func(t *testing.T) {
			status, err := New(kube).Status(context.Background(), target)
			require.NoError(t, err)
			assert.Equal(t, source.Status{Reason: source.ReasonUnavailable}, status)
		})
	}

	t.Run("fails on an unusable PMM URL", func(t *testing.T) {
		_, err := New(fakeKube(pmmConfig("pmm.example.com:443", nil), credentials)).Status(context.Background(), target)
		assert.ErrorContains(t, err, "invalid PMM URL")
	})
}

func TestStatusWithMonitoringDestinations(t *testing.T) {
	t.Run("monitored when the instance sends to a destination of the PMM class", func(t *testing.T) {
		kube := fakeKube(destination("prometheus", "prometheus", nil), prometheusClass,
			pmmDestination("https://pmm.example.com", nil), pmmClass, tokenSecret)
		s := New(kube)
		status, err := s.Status(context.Background(), destinationTarget)
		require.NoError(t, err)

		api := scopeOf(t, status).api
		assert.Equal(t, "https://pmm.example.com/prometheus", api.BaseURL)
		assert.Equal(t, "glsa_destination", api.Token)
		assert.Equal(t, []string{scopeFilter}, api.ExtraFilters)
		assert.Same(t, s.verifiedHTTP, api.HTTP)
	})

	t.Run("skips TLS verification only when the destination opts out", func(t *testing.T) {
		tlsSpec := map[string]any{"tls": map[string]any{"insecureSkipVerify": true}}
		s := New(fakeKube(pmmDestination("https://pmm.example.com", tlsSpec), pmmClass, tokenSecret))
		status, err := s.Status(context.Background(), destinationTarget)
		require.NoError(t, err)
		assert.Same(t, s.insecureHTTP, scopeOf(t, status).api.HTTP)
	})

	notEnabled := map[string]*dynamicfake.FakeDynamicClient{
		"no destination of the PMM class": fakeKube(destination("prometheus", "prometheus", nil), prometheusClass),
		"destinations missing":            fakeKube(pmmClass, tokenSecret),
		"class missing":                   fakeKube(pmmDestination("https://pmm.example.com", nil), tokenSecret),
	}
	for name, kube := range notEnabled {
		t.Run("not enabled: "+name, func(t *testing.T) {
			status, err := New(kube).Status(context.Background(), destinationTarget)
			require.NoError(t, err)
			assert.Equal(t, source.Status{Reason: source.ReasonNotEnabled}, status)
		})
	}

	anonymous := destination("pmm", "percona-pmm", map[string]any{
		"parameters":     map[string]any{"url": "https://pmm.example.com"},
		"authentication": map[string]any{"mode": "Anonymous"},
	})
	badCA := map[string]any{"tls": map[string]any{"ca": map[string]any{"secretKeyRef": map[string]any{"name": "pmm-ca", "key": "ca.crt"}}}}
	unavailable := map[string]*dynamicfake.FakeDynamicClient{
		"token Secret missing":         fakeKube(pmmDestination("https://pmm.example.com", nil), pmmClass),
		"Secret without token":         fakeKube(pmmDestination("https://pmm.example.com", nil), pmmClass, secret("pmm-token", map[string]string{"apiKey": "x"})),
		"unsupported auth mode":        fakeKube(anonymous, pmmClass),
		"CA bundle without a PEM cert": fakeKube(pmmDestination("https://pmm.example.com", badCA), pmmClass, tokenSecret, secret("pmm-ca", map[string]string{"ca.crt": "not a certificate"})),
	}
	for name, kube := range unavailable {
		t.Run("unavailable: "+name, func(t *testing.T) {
			status, err := New(kube).Status(context.Background(), destinationTarget)
			require.NoError(t, err)
			assert.Equal(t, source.Status{Reason: source.ReasonUnavailable}, status)
		})
	}
}

func TestDestinationCAVerifiesThePMMServer(t *testing.T) {
	pmm := httptest.NewTLSServer(http.HandlerFunc(emptyMatrix))
	t.Cleanup(pmm.Close)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pmm.Certificate().Raw})
	tlsSpec := map[string]any{"tls": map[string]any{"ca": map[string]any{"secretKeyRef": map[string]any{"name": "pmm-ca", "key": "ca.crt"}}}}
	s := New(fakeKube(pmmDestination(pmm.URL, tlsSpec), pmmClass, tokenSecret, secret("pmm-ca", map[string]string{"ca.crt": string(caPEM)})))

	_, err := s.QueryRange(context.Background(), destinationTarget, `x{${selector}}`, window)
	require.NoError(t, err, "the destination's CA must verify the PMM server")

	first, err := s.Status(context.Background(), destinationTarget)
	require.NoError(t, err)
	second, err := s.Status(context.Background(), destinationTarget)
	require.NoError(t, err)
	assert.Same(t, scopeOf(t, first).api.HTTP, scopeOf(t, second).api.HTTP, "connections to a PMM server are reused across requests")

	_, err = New(fakeKube(pmmDestination(pmm.URL, nil), pmmClass, tokenSecret)).QueryRange(context.Background(), destinationTarget, `x{${selector}}`, window)
	assert.Error(t, err, "without the CA the self-signed PMM server is not trusted")
}

// fakePMM serves PMM's Prometheus API and records the requests it gets.
type fakePMM struct {
	*httptest.Server
	requests []*http.Request
}

func newFakePMM(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *fakePMM {
	t.Helper()
	pmm := &fakePMM{}
	pmm.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pmm.requests = append(pmm.requests, r)
		handle(w, r)
	}))
	t.Cleanup(pmm.Close)
	return pmm
}

func emptyMatrix(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
}

var window = source.Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: 30 * time.Second}

func TestQueryRangeIsScopedToTheInstance(t *testing.T) {
	pmm := newFakePMM(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"service_name":"db-orders-rs0-0"},"values":[[60,"1"]]}
		]}}`))
	})
	s := New(fakeKube(pmmConfig(pmm.URL, nil), credentials))

	series, err := s.QueryRange(context.Background(), target, `sum by (service_name) (rate(x{${selector}}[${rate}]))`, window)
	require.NoError(t, err)

	require.Len(t, pmm.requests, 1)
	req := pmm.requests[0]
	assert.Equal(t, "/prometheus/api/v1/query_range", req.URL.Path)
	assert.Equal(t, "Bearer glsa_token", req.Header.Get("Authorization"))
	assert.Equal(t, `sum by (service_name) (rate(x{namespace="db",core_openeverest_io_instance="orders"}[120s]))`, req.URL.Query().Get("query"))
	assert.Equal(t, []string{scopeFilter}, req.URL.Query()["extra_filters[]"])
	assert.Equal(t, "orders-rs0-0", series[0].Name, "series are named after the pod, not the PMM service")
}

func TestQueryRangeRejectsUnmonitoredInstances(t *testing.T) {
	unmonitored := target
	unmonitored.MonitoringConfig = ""
	_, err := New(fakeKube()).QueryRange(context.Background(), unmonitored, `x{${selector}}`, window)
	assert.ErrorIs(t, err, errNotMonitored)
}

func TestQueriesReuseTheStatusScope(t *testing.T) {
	pmm := newFakePMM(t, emptyMatrix)
	kube := fakeKube(pmmConfig(pmm.URL, nil), credentials)
	s := New(kube)

	status, err := s.Status(context.Background(), target)
	require.NoError(t, err)
	lookups := len(kube.Actions())
	require.Positive(t, lookups)

	scoped := target
	scoped.Scope = status.Scope
	_, err = s.QueryRange(context.Background(), scoped, `x{${selector}}`, window)
	require.NoError(t, err)
	assert.Len(t, kube.Actions(), lookups, "a scoped query must not read the MonitoringConfig again")
}

func TestListMetrics(t *testing.T) {
	metadata := `{"status":"success","data":{
		"mysql_global_status_threads_connected":[{"type":"gauge","help":"Threads connected"}],
		"other_instances_metric":[{"type":"gauge"}]
	}}`
	tests := map[string]struct {
		metadata func(w http.ResponseWriter)
		want     []source.Metric
	}{
		"typed by metadata": {
			metadata: func(w http.ResponseWriter) { _, _ = w.Write([]byte(metadata)) },
			want: []source.Metric{
				{Name: "mysql_global_status_queries", Type: "unknown"},
				{Name: "mysql_global_status_threads_connected", Type: "gauge", Help: "Threads connected"},
				{Name: "pg_stat_bgwriter_buffers_alloc_total", Type: "counter"},
			},
		},
		"typed by name without metadata": {
			metadata: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"status":"error","error":"unsupported path"}`))
			},
			want: []source.Metric{
				{Name: "mysql_global_status_queries", Type: "unknown"},
				{Name: "mysql_global_status_threads_connected", Type: "unknown"},
				{Name: "pg_stat_bgwriter_buffers_alloc_total", Type: "counter"},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pmm := newFakePMM(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/prometheus/api/v1/label/__name__/values":
					_, _ = w.Write([]byte(`{"status":"success","data":[
						"mysql_global_status_queries","mysql_global_status_threads_connected","pg_stat_bgwriter_buffers_alloc_total","up"
					]}`))
				case "/prometheus/api/v1/metadata":
					tt.metadata(w)
				}
			})
			metrics, err := New(fakeKube(pmmConfig(pmm.URL, nil), credentials)).ListMetrics(context.Background(), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, metrics)

			names := pmm.requests[0].URL.Query()
			assert.Equal(t, scopeFilter, names.Get("match[]"))
			assert.Equal(t, []string{scopeFilter}, names["extra_filters[]"])
		})
	}
}

func TestExploreMetricSplitsByPod(t *testing.T) {
	pmm := newFakePMM(t, emptyMatrix)
	got, err := New(fakeKube(pmmConfig(pmm.URL, nil), credentials)).
		ExploreMetric(context.Background(), target, source.Metric{Name: "mysql_global_status_threads_connected", Type: "gauge"}, window)
	require.NoError(t, err)

	query, err := url.ParseQuery(pmm.requests[0].URL.RawQuery)
	require.NoError(t, err)
	assert.Equal(t, `sum by (service_name) (mysql_global_status_threads_connected{namespace="db",core_openeverest_io_instance="orders"})`, query.Get("query"))
	assert.Equal(t, source.Exploration{Unit: "", Series: []source.Series{}}, got)
}
