// Package pmm reads instance metrics from the PMM server the instance's
// MonitoringConfig points at. Providers label every series an instance pushes
// to PMM with its namespace and core_openeverest_io_instance, which scope the
// queries: one PMM server usually monitors many instances and namespaces.
package pmm

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/promapi"
)

// Type keys PMM queries in dashboard panels.
const Type = "pmm"

const (
	// instanceLabel is the custom label providers pass to pmm-admin, in the
	// Prometheus form of the core.openeverest.io/instance pod label.
	instanceLabel = "core_openeverest_io_instance"
	// serviceLabel names a database pod's PMM service; the operators register
	// it as <namespace>-<pod>.
	serviceLabel = "service_name"
	// PMM serves the Prometheus HTTP API of its VictoriaMetrics under this path.
	apiPath = "/prometheus"
	// credentialsKey holds the PMM token in the MonitoringConfig's Secret.
	credentialsKey = "apiKey"
	requestTimeout = 15 * time.Second
)

var (
	monitoringConfigGVR = schema.GroupVersionResource{Group: "monitoring.openeverest.io", Version: "v1alpha1", Resource: "monitoringconfigs"}
	secretGVR           = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
)

var (
	errNotMonitored = errors.New("PMM monitoring is not enabled for this instance")
	errUnavailable  = errors.New("PMM is not reachable with the instance's MonitoringConfig")
)

// monitoringConfigSpec is the subset of a MonitoringConfig the plugin needs.
type monitoringConfigSpec struct {
	Type string `json:"type"`
	PMM  *struct {
		URL                  string `json:"url"`
		VerifyTLS            *bool  `json:"verifyTLS"`
		CredentialsSecretRef struct {
			Name string `json:"name"`
		} `json:"credentialsSecretRef"`
	} `json:"pmm"`
}

// Source queries the PMM server of each instance's MonitoringConfig.
type Source struct {
	kube dynamic.Interface
	// Shared so that connections to a PMM server are reused across requests.
	verifiedHTTP *http.Client
	insecureHTTP *http.Client
}

// New returns a Source that reads MonitoringConfigs and their Secrets with kube.
func New(kube dynamic.Interface) *Source {
	insecure := http.DefaultTransport.(*http.Transport).Clone()
	// Only used for MonitoringConfigs that set verifyTLS: false.
	insecure.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	return &Source{
		kube:         kube,
		verifiedHTTP: &http.Client{Timeout: requestTimeout},
		insecureHTTP: &http.Client{Timeout: requestTimeout, Transport: insecure},
	}
}

// Type implements source.Source.
func (s *Source) Type() string { return Type }

// scope is the Status result queries reuse instead of reading the
// MonitoringConfig and its Secret again.
type scope struct {
	api promapi.Client
}

// Status implements source.Source: an instance is monitored when its
// monitoring component references a PMM MonitoringConfig. The client for that
// PMM server becomes the Scope.
func (s *Source) Status(ctx context.Context, target source.Target) (source.Status, error) {
	if target.MonitoringConfig == "" {
		return source.Status{Reason: source.ReasonNotEnabled}, nil
	}
	api, err := s.client(ctx, target)
	switch {
	case errors.Is(err, errNotMonitored):
		return source.Status{Reason: source.ReasonNotEnabled}, nil
	case errors.Is(err, errUnavailable):
		return source.Status{Reason: source.ReasonUnavailable}, nil
	case err != nil:
		return source.Status{}, err
	}
	return source.Status{Enabled: true, Scope: scope{api: api}}, nil
}

