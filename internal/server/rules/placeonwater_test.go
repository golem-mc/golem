package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestPlacesOnWater(t *testing.T) {
	t.Parallel()
	for item, want := range map[registry.Item]bool{
		registry.ItemLilyPad: true, registry.ItemFrogspawn: true, registry.ItemStone: false,
	} {
		if got := PlacesOnWater(item); got != want {
			t.Errorf("PlacesOnWater(%v) = %v, want %v", item, got, want)
		}
	}
}
