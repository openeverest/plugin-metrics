package prometheus

import (
	"context"
	"fmt"
	"maps"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

var (
	podMonitorGVR     = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	serviceMonitorGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
	serviceGVR        = schema.GroupVersionResource{Version: "v1", Resource: "services"}

	// VictoriaMetrics operator mirrors the Prometheus Operator's PodMonitor
	// and ServiceMonitor with its own VMPodScrape and VMServiceScrape CRDs,
	// using the same spec.jobLabel/spec.selector shape. Providers that target
	// VictoriaMetrics (e.g. taking over the converted VMServiceScrape) label
	// these the same way, so they are discovered alongside the Prometheus
	// Operator kinds.
	vmPodScrapeGVR     = schema.GroupVersionResource{Group: "operator.victoriametrics.com", Version: "v1beta1", Resource: "vmpodscrapes"}
	vmServiceScrapeGVR = schema.GroupVersionResource{Group: "operator.victoriametrics.com", Version: "v1beta1", Resource: "vmservicescrapes"}
)

// podMonitorKinds and serviceMonitorKinds are the PodMonitor-shaped and
// ServiceMonitor-shaped CRDs this source reads jobs from.
var (
	podMonitorKinds     = []schema.GroupVersionResource{podMonitorGVR, vmPodScrapeGVR}
	serviceMonitorKinds = []schema.GroupVersionResource{serviceMonitorGVR, vmServiceScrapeGVR}
)

// instanceSelectors find an instance's monitors by either convention: monitors
// a provider creates itself carry the everest manager label, while monitors an
// operator creates (which owns app.kubernetes.io/managed-by) get the
// OpenEverest instance label from the provider.
func instanceSelectors(instance string) []string {
	return []string{
		"app.kubernetes.io/managed-by=everest,app.kubernetes.io/instance=" + instance,
		"core.openeverest.io/instance=" + instance,
	}
}

type monitorSpec struct {
	JobLabel string               `json:"jobLabel"`
	Selector metav1.LabelSelector `json:"selector"`
}

// jobs returns the scrape jobs of the instance's PodMonitors/VMPodScrapes and
// ServiceMonitors/VMServiceScrapes, named the way the Prometheus Operator
// names them (VictoriaMetrics operator's converted/mirrored CRDs share the
// same naming).
func (s *Source) jobs(ctx context.Context, target source.Target) ([]string, error) {
	notFound := 0
	jobs := map[string]bool{}

	for _, gvr := range podMonitorKinds {
		monitors, err := s.monitors(ctx, gvr, target)
		if apierrors.IsNotFound(err) {
			notFound++
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, monitor := range monitors {
			// A PodMonitor's job is <namespace>/<name>; providers do not set spec.jobLabel.
			jobs[monitor.GetNamespace()+"/"+monitor.GetName()] = true
		}
	}
	for _, gvr := range serviceMonitorKinds {
		monitors, err := s.monitors(ctx, gvr, target)
		if apierrors.IsNotFound(err) {
			notFound++
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, monitor := range monitors {
			names, err := s.serviceMonitorJobs(ctx, monitor)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				jobs[name] = true
			}
		}
	}

	if notFound == len(podMonitorKinds)+len(serviceMonitorKinds) {
		return nil, errOperatorMissing
	}
	return slices.Sorted(maps.Keys(jobs)), nil
}

// monitors lists the instance's monitors of one kind, once each even when
// they match both conventions.
func (s *Source) monitors(ctx context.Context, gvr schema.GroupVersionResource, target source.Target) ([]unstructured.Unstructured, error) {
	seen := map[string]bool{}
	var monitors []unstructured.Unstructured
	for _, selector := range instanceSelectors(target.Instance) {
		list, err := s.kube.Resource(gvr).Namespace(target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", gvr.Resource, err)
		}
		for _, item := range list.Items {
			if !seen[item.GetName()] {
				seen[item.GetName()] = true
				monitors = append(monitors, item)
			}
		}
	}
	return monitors, nil
}

// serviceMonitorJobs returns a ServiceMonitor's jobs: the name of each Service
// it selects, or the value of the Service label named by spec.jobLabel.
func (s *Source) serviceMonitorJobs(ctx context.Context, monitor unstructured.Unstructured) ([]string, error) {
	rawSpec, _, _ := unstructured.NestedMap(monitor.Object, "spec")
	var spec monitorSpec
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rawSpec, &spec); err != nil {
		return nil, fmt.Errorf("ServiceMonitor %s: %w", monitor.GetName(), err)
	}
	selector, err := metav1.LabelSelectorAsSelector(&spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("ServiceMonitor %s selector: %w", monitor.GetName(), err)
	}
	// Scoping by the instance's namespace keeps out targets of other namespaces.
	services, err := s.kube.Resource(serviceGVR).Namespace(monitor.GetNamespace()).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("list Services of ServiceMonitor %s: %w", monitor.GetName(), err)
	}
	var jobs []string
	for _, service := range services.Items {
		job := service.GetName()
		if value := service.GetLabels()[spec.JobLabel]; spec.JobLabel != "" && value != "" {
			job = value
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
