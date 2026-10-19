// Package prometheus reads instance metrics from an in-cluster Prometheus that
// scrapes the PodMonitors providers create for their instances.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/promapi"
)

// Type keys Prometheus queries in dashboard panels.
const Type = "prometheus"

const requestTimeout = 15 * time.Second

var (
	errNotMonitored    = errors.New("prometheus monitoring is not enabled for this instance")
	errOperatorMissing = errors.New("the PodMonitor CRD is not installed")
)

// Source queries the Prometheus HTTP API at baseURL.
type Source struct {
	api  promapi.Client
	kube dynamic.Interface
}

// New returns a Source for the Prometheus at baseURL.
func New(baseURL string, kube dynamic.Interface) *Source {
	return &Source{
		api:  promapi.Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: requestTimeout}},
		kube: kube,
	}
}

// Type implements source.Source.
func (s *Source) Type() string { return Type }

// Status implements source.Source: an instance is monitored when its provider
// created a PodMonitor for it. The scrape jobs found become the Scope.
func (s *Source) Status(ctx context.Context, target source.Target) (source.Status, error) {
	jobs, err := s.jobs(ctx, target)
	switch {
	case errors.Is(err, errOperatorMissing):
		return source.Status{Reason: source.ReasonUnavailable}, nil
	case err != nil:
		return source.Status{}, err
	case len(jobs) == 0:
		return source.Status{Reason: source.ReasonNotEnabled}, nil
	}
	return source.Status{Enabled: true, Scope: scope{jobs: jobs}}, nil
}

// scope is the Status result queries reuse instead of listing monitors again.
type scope struct {
	jobs []string
}

// selector returns the label matchers confining a query to the target's
// scrape jobs, from the Scope Status resolved when the caller passed it on.
func (s *Source) selector(ctx context.Context, target source.Target) (string, error) {
	sc, ok := target.Scope.(scope)
	if !ok {
		jobs, err := s.jobs(ctx, target)
		if err != nil {
			return "", err
		}
		sc = scope{jobs: jobs}
	}
	if len(sc.jobs) == 0 {
		return "", errNotMonitored
	}
	return scopeSelector(target.Namespace, sc.jobs), nil
}

// QueryRange implements source.Source.
func (s *Source) QueryRange(ctx context.Context, target source.Target, query string, window source.Window) ([]source.Series, error) {
	selector, err := s.selector(ctx, target)
	if err != nil {
		return nil, err
	}
	promQL, err := promapi.RenderQuery(query, selector, window.Step)
	if err != nil {
		return nil, err
	}
	return s.api.QueryRange(ctx, promQL, window)
}

// ValidateQuery implements source.Source: without the selector a query would
// read every instance's metrics.
func (s *Source) ValidateQuery(query string) error {
	return promapi.ValidateQuery(query)
}

// scopeSelector matches only the instance's namespace and scrape jobs.
func scopeSelector(namespace string, jobs []string) string {
	quoted := make([]string, len(jobs))
	for i, job := range jobs {
		quoted[i] = regexp.QuoteMeta(job)
	}
	// PromQL string literals use Go escaping, so %q is safe for any value.
	return fmt.Sprintf("namespace=%q,job=~%q", namespace, strings.Join(quoted, "|"))
}