// client builds a client for the target's PMM server, scoped to the target.
func (s *Source) client(ctx context.Context, target source.Target) (promapi.Client, error) {
	if target.MonitoringConfig == "" {
		return promapi.Client{}, errNotMonitored
	}
	config, err := s.kube.Resource(monitoringConfigGVR).Namespace(target.Namespace).Get(ctx, target.MonitoringConfig, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return promapi.Client{}, fmt.Errorf("%w: MonitoringConfig %q not found", errUnavailable, target.MonitoringConfig)
	}
	if err != nil {
		return promapi.Client{}, fmt.Errorf("get MonitoringConfig %q: %w", target.MonitoringConfig, err)
	}
	rawSpec, _, _ := unstructured.NestedMap(config.Object, "spec")
	var spec monitoringConfigSpec
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rawSpec, &spec); err != nil {
		return promapi.Client{}, fmt.Errorf("MonitoringConfig %q: %w", target.MonitoringConfig, err)
	}
	if spec.Type != Type || spec.PMM == nil {
		return promapi.Client{}, errNotMonitored
	}
	baseURL, err := apiURL(spec.PMM.URL)
	if err != nil {
		return promapi.Client{}, fmt.Errorf("MonitoringConfig %q: %w", target.MonitoringConfig, err)
	}
	token, err := s.token(ctx, target.Namespace, spec.PMM.CredentialsSecretRef.Name)
	if err != nil {
		return promapi.Client{}, err
	}

	httpClient := s.verifiedHTTP
	if spec.PMM.VerifyTLS != nil && !*spec.PMM.VerifyTLS {
		httpClient = s.insecureHTTP
	}
	return promapi.Client{
		BaseURL: baseURL,
		HTTP:    httpClient,
		Token:   token,
		// VictoriaMetrics applies the scope to every read even if a query
		// misses it. PMM drops it only for tokens with label-based access
		// control, whose own filters then confine the query instead.
		ExtraFilters: []string{"{" + scopeSelector(target) + "}"},
		DisplayValue: displayValue(target.Namespace),
	}, nil
}

// apiURL returns PMM's Prometheus API base for the MonitoringConfig URL.
func apiURL(pmmURL string) (string, error) {
	u, err := url.Parse(pmmURL)
	if err != nil {
		return "", fmt.Errorf("invalid PMM URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid PMM URL %q: expected http(s)://host", pmmURL)
	}
	return strings.TrimRight(u.String(), "/") + apiPath, nil
}

// token reads the PMM token from the MonitoringConfig's credentials Secret.
func (s *Source) token(ctx context.Context, namespace, secretName string) (string, error) {
	secret, err := s.kube.Resource(secretGVR).Namespace(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", fmt.Errorf("%w: Secret %q not found", errUnavailable, secretName)
	}
	if err != nil {
		return "", fmt.Errorf("get Secret %q: %w", secretName, err)
	}
	encoded, _, _ := unstructured.NestedString(secret.Object, "data", credentialsKey)
	token, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(token) == 0 {
		return "", fmt.Errorf("%w: Secret %q has no %s", errUnavailable, secretName, credentialsKey)
	}
	return string(token), nil
}

// scopeSelector matches only the instance's series.
func scopeSelector(target source.Target) string {
	// PromQL string literals use Go escaping, so %q is safe for any value.
	return fmt.Sprintf("namespace=%q,%s=%q", target.Namespace, instanceLabel, target.Instance)
}

// displayValue names series after the pod rather than its PMM service.
func displayValue(namespace string) func(label, value string) string {
	return func(label, value string) string {
		if label == serviceLabel {
			return strings.TrimPrefix(value, namespace+"-")
		}
		return value
	}
}

// scopedClient returns the client Status resolved when the caller passed its
// Scope on, or builds it.
func (s *Source) scopedClient(ctx context.Context, target source.Target) (promapi.Client, error) {
	if sc, ok := target.Scope.(scope); ok {
		return sc.api, nil
	}
	return s.client(ctx, target)
}

// QueryRange implements source.Source.
func (s *Source) QueryRange(ctx context.Context, target source.Target, query string, window source.Window) ([]source.Series, error) {
	api, err := s.scopedClient(ctx, target)
	if err != nil {
		return nil, err
	}
	promQL, err := promapi.RenderQuery(query, scopeSelector(target), window.Step)
	if err != nil {
		return nil, err
	}
	return api.QueryRange(ctx, promQL, window)
}

// ValidateQuery implements source.Source: without the selector a query would
// read every instance's metrics in PMM.
func (s *Source) ValidateQuery(query string) error {
	return promapi.ValidateQuery(query)
}
