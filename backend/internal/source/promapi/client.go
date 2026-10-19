// Package promapi reads the Prometheus HTTP API, which Prometheus and PMM's
// VictoriaMetrics both serve, for the sources built on it.
package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

const maxResponseBytes = 32 << 20

// Client calls the Prometheus HTTP API under BaseURL, e.g.
// http://prometheus:9090 for /api/v1/query_range.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Token is sent as a bearer token when set.
	Token string
	// ExtraFilters are series selectors VictoriaMetrics applies to every read
	// on top of the query (extra_filters[]); Prometheus ignores them.
	ExtraFilters []string
	// DisplayValue, when set, rewrites a label value before it names a series.
	DisplayValue func(label, value string) string
}

// QueryRange runs a rendered PromQL query over the window.
func (c Client) QueryRange(ctx context.Context, promQL string, window source.Window) ([]source.Series, error) {
	params := url.Values{
		"query": {promQL},
		"start": {formatTime(window.Start)},
		"end":   {formatTime(window.End)},
		"step":  {strconv.FormatFloat(window.Step.Seconds(), 'f', -1, 64)},
	}
	var matrix matrixData
	if err := c.GetJSON(ctx, "/api/v1/query_range", params, &matrix); err != nil {
		return nil, err
	}
	return decodeMatrix(matrix, c.DisplayValue)
}

// GetJSON calls an API endpoint and decodes its data field.
func (c Client) GetJSON(ctx context.Context, path string, params url.Values, data any) error {
	query := url.Values{}
	for key, values := range params {
		query[key] = slices.Clone(values)
	}
	for _, filter := range c.ExtraFilters {
		query.Add("extra_filters[]", filter)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("metrics API unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read metrics API response: %w", err)
	}
	var parsed struct {
		Status string          `json:"status"`
		Error  string          `json:"error"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("metrics API returned %d with an unreadable body", resp.StatusCode)
	}
	if parsed.Status != "success" {
		return fmt.Errorf("metrics query failed: %s", parsed.Error)
	}
	return json.Unmarshal(parsed.Data, data)
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

func decodeMatrix(matrix matrixData, displayValue func(label, value string) string) ([]source.Series, error) {
	if matrix.ResultType != "matrix" {
		return nil, fmt.Errorf("metrics API returned %q, expected a matrix", matrix.ResultType)
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
		series = append(series, source.Series{Name: seriesName(result.Metric, displayValue), Points: points})
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
func seriesName(metric map[string]string, displayValue func(label, value string) string) string {
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
		if displayValue != nil {
			values[i] = displayValue(key, values[i])
		}
	}
	return strings.Join(values, " / ")
}
