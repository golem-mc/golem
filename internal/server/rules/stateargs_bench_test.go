package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkBoatSpot is a boat item's look straight down onto a pool's
// source: the clip's fluid surface height and the box check.
func BenchmarkBoatSpot(b *testing.B) {
	water := registry.MustState("minecraft:water", nil)
	pool := spotWorld{
		hits: func() (int32, int32, int32, bool, bool) { return 5, 63, 5, true, true },
		blocks: map[[3]int32]uint16{
			{5, 62, 5}: water, {5, 63, 5}: water,
			{4, 62, 5}: water, {6, 62, 5}: water,
			{5, 62, 4}: water, {5, 62, 6}: water,
			{4, 63, 5}: water, {6, 63, 5}: water,
			{5, 63, 4}: water, {5, 63, 6}: water,
		},
	}
	b.ReportAllocs()
	for range b.N {
		if x, y, z, ok := BoatSpot(pool, 5.5, 70, 5.5, 0, -1, 0, 4.5); !ok || !BoatFits(pool, x, y, z) {
			b.Fatal("the pool's spot was refused")
		}
	}
}

// BenchmarkCopperChestToolUse is an axe's scrape and a honeycomb's wax of a
// double copper chest's half, each reading the partner's offset.
func BenchmarkCopperChestToolUse(b *testing.B) {
	exposed := registry.MustState("minecraft:exposed_copper_chest", map[string]string{"facing": "north", "type": "left"})
	bare := registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "left"})
	b.ReportAllocs()
	for range b.N {
		if use, ok := AxeConversion(exposed); !ok || !use.HasPartner {
			b.Fatal("the scrape found no partner")
		}
		if use, ok := HoneycombWax(bare); !ok || !use.HasPartner {
			b.Fatal("the wax found no partner")
		}
	}
}

// BenchmarkSignFrontFacing is the side question of a click on a standing
// sign and on a wall sign.
func BenchmarkSignFrontFacing(b *testing.B) {
	standing := registry.MustState("minecraft:oak_sign", map[string]string{"rotation": "4"})
	wall := registry.MustState("minecraft:oak_wall_sign", map[string]string{"facing": "north"})
	b.ReportAllocs()
	for range b.N {
		for _, s := range []uint16{standing, wall} {
			SignFrontFacing(s, 4, 7, 4.5, 5.5)
		}
	}
}
