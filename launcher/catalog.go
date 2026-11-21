package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

//go:embed catalog.json
var embeddedCatalog []byte

// catalogURL is where the current catalog is fetched from, so plugins added
// later show up without a launcher release.
var catalogURL = "https://raw.githubusercontent.com/golem-mc/golem/main/launcher/catalog.json"

const catalogTimeout = 3 * time.Second

// catalog lists the plugins plugin add offers.
type catalog struct {
	Plugins []catalogPlugin `json:"plugins"`
}

type catalogPlugin struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Path        string          `json:"path"`
	Modules     []catalogModule `json:"modules,omitempty"`
	Requires    []string        `json:"requires,omitempty"` // paths installed with it
}

type catalogModule struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

// parseCatalog decodes a catalog and checks that every requirement names a
// catalog path.
func parseCatalog(data []byte) (catalog, error) {
	var c catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return catalog{}, err
	}
	paths := map[string]bool{}
	for _, p := range c.Plugins {
		if p.Name == "" || p.Path == "" {
			return catalog{}, fmt.Errorf("catalog entry %q has no name or path", p.Name)
		}
		paths[p.Path] = true
		for _, m := range p.Modules {
			paths[m.Path] = true
		}
	}
	for _, p := range c.Plugins {
		for _, r := range p.Requires {
			if !paths[r] {
				return catalog{}, fmt.Errorf("%s requires %s, which the catalog does not list", p.Name, r)
			}
		}
	}
	return c, nil
}

// loadCatalog fetches the current catalog, and answers the embedded copy
// when that fails for any reason.
func loadCatalog(ctx context.Context) catalog {
	if c, err := fetchCatalog(ctx, catalogURL); err == nil {
		return c
	}
	c, err := parseCatalog(embeddedCatalog)
	if err != nil {
		panic("embedded catalog.json: " + err.Error())
	}
	return c
}

func fetchCatalog(ctx context.Context, url string) (catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return catalog{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return catalog{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return catalog{}, fmt.Errorf("catalog: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return catalog{}, err
	}
	return parseCatalog(data)
}
