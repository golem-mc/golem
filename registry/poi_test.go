package registry

import "testing"

func TestPOIMembershipMatchesTypes(t *testing.T) {
	t.Parallel()
	for id := range 1 << 16 {
		state := uint16(id)
		typ := poiTypeOfBlock[BlockOf(state)]
		if typ == "minecraft:home" && StateValue(state, PropPart) != "head" {
			typ = ""
		}
		if got := IsPOI(state); got != (typ != "") {
			t.Fatalf("state %d: membership %t, want type %q", state, got, typ)
		}
		if got := POITypeOf(state); got != typ {
			t.Fatalf("state %d: type %q, want %q", state, got, typ)
		}
	}
}

func TestPOITypeStateVariants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block Block
		typ   string
	}{
		{BlockLectern, "librarian"}, {BlockWaterCauldron, "leatherworker"},
		{BlockLavaCauldron, "leatherworker"}, {BlockPowderSnowCauldron, "leatherworker"},
		{BlockBeeNest, "bee_nest"}, {BlockBeehive, "beehive"},
		{BlockWaxedOxidizedLightningRod, "lightning_rod"}, {BlockNetherPortal, "nether_portal"},
		{BlockTestInstanceBlock, "test_instance"}, {BlockBell, "meeting"},
	} {
		for state := range uint16(StateCount) {
			if BlockOf(state) == tc.block && POITypeOf(state) != "minecraft:"+tc.typ {
				t.Fatalf("state %d: type %q, want %s", state, POITypeOf(state), tc.typ)
			}
		}
	}
	for state := range uint16(StateCount) {
		b := BlockOf(state)
		if !b.InGroup(GroupBed) {
			continue
		}
		want := ""
		if b != BlockStrawBed && StateValue(state, PropPart) == "head" {
			want = "minecraft:home"
		}
		if got := POITypeOf(state); got != want {
			t.Fatalf("bed state %d: type %q, want %q", state, got, want)
		}
	}
	if POITypeOf(BlockStone.DefaultState()) != "" {
		t.Fatal("stone is a POI")
	}
}
