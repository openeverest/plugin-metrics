package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openeverest/plugin-metrics/backend/internal/dashboard"
	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

type fakeSource struct {
	typ     string
	status  source.Status
	queries []string
}

func (f *fakeSource) Type() string { return f.typ }

func (f *fakeSource) Status(context.Context, source.Target) (source.Status, error) {
	return f.status, nil
}

func (f *fakeSource) QueryRange(_ context.Context, _ source.Target, query string, _ source.Window) ([]source.Series, error) {
	f.queries = append(f.queries, query)
	return []source.Series{{Name: "proxy", Points: []source.Point{{T: 1}}}}, nil
}

const testCatalog = `
dashboards:
  milvus:
    panels:
      - {id: cpu, title: CPU, unit: cores, queries: {prometheus: prom-cpu, pmm: pmm-cpu}}
      - {id: pmm-only, title: PMM only, queries: {pmm: pmm-q}}
`

// newTestServer fakes the Everest API: only "allowed" may read the instance.
func newTestServer(t *testing.T, sources ...source.Source) *httptest.Server {
	t.Helper()
	everest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer allowed" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"spec":{"providerRef":{"name":"milvus"}}}`))
	}))
	t.Cleanup(everest.Close)

	catalog, err := dashboard.Parse([]byte(testCatalog))
	require.NoError(t, err)
	srv := httptest.NewServer(newMux(&server{everest: newEverestClient(everest.URL), catalog: catalog, sources: sources}))
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

func TestPanelRunsTheSourceQuery(t *testing.T) {
	prom := &fakeSource{typ: "prometheus", status: source.Status{Enabled: true}}
	srv := newTestServer(t, prom)

	var got panelDataResponse
	require.Equal(t, http.StatusOK, get(t, srv.URL+"/api/panels/cpu"+instanceQuery+"&range=1h", "allowed", &got))
	assert.Equal(t, []string{"prom-cpu"}, prom.queries)
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

func TestParseWindowAlignsToStep(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 34, 56, 0, time.UTC)
	window, err := parseWindow("24h", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 9, 12, 30, 0, 0, time.UTC), window.End)
	assert.Equal(t, 24*time.Hour, window.End.Sub(window.Start))
	assert.Equal(t, 5*time.Minute, window.Step)
}
