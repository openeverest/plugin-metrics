package promapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderQuery(t *testing.T) {
	got, err := RenderQuery(`sum(rate(x{${selector},a="b"}[${rate}]))`, `namespace="db"`, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, `sum(rate(x{namespace="db",a="b"}[120s]))`, got, "rate windows have a floor")

	got, err = RenderQuery(`rate(x{${selector}}[${rate}])`, `namespace="db"`, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, `rate(x{namespace="db"}[300s])`, got)

	_, err = RenderQuery(`sum(x)`, `namespace="db"`, time.Minute)
	assert.ErrorContains(t, err, "must be scoped")
}
