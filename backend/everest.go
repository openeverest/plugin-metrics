package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const everestRequestTimeout = 10 * time.Second

func everestAPIURL() string {
	if v := os.Getenv("EVEREST_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	// Injected by Kubernetes when the plugin runs in the same namespace as the "everest" Service.
	host := os.Getenv("EVEREST_SERVICE_HOST")
	port := os.Getenv("EVEREST_SERVICE_PORT")
	if host != "" && port != "" {
		return fmt.Sprintf("http://%s:%s", host, port)
	}
	return "http://everest.everest-system.svc.cluster.local:8080"
}

// statusError carries an HTTP status to return to the caller.
type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }

// instance is the subset of the Instance CR the plugin needs.
type instance struct {
	Spec struct {
		ProviderRef struct {
			Name string `json:"name"`
		} `json:"providerRef"`
	} `json:"spec"`
}

type everestClient struct {
	baseURL string
	http    *http.Client
}

func newEverestClient(baseURL string) *everestClient {
	return &everestClient{baseURL: baseURL, http: &http.Client{Timeout: everestRequestTimeout}}
}

// getInstance fetches the Instance as the calling user, which doubles as the
// authorization check: the Everest API enforces the user's own RBAC.
func (c *everestClient) getInstance(ctx context.Context, token string, ref instanceRef) (*instance, error) {
	apiURL := fmt.Sprintf("%s/v1/clusters/%s/namespaces/%s/instances/%s",
		c.baseURL,
		url.PathEscape(ref.k8sCluster),
		url.PathEscape(ref.namespace),
		url.PathEscape(ref.name),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &statusError{status: http.StatusBadGateway, message: "everest API unreachable"}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return nil, &statusError{status: resp.StatusCode, message: http.StatusText(resp.StatusCode)}
	default:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &statusError{status: http.StatusBadGateway, message: fmt.Sprintf("everest API returned %d", resp.StatusCode)}
	}

	var in instance
	if err := json.NewDecoder(resp.Body).Decode(&in); err != nil {
		return nil, &statusError{status: http.StatusBadGateway, message: "invalid instance response"}
	}
	return &in, nil
}
