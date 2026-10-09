// Package dashboard holds the panels shown for each provider and the queries
// each monitoring source runs to fill them.
package dashboard

import (
	_ "embed"
	"fmt"

	"sigs.k8s.io/yaml"
)

//go:embed dashboards.yaml
var builtIn []byte

// Units the frontend knows how to format.
var units = map[string]bool{"": true, "cores": true, "bytes": true, "ops": true, "ms": true}

// Panel is one chart. Queries are keyed by source type.
type Panel struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Unit    string            `json:"unit"`
	Queries map[string]string `json:"queries"`
}

// Dashboard is the ordered list of panels for one provider.
type Dashboard struct {
	Panels []Panel `json:"panels"`
}

// Catalog maps provider names to their dashboards.
type Catalog struct {
	Dashboards map[string]Dashboard `json:"dashboards"`
}

// BuiltIn returns the dashboards shipped with the plugin.
func BuiltIn() (*Catalog, error) {
	return Parse(builtIn)
}

// Parse reads and validates a catalog.
func Parse(data []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.UnmarshalStrict(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse dashboards: %w", err)
	}
	for provider, dashboard := range catalog.Dashboards {
		seen := map[string]bool{}
		for _, panel := range dashboard.Panels {
			switch {
			case panel.ID == "" || panel.Title == "":
				return nil, fmt.Errorf("dashboard %q: every panel needs an id and a title", provider)
			case seen[panel.ID]:
				return nil, fmt.Errorf("dashboard %q: duplicate panel id %q", provider, panel.ID)
			case !units[panel.Unit]:
				return nil, fmt.Errorf("dashboard %q panel %q: unknown unit %q", provider, panel.ID, panel.Unit)
			case len(panel.Queries) == 0:
				return nil, fmt.Errorf("dashboard %q panel %q: no queries", provider, panel.ID)
			}
			seen[panel.ID] = true
		}
	}
	return &catalog, nil
}

// ForProvider returns the dashboard for a provider.
func (c *Catalog) ForProvider(provider string) (Dashboard, bool) {
	dashboard, ok := c.Dashboards[provider]
	return dashboard, ok
}

// Panel returns the panel with the given id.
func (d Dashboard) Panel(id string) (Panel, bool) {
	for _, panel := range d.Panels {
		if panel.ID == id {
			return panel, true
		}
	}
	return Panel{}, false
}
