// Package prometheus reads instance metrics from an in-cluster Prometheus that
// scrapes the PodMonitors providers create for their instances.
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

// Type keys Prometheus queries in dashboard panels.
const Type = "prometheus"

// Dashboard queries use these placeholders; the selector confines every query
// to the instance's scrape jobs, so it is mandatory.
const (
	selectorPlaceholder = "${selector}"
	ratePlaceholder     = "${rate}"
)

const (
	// Shorter windows than four scrape intervals make rate() return gaps.
	minRateInterval  = 2 * time.Minute
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 32 << 20
)

var (
	errNotMonitored    = errors.New("prometheus monitoring is not enabled for this instance")
	errOperatorMissing = errors.New("the PodMonitor CRD is not installed")
)

// Source queries the Prometheus HTTP API at baseURL.
type Source struct {
	baseURL string
	http    *http.Client
	kube    dynamic.Interface
}

// New returns a Source for the Prometheus at baseURL.
func New(baseURL string, kube dynamic.Interface) *Source {
	return &Source{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
		kube:    kube,
	}
}

// Type implements source.Source.
func (s *Source) Type() string { return Type }

// Status implements source.Source: an instance is monitored when its provider
// created a PodMonitor for it.
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
	return source.Status{Enabled: true}, nil
}

// QueryRange implements source.Source.
func (s *Source) QueryRange(ctx context.Context, target source.Target, query string, window source.Window) ([]source.Series, error) {
	jobs, err := s.jobs(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, errNotMonitored
	}
	promQL, err := renderQuery(query, target.Namespace, jobs, window.Step)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"query": {promQL},
		"start": {formatTime(window.Start)},
		"end":   {formatTime(window.End)},
		"step":  {strconv.FormatFloat(window.Step.Seconds(), 'f', -1, 64)},
	}
	var matrix matrixData
	if err := s.getJSON(ctx, "/api/v1/query_range", params, &matrix); err != nil {
		return nil, err
	}
	return decodeMatrix(matrix)
}

// getJSON calls a Prometheus API endpoint and decodes its data field.
func (s *Source) getJSON(ctx context.Context, path string, params url.Values, data any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("prometheus unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read prometheus response: %w", err)
	}
	var parsed struct {
		Status string          `json:"status"`
		Error  string          `json:"error"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("prometheus returned %d with an unreadable body", resp.StatusCode)
	}
	if parsed.Status != "success" {
		return fmt.Errorf("prometheus query failed: %s", parsed.Error)
	}
	return json.Unmarshal(parsed.Data, data)
}

// ValidateQuery implements source.Source: without the selector a query would
// read every instance's metrics.
func (s *Source) ValidateQuery(query string) error {
	return validateQuery(query)
}

func validateQuery(query string) error {
	if !strings.Contains(query, selectorPlaceholder) {
		return fmt.Errorf("query must be scoped with %s", selectorPlaceholder)
	}
	return nil
}

func renderQuery(query, namespace string, jobs []string, step time.Duration) (string, error) {
	if err := validateQuery(query); err != nil {
		return "", err
	}
	rate := max(step, minRateInterval)
	return strings.NewReplacer(
		selectorPlaceholder, scopeSelector(namespace, jobs),
		ratePlaceholder, strconv.Itoa(int(rate.Seconds()))+"s",
	).Replace(query), nil
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

func formatTime(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

type matrixData struct {
	ResultType string `json:"resultType"`
	Result     []struct {
		Metric map[string]string `json:"metric"`
		Values [][2]any          `json:"values"`
	} `json:"result"`
}

func decodeMatrix(matrix matrixData) ([]source.Series, error) {
	if matrix.ResultType != "matrix" {
		return nil, fmt.Errorf("prometheus returned %q, expected a matrix", matrix.ResultType)
	}

	series := make([]source.Series, 0, len(matrix.Result))
	for _, result := range matrix.Result {
		points := make([]source.Point, 0, len(result.Values))
		for _, sample := range result.Values {
			point, err := toPoint(sample)
			if err != nil {
				return nil, err
			}
			points = append(points, point)
		}
		series = append(series, source.Series{Name: seriesName(result.Metric), Points: points})
	}
	slices.SortFunc(series, func(a, b source.Series) int { return strings.Compare(a.Name, b.Name) })
	return series, nil
}

// toPoint converts a [unixSeconds, "value"] sample.
func toPoint(sample [2]any) (source.Point, error) {
	seconds, ok := sample[0].(float64)
	if !ok {
		return source.Point{}, fmt.Errorf("invalid sample timestamp %v", sample[0])
	}
	raw, ok := sample[1].(string)
	if !ok {
		return source.Point{}, fmt.Errorf("invalid sample value %v", sample[1])
	}
	point := source.Point{T: int64(math.Round(seconds * 1000))}
	if value, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
		point.V = &value
	}
	return point, nil
}

// seriesName joins the label values a query kept, e.g. its "by (...)" labels.
func seriesName(metric map[string]string) string {
	keys := make([]string, 0, len(metric))
	for key := range metric {
		if key != "__name__" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = metric[key]
	}
	return strings.Join(values, " / ")
}
