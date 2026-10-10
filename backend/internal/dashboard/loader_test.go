package dashboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const loaderBuiltIn = `
dashboards:
  milvus:
    panels:
      - {id: cpu, title: CPU, queries: {prometheus: "x{${selector}}"}}
`

func scoped(_, query string) error {
	if !strings.Contains(query, "${selector}") {
		return errors.New("unscoped")
	}
	return nil
}

func newTestLoader(t *testing.T, path string) *Loader {
	t.Helper()
	builtIn, err := Parse([]byte(loaderBuiltIn))
	require.NoError(t, err)
	loader, err := NewLoader(builtIn, path, scoped)
	require.NoError(t, err)
	return loader
}

// writeOverrides bumps the modification time so successive writes within the
// filesystem's timestamp granularity still count as a change.
func writeOverrides(t *testing.T, path, content string, age time.Duration) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o600))
	modTime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func panelIDs(t *testing.T, loader *Loader, provider string) []string {
	t.Helper()
	d, ok := loader.Catalog().ForProvider(provider)
	if !ok {
		return nil
	}
	ids := []string{}
	for _, p := range d.Panels {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestLoaderWithoutOverridesServesBuiltIn(t *testing.T) {
	loader := newTestLoader(t, filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Equal(t, []string{"cpu"}, panelIDs(t, loader, "milvus"))
}

func TestLoaderOverlaysAndReloadsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboards.yaml")
	writeOverrides(t, path, `
dashboards:
  valkey:
    panels:
      - {id: commands, title: Commands, unit: ops, queries: {prometheus: "rate(c{${selector}}[${rate}])"}}
`, time.Minute)
	loader := newTestLoader(t, path)
	assert.Equal(t, []string{"commands"}, panelIDs(t, loader, "valkey"))
	assert.Equal(t, []string{"cpu"}, panelIDs(t, loader, "milvus"), "providers without an override keep the built-in dashboard")

	writeOverrides(t, path, `
dashboards:
  milvus:
    panels:
      - {id: memory, title: Memory, unit: bytes, queries: {prometheus: "m{${selector}}"}}
`, 0)
	assert.Equal(t, []string{"memory"}, panelIDs(t, loader, "milvus"), "an override replaces the whole built-in dashboard")
	assert.Nil(t, panelIDs(t, loader, "valkey"), "removed overrides disappear on reload")

	require.NoError(t, os.Remove(path))
	assert.Equal(t, []string{"cpu"}, panelIDs(t, loader, "milvus"))
}

func TestLoaderKeepsTheLastGoodOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboards.yaml")
	writeOverrides(t, path, `
dashboards:
  valkey:
    panels:
      - {id: commands, title: Commands, queries: {prometheus: "c{${selector}}"}}
`, time.Minute)
	loader := newTestLoader(t, path)

	writeOverrides(t, path, `
dashboards:
  valkey:
    panels:
      - {id: leaky, title: Leaky, queries: {prometheus: "c"}}
`, 0)
	assert.Equal(t, []string{"commands"}, panelIDs(t, loader, "valkey"), "an unscoped query must not replace the last good dashboards")
}

func TestLoaderRejectsAnUnscopedBuiltIn(t *testing.T) {
	builtIn, err := Parse([]byte(`
dashboards:
  milvus:
    panels:
      - {id: cpu, title: CPU, queries: {prometheus: "x"}}`))
	require.NoError(t, err)
	_, err = NewLoader(builtIn, "", scoped)
	assert.ErrorContains(t, err, "unscoped")
}
