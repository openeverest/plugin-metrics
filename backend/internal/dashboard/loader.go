package dashboard

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"slices"
	"sync"
	"time"
)

// QueryValidator rejects a query a source of the given type could not run safely.
type QueryValidator func(sourceType, query string) error

// Loader serves the built-in dashboards overlaid by an optional override file,
// typically a mounted ConfigMap. The file is re-read when it changes, so
// editing the ConfigMap needs no restart; a broken edit keeps the last good one.
type Loader struct {
	builtIn  *Catalog
	path     string
	validate QueryValidator

	mu      sync.Mutex
	modTime time.Time
	current *Catalog
}

// NewLoader validates the built-in catalog and loads the override file, if any.
func NewLoader(builtIn *Catalog, path string, validate QueryValidator) (*Loader, error) {
	if err := validateQueries(builtIn, validate); err != nil {
		return nil, fmt.Errorf("built-in dashboards: %w", err)
	}
	l := &Loader{builtIn: builtIn, path: path, validate: validate, current: builtIn}
	l.Catalog()
	return l, nil
}

// Catalog returns the effective dashboards.
func (l *Loader) Catalog() *Catalog {
	if l.path == "" {
		return l.builtIn
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// A ConfigMap mount swaps the file atomically, which changes its modification time.
	info, err := os.Stat(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		l.current, l.modTime = l.builtIn, time.Time{}
		return l.current
	}
	if err != nil {
		log.Printf("dashboards: keeping the current dashboards, cannot read %s: %v", l.path, err)
		return l.current
	}
	if info.ModTime().Equal(l.modTime) {
		return l.current
	}
	l.modTime = info.ModTime()

	overrides, err := l.loadOverrides()
	if err != nil {
		log.Printf("dashboards: keeping the current dashboards, %s is invalid: %v", l.path, err)
		return l.current
	}
	l.current = l.builtIn.overlay(overrides)
	log.Printf("dashboards: loaded %s for %v", l.path, slices.Sorted(maps.Keys(overrides.Dashboards)))
	return l.current
}

func (l *Loader) loadOverrides() (*Catalog, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, err
	}
	overrides, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return overrides, validateQueries(overrides, l.validate)
}

// overlay replaces whole dashboards by provider; an empty one hides the built-in.
func (c *Catalog) overlay(overrides *Catalog) *Catalog {
	merged := &Catalog{Dashboards: maps.Clone(c.Dashboards)}
	if merged.Dashboards == nil {
		merged.Dashboards = map[string]Dashboard{}
	}
	maps.Copy(merged.Dashboards, overrides.Dashboards)
	return merged
}

func validateQueries(catalog *Catalog, validate QueryValidator) error {
	for provider, dashboard := range catalog.Dashboards {
		for _, panel := range dashboard.Panels {
			for sourceType, query := range panel.Queries {
				if err := validate(sourceType, query); err != nil {
					return fmt.Errorf("dashboard %q panel %q (%s): %w", provider, panel.ID, sourceType, err)
				}
			}
		}
	}
	return nil
}
