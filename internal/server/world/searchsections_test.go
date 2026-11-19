package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSearchSectionCountsFollowWritesAndDecode(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"nether_portal", "lectern", "water_cauldron", "blue_bed", "waxed_oxidized_lightning_rod", "bee_nest"} {
		t.Run(name, func(t *testing.T) {
			state := registry.MustState("minecraft:"+name, nil)
			if name == "blue_bed" {
				state, _ = registry.WithProp(state, registry.PropPart, "head")
			}
			portal := 0
			if name == "nether_portal" {
				portal = blocksPerSection
			}
			c := newChunk(-1, -1)
			c.Fill(-16, 0, state)
			check := func(c *Chunk, count, portalCount int) {
				t.Helper()
				s := &c.sections[3]
				if s.pois != count || s.portals != portalCount {
					t.Fatalf("counts = %d,%d; want %d,%d", s.pois, s.portals, count, portalCount)
				}
			}
			check(c, blocksPerSection, portal)
			root, err := encodeChunkNBT(c)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := decodeChunkNBT(root)
			if err != nil {
				t.Fatal(err)
			}
			check(loaded, blocksPerSection, portal)
			// A shared write takes the atomic-word path used by live chunks.
			c.shared = true
			c.SetBlock(0, -16, 0, airState)
			c.SetBlock(0, -16, 0, airState)
			check(c, blocksPerSection-1, max(0, portal-1))
			root, err = encodeChunkNBT(c)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err = decodeChunkNBT(root)
			if err != nil {
				t.Fatal(err)
			}
			check(loaded, blocksPerSection-1, max(0, portal-1))
			loaded.Fill(-16, 0, airState)
			check(loaded, 0, 0)
			loaded.sections[3].recalcCounts()
			check(loaded, 0, 0)
		})
	}
}

func TestSearchSectionMasksTrackLastRemovalAndBedParts(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	pos := ChunkPos{X: -1, Z: -1}
	if w.PortalSections(pos) != 0 || w.POISections(pos) != 0 {
		t.Fatal("unloaded chunk has matches")
	}
	w.Acquire(pos)
	portal := registry.BlockNetherPortal.DefaultState()
	head := registry.MustState("minecraft:blue_bed", map[string]string{"part": "head"})
	foot := registry.MustState("minecraft:blue_bed", map[string]string{"part": "foot"})
	for _, y := range []int32{-64, -1, 0, 319} {
		w.SetBlock(-1, y, -1, portal)
		if !w.PortalSections(pos).Has(y) || !w.POISections(pos).Has(y) {
			t.Fatalf("missed portal at %d", y)
		}
		w.SetBlock(-1, y, -1, head)
		if w.PortalSections(pos).Has(y) || !w.POISections(pos).Has(y) {
			t.Fatalf("head at %d miscounted", y)
		}
		w.SetBlock(-1, y, -1, foot)
		if w.POISections(pos).Has(y) {
			t.Fatalf("foot at %d counted as POI", y)
		}
	}
	if w.POISections(pos).Has(-65) || w.POISections(pos).Has(320) {
		t.Fatal("out of world section marked")
	}
}
