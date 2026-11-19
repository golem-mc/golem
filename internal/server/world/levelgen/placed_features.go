package levelgen

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed placed_features.txt
var placedFeaturesData string

//go:embed sculk_features.txt
var sculkFeaturesData string

// PlacedFeatureJSON returns the data generator's JSON for a modeled overworld
// placed feature ("minecraft:" prefix optional), with its configured feature
// and any placed feature a selector names written in place.
// It is empty for a feature the file does not model.
// vanilla: PlacedFeature.DIRECT_CODEC
func PlacedFeatureJSON(name string) string {
	return placedFeatureTable()[strings.TrimPrefix(name, "minecraft:")]
}

var placedFeatureTable = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(placedFeaturesData+sculkFeaturesData, "\n") {
		if name, doc, ok := strings.Cut(line, "\t"); ok {
			out[name] = doc
		}
	}
	return out
})
