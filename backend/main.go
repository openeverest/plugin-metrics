package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openeverest/plugin-metrics/backend/internal/dashboard"
	"github.com/openeverest/plugin-metrics/backend/internal/source"
	"github.com/openeverest/plugin-metrics/backend/internal/source/prometheus"
)

// dist/main.js is copied from the frontend build during the Docker build.
//
//go:embed dist/main.js
var distFS embed.FS

var (
	bundleData    []byte
	bundleDataErr error
	bundleETag    string
	bundleOnce    sync.Once
)

func loadBundle() {
	bundleData, bundleDataErr = distFS.ReadFile("dist/main.js")
	if bundleDataErr == nil {
		sum := sha256.Sum256(bundleData)
		bundleETag = `"` + hex.EncodeToString(sum[:]) + `"`
	}
}

// GET /main.js — stable URL, so revalidate via a content ETag instead of a hashed filename.
func handleBundle(w http.ResponseWriter, r *http.Request) {
	bundleOnce.Do(loadBundle)
	if bundleDataErr != nil {
		http.Error(w, "bundle not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", bundleETag)
	if r.Header.Get("If-None-Match") == bundleETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(bundleData)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func kubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	// Local development outside the cluster.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func newMux(s *server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /main.js", handleBundle)
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /api/panels/{panel}", s.handlePanel)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/explore", s.handleExplore)
	return mux
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg, err := kubeConfig()
	if err != nil {
		log.Fatalf("kubernetes config: %v", err)
	}
	kube, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}
	catalog, err := dashboard.BuiltIn()
	if err != nil {
		log.Fatalf("dashboards: %v", err)
	}

	prometheusURL := envOrDefault("PROMETHEUS_URL", "http://kube-prometheus-stack-prometheus.monitoring.svc:9090")
	s := &server{
		everest: newEverestClient(everestAPIURL()),
		catalog: catalog,
		// Tried in order; the first one monitoring an instance serves it.
		sources: []source.Source{prometheus.New(prometheusURL, kube)},
	}
	srv := &http.Server{
		Addr:              ":" + envOrDefault("PORT", "8080"),
		Handler:           newMux(s),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("plugin-metrics backend listening on %s (everest API: %s, prometheus: %s)", srv.Addr, s.everest.baseURL, prometheusURL)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(fmt.Errorf("server error: %w", err))
	}
}
