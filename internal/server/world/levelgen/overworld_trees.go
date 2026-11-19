package levelgen

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed overworld_trees.txt
var overworldTreesData string

var (
	treeOnce       sync.Once
	treePlaced     map[string]string
	treeConfigured map[string]string
	treeProviders  map[string]string
)

// OverworldTreeFeature returns the data pack's JSON of a placed
// (placed = true) or configured tree, mushroom or selector feature of the
// overworld, by qualified name.
func OverworldTreeFeature(placed bool, name string) (string, bool) {
	loadTreeFeatures()
	m := treeConfigured
	if placed {
		m = treePlaced
	}
	js, ok := m[name]
	return js, ok
}

// OverworldStateProvider returns a registered block-state provider document.
func OverworldStateProvider(name string) (string, bool) {
	loadTreeFeatures()
	js, ok := treeProviders[name]
	return js, ok
}

func loadTreeFeatures() {
	treeOnce.Do(func() { treePlaced, treeConfigured, treeProviders = readTreeFeatures(overworldTreesData) })
}

func readTreeFeatures(data string) (placed, configured, providers map[string]string) {
	placed, configured, providers = map[string]string{}, map[string]string{}, map[string]string{}
	for line := range strings.SplitSeq(data, "\n") {
		kind, rest, ok := strings.Cut(line, " ")
		if !ok || kind == "#" {
			continue
		}
		name, doc, _ := strings.Cut(rest, " ")
		switch kind {
		case "placed":
			placed[name] = doc
		case "configured":
			configured[name] = doc
		case "provider":
			providers[name] = doc
		}
	}
	return
}
