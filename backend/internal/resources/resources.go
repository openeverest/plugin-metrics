// Package resources charts the CPU and memory an instance's pods use, next to
// their limits. Usage comes from the container metrics (cAdvisor) the
// cluster's Prometheus scrapes from every kubelet, so it is shown for every
// provider, whichever source serves the engine's own panels.
package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/promapi"
)

// SourceType names the monitoring system the panels read.
const SourceType = "prometheus"

// PanelPrefix starts the ID of every panel this package serves; dashboards
// must not use it.
const PanelPrefix = "pod-"

const (
	// Providers label every pod of an instance with these.
	instanceLabel  = "core.openeverest.io/instance"
	componentLabel = "core.openeverest.io/component"

	requestTimeout = 15 * time.Second
	// The dashboard waits for the availability check, so it gives up sooner.
	checkTimeout = 5 * time.Second

	limitName = "limit"
)

// containerFilter drops cAdvisor's pod-level and pause-container series, which
// would count the containers twice.
const containerFilter = `container!="",container!="POD"`

// Panel is one chart of the pods' usage.
type Panel struct {
	ID    string
	Title string
	Unit  string
	query string
	// limit is the container resource whose limits are drawn as lines.
	limit string
}

var panels = []Panel{
	{
		ID:    PanelPrefix + "cpu",
		Title: "CPU usage",
		Unit:  "cores",
		query: `sum by (pod) (rate(container_cpu_usage_seconds_total{` + promapi.SelectorPlaceholder + `,` + containerFilter + `}[` + promapi.RatePlaceholder + `]))`,
		limit: "cpu",
	},
	{
		ID:    PanelPrefix + "memory",
		Title: "Memory usage",
		Unit:  "bytes",
		query: `sum by (pod) (container_memory_working_set_bytes{` + promapi.SelectorPlaceholder + `,` + containerFilter + `})`,
		limit: "memory",
	},
}

// availabilityQuery finds out whether Prometheus has the pods' container metrics.
const availabilityQuery = `count(container_memory_working_set_bytes{` + promapi.SelectorPlaceholder + `,` + containerFilter + `})`

var podGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// ErrUnknownPanel means the panel ID is not one of this package's panels.
var ErrUnknownPanel = errors.New("unknown resources panel")

// Usage reads the pods of an instance and their usage from Prometheus.
type Usage struct {
	api  promapi.Client
	kube dynamic.Interface
}

// New returns a Usage that queries the Prometheus at baseURL.
func New(baseURL string, kube dynamic.Interface) *Usage {
	return &Usage{
		api:  promapi.Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: requestTimeout}},
		kube: kube,
	}
}

// pod is what the panels need from one of the instance's pods.
type pod struct {
	name      string
	component string
	// limits sums the containers' limits by resource; containers without a
	// limit add nothing.
	limits map[string]float64
}

// Panels returns the panels to show for the target: none when it has no pods
// or Prometheus has no container metrics for them.
func (u *Usage) Panels(ctx context.Context, target source.Target) ([]Panel, error) {
	pods, err := u.pods(ctx, target)
	if err != nil || len(pods) == 0 {
		return nil, err
	}
	query, err := promapi.RenderQuery(availabilityQuery, podSelector(target.Namespace, pods), 0)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	var vector struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := u.api.GetJSON(ctx, "/api/v1/query", url.Values{"query": {query}}, &vector); err != nil {
		return nil, err
	}
	if len(vector.Result) == 0 {
		return nil, nil
	}
	return slices.Clone(panels), nil
}

// QueryRange returns a panel's usage series per pod, followed by its limit lines.
func (u *Usage) QueryRange(ctx context.Context, target source.Target, panelID string, window source.Window) ([]source.Series, error) {
	i := slices.IndexFunc(panels, func(p Panel) bool { return p.ID == panelID })
	if i < 0 {
		return nil, ErrUnknownPanel
	}
	panel := panels[i]
	pods, err := u.pods(ctx, target)
	if err != nil || len(pods) == 0 {
		return []source.Series{}, err
	}
	query, err := promapi.RenderQuery(panel.query, podSelector(target.Namespace, pods), window.Step)
	if err != nil {
		return nil, err
	}
	usage, err := u.api.QueryRange(ctx, query, window)
	if err != nil {
		return nil, err
	}
	return append(usage, limitSeries(pods, panel.limit, timestamps(usage))...), nil
}

