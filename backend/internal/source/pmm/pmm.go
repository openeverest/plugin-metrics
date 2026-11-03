// Package pmm reads instance metrics from the PMM server the instance sends
// its metrics to: a MonitoringDestination of the PMM MonitoringClass, or the
// legacy MonitoringConfig. Providers label every series an instance pushes to
// PMM with its namespace and core_openeverest_io_instance, which scope the
// queries: one PMM server usually monitors many instances and namespaces.
package pmm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	// integration is the spec.providerManaged.integration of MonitoringClasses
	// whose destinations are PMM servers.
	integration = "pmm"
	// tokenAuthMode is the PMM MonitoringClass's authentication mode: the
	// destination's credentials Secret holds a PMM service account token
	// under tokenKey.
	tokenAuthMode = "ServiceAccountToken"
	tokenKey      = "token"
	// monitoringConfigTokenKey holds the PMM token in a MonitoringConfig's Secret.
	monitoringConfigTokenKey = "apiKey"
	requestTimeout           = 15 * time.Second
)

var (
	monitoringConfigGVR      = schema.GroupVersionResource{Group: "monitoring.openeverest.io", Version: "v1alpha1", Resource: "monitoringconfigs"}
	monitoringDestinationGVR = schema.GroupVersionResource{Group: "monitoring.openeverest.io", Version: "v1alpha1", Resource: "monitoringdestinations"}
	monitoringClassGVR       = schema.GroupVersionResource{Group: "monitoring.openeverest.io", Version: "v1alpha1", Resource: "monitoringclasses"}
	secretGVR                = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
)

var (
	errNotMonitored = errors.New("PMM monitoring is not enabled for this instance")
	errUnavailable  = errors.New("PMM is not reachable with the instance's monitoring settings")
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

// destinationSpec is the subset of a MonitoringDestination the plugin needs.
type destinationSpec struct {
	ClassRef struct {
		Name string `json:"name"`
	} `json:"classRef"`
	Authentication struct {
		Mode                 string `json:"mode"`
		CredentialsSecretRef *struct {
			Name string `json:"name"`
		} `json:"credentialsSecretRef"`
	} `json:"authentication"`
	TLS *struct {
		CA *struct {
			SecretKeyRef *struct {
				Name string `json:"name"`
				Key  string `json:"key"`
			} `json:"secretKeyRef"`
		} `json:"ca"`
		InsecureSkipVerify bool `json:"insecureSkipVerify"`
	} `json:"tls"`
	Parameters struct {
		URL string `json:"url"`
	} `json:"parameters"`
}

// endpoint is a PMM server and how to reach it.
type endpoint struct {
	url                string
	token              string
	insecureSkipVerify bool
	// caBundle is the PEM bundle the server certificate is verified against;
	// nil uses the system trust store.
	caBundle []byte
}

// Source queries the PMM server each instance sends its metrics to.
type Source struct {
	kube dynamic.Interface
	// Shared so that connections to a PMM server are reused across requests.
	verifiedHTTP *http.Client
	insecureHTTP *http.Client
	caHTTPMu     sync.Mutex
	// caHTTP holds a client per CA bundle a destination verifies PMM with.
	caHTTP map[string]*http.Client
}

// New returns a Source that reads the instances' PMM settings and their
// Secrets with kube.
func New(kube dynamic.Interface) *Source {
	insecure := http.DefaultTransport.(*http.Transport).Clone()
	// Only used for PMM servers whose settings opt out of verification.
	insecure.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	return &Source{
		kube:         kube,
		verifiedHTTP: &http.Client{Timeout: requestTimeout},
		insecureHTTP: &http.Client{Timeout: requestTimeout, Transport: insecure},
		caHTTP:       map[string]*http.Client{},
	}
}

// Type implements source.Source.
func (s *Source) Type() string { return Type }

// scope is the Status result queries reuse instead of reading the instance's
// PMM settings and their Secrets again.
type scope struct {
	api promapi.Client
}

// Status implements source.Source: an instance is monitored when it sends
// metrics to a PMM MonitoringDestination or references a PMM
// MonitoringConfig. The client for that PMM server becomes the Scope.
func (s *Source) Status(ctx context.Context, target source.Target) (source.Status, error) {
	if target.MonitoringConfig == "" && len(target.MonitoringDestinations) == 0 {
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
	server, err := s.endpoint(ctx, target)
	if err != nil {
		return promapi.Client{}, err
	}
	baseURL, err := apiURL(server.url)
	if err != nil {
		return promapi.Client{}, err
	}
	httpClient, err := s.httpClient(server)
	if err != nil {
		return promapi.Client{}, err
	}
	return promapi.Client{
		BaseURL: baseURL,
		HTTP:    httpClient,
		Token:   server.token,
		// VictoriaMetrics applies the scope to every read even if a query
		// misses it. PMM drops it only for tokens with label-based access
		// control, whose own filters then confine the query instead.
		ExtraFilters: []string{"{" + scopeSelector(target) + "}"},
		DisplayValue: displayValue(target.Namespace),
	}, nil
}

// endpoint finds the PMM server the target sends its metrics to.
func (s *Source) endpoint(ctx context.Context, target source.Target) (endpoint, error) {
	if target.MonitoringConfig != "" {
		return s.monitoringConfigEndpoint(ctx, target)
	}
	return s.destinationEndpoint(ctx, target)
}

// destinationEndpoint returns the first of the target's MonitoringDestinations
// whose MonitoringClass is PMM's.
func (s *Source) destinationEndpoint(ctx context.Context, target source.Target) (endpoint, error) {
	for _, name := range target.MonitoringDestinations {
		destination, err := s.kube.Resource(monitoringDestinationGVR).Namespace(target.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// Without it there is no class to tell whether it is PMM; the
			// Instance's status reports the missing destination.
			continue
		}
		if err != nil {
			return endpoint{}, fmt.Errorf("get MonitoringDestination %q: %w", name, err)
		}
		rawSpec, _, _ := unstructured.NestedMap(destination.Object, "spec")
		var spec destinationSpec
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rawSpec, &spec); err != nil {
			return endpoint{}, fmt.Errorf("MonitoringDestination %q: %w", name, err)
		}
		isPMM, err := s.isPMMClass(ctx, spec.ClassRef.Name)
		if err != nil {
			return endpoint{}, err
		}
		if isPMM {
			return s.pmmDestination(ctx, target.Namespace, name, spec)
		}
	}
	return endpoint{}, errNotMonitored
}

// isPMMClass reports whether the provider's PMM integration fulfils a
// MonitoringClass, whatever the class is named.
func (s *Source) isPMMClass(ctx context.Context, name string) (bool, error) {
	class, err := s.kube.Resource(monitoringClassGVR).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get MonitoringClass %q: %w", name, err)
	}
	got, _, _ := unstructured.NestedString(class.Object, "spec", "providerManaged", "integration")
	return got == integration, nil
}

