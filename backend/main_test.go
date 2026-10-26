package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openeverest/plugin-metrics/backend/internal/dashboard"
	"github.com/openeverest/plugin-metrics/backend/internal/resources"
	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

type fakeSource struct {
	typ     string
	status  source.Status
	targets []source.Target
	queries []string
	scopes  []any
}

func (f *fakeSource) Type() string { return f.typ }

func (f *fakeSource) Status(_ context.Context, target source.Target) (source.Status, error) {
	f.targets = append(f.targets, target)
	return f.status, nil
}

func (f *fakeSource) QueryRange(_ context.Context, target source.Target, query string, _ source.Window) ([]source.Series, error) {
	f.queries = append(f.queries, query)
	f.scopes = append(f.scopes, target.Scope)
	return []source.Series{{Name: "proxy", Points: []source.Point{{T: 1}}}}, nil
}

func (f *fakeSource) ValidateQuery(string) error { return nil }

type fakeExplorer struct {
	fakeSource
	explored []source.Metric
}

func (f *fakeExplorer) ListMetrics(_ context.Context, target source.Target) ([]source.Metric, error) {
	f.scopes = append(f.scopes, target.Scope)
	return []source.Metric{{Name: "process_open_fds", Type: source.MetricGauge}}, nil
}

func (f *fakeExplorer) ExploreMetric(_ context.Context, target source.Target, metric source.Metric, _ source.Window) (source.Exploration, error) {
	f.explored = append(f.explored, metric)
	f.scopes = append(f.scopes, target.Scope)
	return source.Exploration{Unit: "bytes"}, nil
}

type fakeResources struct {
	panels  []resources.Panel
	err     error
	queried []string
}

func (f *fakeResources) Panels(context.Context, source.Target) ([]resources.Panel, error) {
	return f.panels, f.err
}

func (f *fakeResources) QueryRange(_ context.Context, _ source.Target, panelID string, _ source.Window) ([]source.Series, error) {
	f.queried = append(f.queried, panelID)
	if panelID != "pod-cpu" {
		return nil, resources.ErrUnknownPanel
	}
	return []source.Series{{Name: "vectors-0"}, {Name: "limit", Kind: source.SeriesLimit}}, nil
}

var podCPU = resources.Panel{ID: "pod-cpu", Title: "CPU usage", Unit: "cores"}

const testCatalog = `
dashboards:
  milvus:
    panels:
      - {id: cpu, title: CPU, unit: cores, queries: {prometheus: prom-cpu, pmm: pmm-cpu}}
      - {id: pmm-only, title: PMM only, queries: {pmm: pmm-q}}
  percona:
    panels:
      - {id: qps, title: QPS, queries: {pmm: pmm-qps}}
`

// Instances the fake Everest API serves, by name.
var testInstances = map[string]string{
	"vectors": `{"spec":{"providerRef":{"name":"milvus"}}}`,
	"orders": `{"spec":{"providerRef":{"name":"percona"},
		"components":{"monitoring":{"parameters":{"monitoringConfigName":"pmm-prod"}}}}}`,
}

// newTestServer fakes the Everest API: only "allowed" may read the instances.
func newTestServer(t *testing.T, sources ...source.Source) *httptest.Server {
	t.Helper()
	return newTestServerWithResources(t, nil, sources...)
}

func newTestServerWithResources(t *testing.T, usage resourceUsage, sources ...source.Source) *httptest.Server {
	t.Helper()
	everest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer allowed" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, ok := testInstances[path.Base(r.URL.Path)]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(everest.Close)

	catalog, err := dashboard.Parse([]byte(testCatalog))
	require.NoError(t, err)
	dashboards, err := dashboard.NewLoader(catalog, "", queryValidator(sources))
	require.NoError(t, err)
	srv := httptest.NewServer(newMux(&server{everest: newEverestClient(everest.URL), dashboards: dashboards, sources: sources, resources: usage}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url, token string, into any) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if into != nil && resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(into))
	}
	return resp.StatusCode
}

const instanceQuery = "?namespace=db&instance=vectors"

func TestDashboardListsPanelsOfTheMonitoringSource(t *testing.T) {
	notMonitoring := &fakeSource{typ: "prometheus", status: source.Status{Reason: source.ReasonNotEnabled}}
	pmm := &fakeSource{typ: "pmm", status: source.Status{Enabled: true}}
	srv := newTestServer(t, notMonitoring, pmm)

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &got))
	assert.Equal(t, sourceInfo{Type: "pmm", Status: source.Status{Enabled: true}}, got.Source)
	assert.Equal(t, []panelInfo{{ID: "cpu", Title: "CPU", Unit: "cores"}, {ID: "pmm-only", Title: "PMM only"}}, got.Panels)
}