// pods lists the pods the instance has now. A pod replaced within the time
// range is charted only under its current name.
func (u *Usage) pods(ctx context.Context, target source.Target) ([]pod, error) {
	list, err := u.kube.Resource(podGVR).Namespace(target.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: instanceLabel + "=" + target.Instance,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	pods := make([]pod, 0, len(list.Items))
	for _, item := range list.Items {
		pods = append(pods, pod{
			name:      item.GetName(),
			component: item.GetLabels()[componentLabel],
			limits:    containerLimits(item),
		})
	}
	slices.SortFunc(pods, func(a, b pod) int { return strings.Compare(a.name, b.name) })
	return pods, nil
}

// containerLimits sums the limits of the pod's containers; init containers
// are done before the others start.
func containerLimits(item unstructured.Unstructured) map[string]float64 {
	limits := map[string]float64{}
	containers, _, _ := unstructured.NestedSlice(item.Object, "spec", "containers")
	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		values, _, _ := unstructured.NestedMap(container, "resources", "limits")
		for name, value := range values {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if quantity, err := resource.ParseQuantity(text); err == nil {
				limits[name] += quantity.AsApproximateFloat64()
			}
		}
	}
	return limits
}

// podSelector matches only the pods' series in the instance's namespace.
func podSelector(namespace string, pods []pod) string {
	names := make([]string, len(pods))
	for i, p := range pods {
		names[i] = regexp.QuoteMeta(p.name)
	}
	// PromQL string literals use Go escaping, so %q is safe for any value.
	return fmt.Sprintf("namespace=%q,pod=~%q", namespace, strings.Join(names, "|"))
}

// timestamps returns the usage series' sample times, which the limit lines span.
func timestamps(series []source.Series) []int64 {
	seen := map[int64]bool{}
	for _, s := range series {
		for _, p := range s.Points {
			seen[p.T] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// limitSeries draws one line per distinct limit: a single "limit" when every
// pod with a limit has the same one, otherwise one per component, or one per
// pod when the pods of a component differ.
func limitSeries(pods []pod, resourceName string, times []int64) []source.Series {
	if len(times) == 0 {
		return nil
	}
	byComponent := map[string][]pod{}
	distinct := map[float64]bool{}
	for _, p := range pods {
		if value := p.limits[resourceName]; value > 0 {
			byComponent[p.component] = append(byComponent[p.component], p)
			distinct[value] = true
		}
	}
	switch len(distinct) {
	case 0:
		return nil
	case 1:
		for value := range distinct {
			return []source.Series{constantSeries(limitName, value, times)}
		}
	}

	var lines []source.Series
	for _, component := range slices.Sorted(maps.Keys(byComponent)) {
		group := byComponent[component]
		if component != "" && sameLimit(group, resourceName) {
			lines = append(lines, constantSeries(limitName+" · "+component, group[0].limits[resourceName], times))
			continue
		}
		for _, p := range group {
			lines = append(lines, constantSeries(limitName+" · "+p.name, p.limits[resourceName], times))
		}
	}
	return lines
}

func sameLimit(pods []pod, resourceName string) bool {
	for _, p := range pods[1:] {
		if p.limits[resourceName] != pods[0].limits[resourceName] {
			return false
		}
	}
	return true
}

func constantSeries(name string, value float64, times []int64) source.Series {
	points := make([]source.Point, len(times))
	for i, t := range times {
		points[i] = source.Point{T: t, V: &value}
	}
	return source.Series{Name: name, Kind: source.SeriesLimit, Points: points}
}