// pmmDestination reads how to reach the PMM server of a destination.
func (s *Source) pmmDestination(ctx context.Context, namespace, name string, spec destinationSpec) (endpoint, error) {
	auth := spec.Authentication
	if auth.Mode != tokenAuthMode || auth.CredentialsSecretRef == nil {
		return endpoint{}, fmt.Errorf("%w: MonitoringDestination %q uses unsupported authentication mode %q", errUnavailable, name, auth.Mode)
	}
	token, err := s.secretValue(ctx, namespace, auth.CredentialsSecretRef.Name, tokenKey)
	if err != nil {
		return endpoint{}, err
	}
	server := endpoint{url: spec.Parameters.URL, token: token}
	if spec.TLS == nil {
		return server, nil
	}
	server.insecureSkipVerify = spec.TLS.InsecureSkipVerify
	if ca := spec.TLS.CA; ca != nil && ca.SecretKeyRef != nil {
		bundle, err := s.secretValue(ctx, namespace, ca.SecretKeyRef.Name, ca.SecretKeyRef.Key)
		if err != nil {
			return endpoint{}, err
		}
		server.caBundle = []byte(bundle)
	}
	return server, nil
}

// monitoringConfigEndpoint reads how to reach the PMM server of the target's
// MonitoringConfig.
func (s *Source) monitoringConfigEndpoint(ctx context.Context, target source.Target) (endpoint, error) {
	config, err := s.kube.Resource(monitoringConfigGVR).Namespace(target.Namespace).Get(ctx, target.MonitoringConfig, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return endpoint{}, fmt.Errorf("%w: MonitoringConfig %q not found", errUnavailable, target.MonitoringConfig)
	}
	if err != nil {
		return endpoint{}, fmt.Errorf("get MonitoringConfig %q: %w", target.MonitoringConfig, err)
	}
	rawSpec, _, _ := unstructured.NestedMap(config.Object, "spec")
	var spec monitoringConfigSpec
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rawSpec, &spec); err != nil {
		return endpoint{}, fmt.Errorf("MonitoringConfig %q: %w", target.MonitoringConfig, err)
	}
	if spec.Type != Type || spec.PMM == nil {
		return endpoint{}, errNotMonitored
	}
	token, err := s.secretValue(ctx, target.Namespace, spec.PMM.CredentialsSecretRef.Name, monitoringConfigTokenKey)
	if err != nil {
		return endpoint{}, err
	}
	return endpoint{
		url:                spec.PMM.URL,
		token:              token,
		insecureSkipVerify: spec.PMM.VerifyTLS != nil && !*spec.PMM.VerifyTLS,
	}, nil
}

// apiURL returns PMM's Prometheus API base for the PMM server URL.
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

// httpClient returns a client that verifies the PMM server's certificate the
// way the endpoint asks.
func (s *Source) httpClient(server endpoint) (*http.Client, error) {
	switch {
	case server.insecureSkipVerify:
		return s.insecureHTTP, nil
	case server.caBundle == nil:
		return s.verifiedHTTP, nil
	}
	s.caHTTPMu.Lock()
	defer s.caHTTPMu.Unlock()
	if client, ok := s.caHTTP[string(server.caBundle)]; ok {
		return client, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(server.caBundle) {
		return nil, fmt.Errorf("%w: the PMM CA bundle has no PEM certificate", errUnavailable)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	client := &http.Client{Timeout: requestTimeout, Transport: transport}
	s.caHTTP[string(server.caBundle)] = client
	return client, nil
}

// secretValue reads one key of a Secret in namespace.
func (s *Source) secretValue(ctx context.Context, namespace, secretName, key string) (string, error) {
	secret, err := s.kube.Resource(secretGVR).Namespace(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", fmt.Errorf("%w: Secret %q not found", errUnavailable, secretName)
	}
	if err != nil {
		return "", fmt.Errorf("get Secret %q: %w", secretName, err)
	}
	encoded, _, _ := unstructured.NestedString(secret.Object, "data", key)
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(value) == 0 {
		return "", fmt.Errorf("%w: Secret %q has no %s", errUnavailable, secretName, key)
	}
	return string(value), nil
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