func TestDashboardExplainsWhyNothingIsMonitored(t *testing.T) {
	srv := newTestServer(t, &fakeSource{typ: "prometheus", status: source.Status{Reason: source.ReasonNotEnabled}})

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &got))
	assert.Equal(t, source.ReasonNotEnabled, got.Source.Reason)
	assert.Empty(t, got.Panels)
}

const pmmInstanceQuery = "?namespace=db&instance=orders"

func TestDashboardPrefersTheSourceItsPanelsUse(t *testing.T) {
	prom := &fakeSource{typ: "prometheus", status: source.Status{Enabled: true}}
	pmm := &fakeSource{typ: "pmm", status: source.Status{Enabled: true}}
	srv := newTestServer(t, prom, pmm)

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+pmmInstanceQuery, "allowed", &got))
	assert.Equal(t, "pmm", got.Source.Type)
	assert.Equal(t, []panelInfo{{ID: "qps", Title: "QPS"}}, got.Panels)
	assert.Empty(t, prom.targets, "a source the dashboard has no queries for is only a fallback")
	assert.Equal(t, []source.Target{{Namespace: "db", Instance: "orders", MonitoringConfig: "pmm-prod"}}, pmm.targets,
		"sources learn the MonitoringConfig the instance references")

	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &got))
	assert.Equal(t, "prometheus", got.Source.Type, "sources the dashboard uses keep the configured order")
}

func TestDashboardExplainsWhyTheExpectedSourceIsOff(t *testing.T) {
	srv := newTestServer(t,
		&fakeSource{typ: "prometheus", status: source.Status{Reason: source.ReasonNotEnabled}},
		&fakeSource{typ: "pmm", status: source.Status{Reason: source.ReasonUnavailable}},
	)

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+pmmInstanceQuery, "allowed", &got))
	assert.Equal(t, sourceInfo{Type: "pmm", Status: source.Status{Reason: source.ReasonUnavailable}}, got.Source)
	assert.Empty(t, got.Panels)
}

func TestPanelRunsTheSourceQuery(t *testing.T) {
	prom := &fakeSource{typ: "prometheus", status: source.Status{Enabled: true, Scope: "jobs"}}
	srv := newTestServer(t, prom)

	var got panelDataResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/panels/cpu"+instanceQuery+"&range=1h", "allowed", &got))
	assert.Equal(t, []string{"prom-cpu"}, prom.queries)
	assert.Equal(t, []any{"jobs"}, prom.scopes, "the query must reuse what Status resolved")
	assert.Equal(t, "proxy", got.Series[0].Name)

	assert.Equal(t, http.StatusNotFound, get(t, srv.URL+"/api/panels/pmm-only"+instanceQuery+"&range=1h", "allowed", nil))
	assert.Equal(t, http.StatusBadRequest, get(t, srv.URL+"/api/panels/cpu"+instanceQuery+"&range=7d", "allowed", nil))
}

func TestPanelRequiresAccessToTheInstance(t *testing.T) {
	prom := &fakeSource{typ: "prometheus", status: source.Status{Enabled: true}}
	srv := newTestServer(t, prom)

	assert.Equal(t, http.StatusForbidden, get(t, srv.URL+"/api/panels/cpu"+instanceQuery+"&range=1h", "denied", nil))
	assert.Empty(t, prom.queries, "no query may run for a user who cannot read the instance")
}

func TestDashboardStartsWithPodResources(t *testing.T) {
	usage := &fakeResources{panels: []resources.Panel{podCPU}}
	srv := newTestServerWithResources(t, usage, &fakeSource{typ: "pmm", status: source.Status{Enabled: true}})

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &got))
	assert.Equal(t, []panelInfo{
		{ID: "pod-cpu", Title: "CPU usage", Unit: "cores", Source: "prometheus"},
		{ID: "cpu", Title: "CPU", Unit: "cores"},
		{ID: "pmm-only", Title: "PMM only"},
	}, got.Panels)
}

