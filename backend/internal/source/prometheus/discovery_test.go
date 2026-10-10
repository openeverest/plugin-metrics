package prometheus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func object(apiVersion, kind, name string, labels map[string]string, spec map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetNamespace("db")
	obj.SetName(name)
	obj.SetLabels(labels)
	if spec != nil {
		obj.Object["spec"] = spec
	}
	return obj
}

func serviceMonitor(name string, labels map[string]string, spec map[string]any) *unstructured.Unstructured {
	return object("monitoring.coreos.com/v1", "ServiceMonitor", name, labels, spec)
}

func service(name string, labels map[string]string) *unstructured.Unstructured {
	return object("v1", "Service", name, labels, nil)
}

var openEverestInstanceLabel = map[string]string{"core.openeverest.io/instance": "vectors"}

func TestJobsFromOperatorMonitors(t *testing.T) {
	kube := fakeKube(
		// An operator-made PodMonitor the provider labelled for the instance.
		podMonitor("vectors", map[string]string{"app.kubernetes.io/managed-by": "mssql-operator", "core.openeverest.io/instance": "vectors"}),
		// Matches both conventions; must count once.
		podMonitor("vectors-metrics", map[string]string{
			"app.kubernetes.io/managed-by": "everest", "app.kubernetes.io/instance": "vectors", "core.openeverest.io/instance": "vectors",
		}),
		serviceMonitor("vectors-metrics-sm", openEverestInstanceLabel, map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "exporter"}},
		}),
		serviceMonitor("vectors-labelled-job", openEverestInstanceLabel, map[string]any{
			"jobLabel": "team",
			"selector": map[string]any{"matchExpressions": []any{
				map[string]any{"key": "tier", "operator": "In", "values": []any{"db"}},
			}},
		}),
		serviceMonitor("other-instance", map[string]string{"core.openeverest.io/instance": "other"}, map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "other"}},
		}),
		service("vectors-exporter", map[string]string{"app": "exporter"}),
		service("vectors-db", map[string]string{"tier": "db", "team": "search"}),
		service("unrelated", map[string]string{"app": "other"}),
	)

	jobs, err := New("http://prometheus", kube).jobs(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, []string{"db/vectors", "db/vectors-metrics", "search", "vectors-exporter"}, jobs)
}

func TestServiceMonitorsAloneEnableMonitoring(t *testing.T) {
	kube := fakeKube(
		serviceMonitor("vectors", openEverestInstanceLabel, map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "exporter"}},
		}),
		service("vectors-exporter", map[string]string{"app": "exporter"}),
	)
	status, err := New("http://prometheus", kube).Status(context.Background(), target)
	require.NoError(t, err)
	assert.True(t, status.Enabled)
}
