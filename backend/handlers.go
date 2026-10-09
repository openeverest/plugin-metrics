package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/openeverest/plugin-metrics/backend/internal/dashboard"
	"github.com/openeverest/plugin-metrics/backend/internal/source"
)

const defaultK8sCluster = "main"

type server struct {
	everest *everestClient
	catalog *dashboard.Catalog
	sources []source.Source
}

type instanceRef struct {
	k8sCluster string
	namespace  string
	name       string
}

type sourceInfo struct {
	Type string `json:"type"`
	source.Status
}

type panelInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Unit  string `json:"unit"`
}

type dashboardResponse struct {
	Source sourceInfo  `json:"source"`
	Panels []panelInfo `json:"panels"`
}

type panelDataResponse struct {
	Series []source.Series `json:"series"`
}

func badRequest(message string) error {
	return &statusError{status: http.StatusBadRequest, message: message}
}

// userToken returns the caller's token. X-Everest-User is the spec'd header;
// until the host sets it, the proxy forwards the user's Authorization header.
func userToken(r *http.Request) (string, error) {
	if v := r.Header.Get("X-Everest-User"); v != "" {
		return v, nil
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && token != "" {
		return token, nil
	}
	return "", &statusError{status: http.StatusUnauthorized, message: "missing auth token"}
}

func parseInstanceRef(r *http.Request) (instanceRef, error) {
	q := r.URL.Query()
	ref := instanceRef{
		k8sCluster: q.Get("k8sCluster"),
		namespace:  q.Get("namespace"),
		name:       q.Get("instance"),
	}
	if ref.k8sCluster == "" {
		ref.k8sCluster = defaultK8sCluster
	}
	if len(validation.IsDNS1123Subdomain(ref.k8sCluster)) > 0 {
		return ref, badRequest("invalid k8sCluster")
	}
	if len(validation.IsDNS1123Label(ref.namespace)) > 0 {
		return ref, badRequest("invalid namespace")
	}
	if len(validation.IsDNS1123Subdomain(ref.name)) > 0 {
		return ref, badRequest("invalid instance")
	}
	return ref, nil
}

// authorize resolves the target instance as the calling user.
func (s *server) authorize(r *http.Request) (source.Target, *instance, error) {
	token, err := userToken(r)
	if err != nil {
		return source.Target{}, nil, err
	}
	ref, err := parseInstanceRef(r)
	if err != nil {
		return source.Target{}, nil, err
	}
	in, err := s.everest.getInstance(r.Context(), token, ref)
	if err != nil {
		return source.Target{}, nil, err
	}
	return source.Target{Namespace: ref.namespace, Instance: ref.name}, in, nil
}

// activeSource returns the first source monitoring the target, or the first
// source's status so the UI can say why there is nothing to show.
func (s *server) activeSource(ctx context.Context, target source.Target) (source.Source, source.Status, error) {
	var first source.Status
	for i, src := range s.sources {
		status, err := src.Status(ctx, target)
		if err != nil {
			log.Printf("%s status for %s/%s: %v", src.Type(), target.Namespace, target.Instance, err)
			return nil, source.Status{}, &statusError{status: http.StatusBadGateway, message: "failed to check monitoring status"}
		}
		if status.Enabled {
			return src, status, nil
		}
		if i == 0 {
			first = status
		}
	}
	return s.sources[0], first, nil
}

// GET /api/dashboard?k8sCluster=&namespace=&instance=
func (s *server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	target, in, err := s.authorize(r)
	if err != nil {
		writeError(w, err)
		return
	}
	src, status, err := s.activeSource(r.Context(), target)
	if err != nil {
		writeError(w, err)
		return
	}

	response := dashboardResponse{Source: sourceInfo{Type: src.Type(), Status: status}, Panels: []panelInfo{}}
	if d, ok := s.catalog.ForProvider(in.Spec.ProviderRef.Name); ok && status.Enabled {
		for _, panel := range d.Panels {
			if _, ok := panel.Queries[src.Type()]; ok {
				response.Panels = append(response.Panels, panelInfo{ID: panel.ID, Title: panel.Title, Unit: panel.Unit})
			}
		}
	}
	writeJSON(w, response)
}

// GET /api/panels/{panel}?k8sCluster=&namespace=&instance=&range=
func (s *server) handlePanel(w http.ResponseWriter, r *http.Request) {
	window, err := parseWindow(r.URL.Query().Get("range"), time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	target, in, err := s.authorize(r)
	if err != nil {
		writeError(w, err)
		return
	}
	src, status, err := s.activeSource(r.Context(), target)
	if err != nil {
		writeError(w, err)
		return
	}
	if !status.Enabled {
		writeError(w, &statusError{status: http.StatusConflict, message: "no monitoring source collects metrics for this instance"})
		return
	}

	d, _ := s.catalog.ForProvider(in.Spec.ProviderRef.Name)
	panel, ok := d.Panel(r.PathValue("panel"))
	query, hasQuery := panel.Queries[src.Type()]
	if !ok || !hasQuery {
		writeError(w, &statusError{status: http.StatusNotFound, message: "unknown panel"})
		return
	}

	series, err := src.QueryRange(r.Context(), target, query, window)
	if err != nil {
		log.Printf("%s query for panel %q of %s/%s: %v", src.Type(), panel.ID, target.Namespace, target.Instance, err)
		writeError(w, &statusError{status: http.StatusBadGateway, message: "metrics query failed"})
		return
	}
	writeJSON(w, panelDataResponse{Series: series})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := "internal error"
	var se *statusError
	if errors.As(err, &se) {
		status, msg = se.status, se.message
	} else {
		log.Printf("request failed: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