func TestPodResourcesNeedNoMonitoringSource(t *testing.T) {
	usage := &fakeResources{panels: []resources.Panel{podCPU}}
	notMonitoring := &fakeSource{typ: "prometheus", status: source.Status{Reason: source.ReasonNotEnabled}}
	srv := newTestServerWithResources(t, usage, notMonitoring)

	var dashboard dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &dashboard))
	assert.Equal(t, source.ReasonNotEnabled, dashboard.Source.Reason)
	assert.Equal(t, []panelInfo{{ID: "pod-cpu", Title: "CPU usage", Unit: "cores", Source: "prometheus"}}, dashboard.Panels)

	var got panelDataResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/panels/pod-cpu"+instanceQuery+"&range=1h", "allowed", &got))
	assert.Equal(t, []source.Series{{Name: "vectors-0"}, {Name: "limit", Kind: source.SeriesLimit}}, got.Series)
	assert.Empty(t, notMonitoring.queries)

	assert.Equal(t, http.StatusNotFound, get(t, srv.URL+"/api/panels/pod-disk"+instanceQuery+"&range=1h", "allowed", nil))
	assert.Equal(t, http.StatusForbidden, get(t, srv.URL+"/api/panels/pod-cpu"+instanceQuery+"&range=1h", "denied", nil))
	assert.Equal(t, []string{"pod-cpu", "pod-disk"}, usage.queried, "no query may run for a user who cannot read the instance")
}

func TestDashboardWithoutPodResources(t *testing.T) {
	prom := &fakeSource{typ: "prometheus", status: source.Status{Enabled: true}}
	srv := newTestServerWithResources(t, &fakeResources{err: errors.New("prometheus unreachable")}, prom)

	var got dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &got),
		"pod resources failing must not fail the dashboard")
	assert.Equal(t, []panelInfo{{ID: "cpu", Title: "CPU", Unit: "cores"}}, got.Panels)

	srv = newTestServer(t, prom)
	assert.Equal(t, http.StatusNotFound, get(t, srv.URL+"/api/panels/pod-cpu"+instanceQuery+"&range=1h", "allowed", nil))
}

func TestExploreChartsAPickedMetric(t *testing.T) {
	prom := &fakeExplorer{fakeSource: fakeSource{typ: "prometheus", status: source.Status{Enabled: true, Scope: "jobs"}}}
	srv := newTestServer(t, prom)

	var dashboard dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &dashboard))
	assert.True(t, dashboard.Source.Explorable)

	var metrics metricsResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/metrics"+instanceQuery, "allowed", &metrics))
	assert.Equal(t, "process_open_fds", metrics.Metrics[0].Name)

	var exploration source.Exploration
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/explore"+instanceQuery+"&metric=process_open_fds&type=gauge&range=24h", "allowed", &exploration))
	assert.Equal(t, "bytes", exploration.Unit)
	assert.Equal(t, []source.Metric{{Name: "process_open_fds", Type: "gauge"}}, prom.explored)
	assert.Equal(t, []any{"jobs", "jobs"}, prom.scopes, "listing and exploring must reuse what Status resolved")
}

func TestExploreRejectsInvalidMetricsAndUsers(t *testing.T) {
	prom := &fakeExplorer{fakeSource: fakeSource{typ: "prometheus", status: source.Status{Enabled: true}}}
	srv := newTestServer(t, prom)
	explore := srv.URL + "/api/explore" + instanceQuery + "&range=1h"

	assert.Equal(t, http.StatusBadRequest, get(t, explore+`&type=gauge&metric=x%7Bjob%3D%22other%22%7D`, "allowed", nil))
	assert.Equal(t, http.StatusBadRequest, get(t, explore+"&type=rate&metric=x", "allowed", nil))
	assert.Equal(t, http.StatusForbidden, get(t, explore+"&type=gauge&metric=x", "denied", nil))
	assert.Empty(t, prom.explored)
}

func TestExploreNeedsAnExplorableSource(t *testing.T) {
	srv := newTestServer(t, &fakeSource{typ: "pmm", status: source.Status{Enabled: true}})

	var dashboard dashboardResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/dashboard"+instanceQuery, "allowed", &dashboard))
	assert.False(t, dashboard.Source.Explorable)
	assert.Equal(t, http.StatusConflict, get(t, srv.URL+"/api/metrics"+instanceQuery, "allowed", nil))
}

func TestParseWindowAlignsToStep(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 34, 56, 0, time.UTC)
	window, err := parseWindow("24h", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC), window.End)
	assert.Equal(t, 24*time.Hour, window.End.Sub(window.Start))
	assert.Equal(t, 5*time.Minute, window.Step)
}
