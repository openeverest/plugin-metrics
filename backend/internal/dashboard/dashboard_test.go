package dashboard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltInScopesEveryPrometheusQuery(t *testing.T) {
	catalog, err := BuiltIn()
	require.NoError(t, err)

	for _, provider := range []string{"provider-cassandra", "provider-cloudnative-pg", "mariadb", "milvus", "mssql", "tidb", "valkey"} {
		d, ok := catalog.ForProvider(provider)
		require.True(t, ok, "no built-in dashboard for %s", provider)
		assert.Len(t, d.Panels, 6, provider)
		for _, panel := range d.Panels {
			assert.Contains(t, panel.Queries["prometheus"], "${selector}", "%s panel %q would read other instances' metrics", provider, panel.ID)
		}
	}
}

func TestParseRejectsInvalidPanels(t *testing.T) {
	tests := map[string]string{
		"duplicate id": `
dashboards:
  x:
    panels:
      - {id: a, title: A, queries: {prometheus: q}}
      - {id: a, title: B, queries: {prometheus: q}}`,
		"unknown unit": `
dashboards:
  x:
    panels:
      - {id: a, title: A, unit: parsecs, queries: {prometheus: q}}`,
		"no queries": `
dashboards:
  x:
    panels:
      - {id: a, title: A}`,
		"unknown field": `
dashboards:
  x:
    panels:
      - {id: a, title: A, querys: {prometheus: q}}`,
	}
	for name, yaml := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(strings.TrimSpace(yaml)))
			assert.Error(t, err)
		})
	}
}
