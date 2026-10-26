package dashboard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltInScopesEveryQuery(t *testing.T) {
	catalog, err := BuiltIn()
	require.NoError(t, err)

	for _, provider := range []string{
		"provider-cassandra", "provider-cloudnative-pg", "mariadb", "milvus", "mssql", "tidb", "valkey",
		"percona-server-mongodb", "provider-percona-postgresql", "percona-xtradb-cluster",
	} {
		d, ok := catalog.ForProvider(provider)
		require.True(t, ok, "no built-in dashboard for %s", provider)
		assert.NotEmpty(t, d.Panels, provider)
	}
	for provider, d := range catalog.Dashboards {
		for _, panel := range d.Panels {
			for sourceType, query := range panel.Queries {
				assert.Contains(t, query, "${selector}", "%s panel %q would read other instances' %s metrics", provider, panel.ID, sourceType)
			}
		}
	}
}

func TestUses(t *testing.T) {
	d := Dashboard{Panels: []Panel{
		{ID: "a", Queries: map[string]string{"prometheus": "q"}},
		{ID: "b", Queries: map[string]string{"pmm": "q"}},
	}}
	assert.True(t, d.Uses("pmm"))
	assert.False(t, d.Uses("other"))
	assert.False(t, Dashboard{}.Uses("pmm"), "a provider without a dashboard uses no source")
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
		"reserved id": `
dashboards:
  x:
    panels:
      - {id: pod-cpu, title: A, queries: {prometheus: q}}`,
	}
	for name, yaml := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(strings.TrimSpace(yaml)))
			assert.Error(t, err)
		})
	}
}
