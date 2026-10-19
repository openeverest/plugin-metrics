package promapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

func TestQueryRange(t *testing.T) {
	var gotPath, gotAuth string
	var gotQuery url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotQuery = r.URL.Path, r.Header.Get("Authorization"), r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"core_openeverest_io_component":"proxy"},"values":[[1700000000,"0.5"],[1700000030,"NaN"]]},
			{"metric":{"__name__":"up","core_openeverest_io_component":"dataNode"},"values":[[1700000000.5,"2"]]}
		]}}`))
	}))
	defer api.Close()

	client := Client{BaseURL: api.URL + "/prometheus/", HTTP: api.Client(), Token: "secret", ExtraFilters: []string{`{namespace="db"}`}}
	window := source.Window{Start: time.Unix(1700000000, 0), End: time.Unix(1700003600, 0), Step: 30 * time.Second}
	series, err := client.QueryRange(context.Background(), `sum(x)`, window)
	require.NoError(t, err)

	assert.Equal(t, "/prometheus/api/v1/query_range", gotPath)
	assert.Equal(t, "Bearer secret", gotAuth)
	assert.Equal(t, `sum(x)`, gotQuery.Get("query"))
	assert.Equal(t, []string{`{namespace="db"}`}, gotQuery["extra_filters[]"])
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

func TestQueryRangeWithoutCredentials(t *testing.T) {
	var gotAuth string
	var gotQuery url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.Query()
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer api.Close()

	_, err := Client{BaseURL: api.URL, HTTP: api.Client()}.
		QueryRange(context.Background(), `x`, source.Window{Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, gotAuth)
	assert.NotContains(t, gotQuery, "extra_filters[]")
}

func TestQueryRangeErrors(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
	}))
	defer api.Close()

	_, err := Client{BaseURL: api.URL, HTTP: api.Client()}.
		QueryRange(context.Background(), `x`, source.Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: time.Minute})
	assert.ErrorContains(t, err, "parse error")
}
