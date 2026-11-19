package containers

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The tables the structures' containers roll, read from the data pack one by
// one as the shapes they use are generated: the mineshaft's biome-gated
// disc, the ice box's suspicious stew, the outpost's goat horn, a trial
// chamber pot and a trial chamber dispenser.
// vanilla: RandomizableContainer.unpackLootTable

// structChest places a chest rolling a structure's table under the seed.
func structChest(t *testing.T, w *servertest.WindowedClient, table string, seed int64) []protocol.ItemStack {
	t.Helper()
	servertest.Setblock(t, w.Client, 4, 64, 0,
		fmt.Sprintf(`minecraft:chest{LootTable:"%s",LootTableSeed:%dL}`, table, seed))
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	return slots[:27]
}

// TestAnAbandonedMineshaftChestFillsOnOpen: the mineshaft's table is
// generated, so its chest opens rolled, and the picture disc its last pool
// gates on the sulfur caves stays out of a chest in the plains.
func TestAnAbandonedMineshaftChestFillsOnOpen(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	slots := structChest(t, w, "minecraft:chests/abandoned_mineshaft", 7)

	rolled := 0
	for _, s := range slots {
		if s.Empty() {
			continue
		}
		rolled++
		if s.ItemID == servertest.MustItem("minecraft:music_disc_bounce") {
			t.Error("the chest rolled the sulfur-caves picture disc outside the biome")
		}
	}
	if rolled == 0 {
		t.Fatal("the abandoned_mineshaft chest opened empty")
	}
}

// TestAMineshaftChestInTheSulfurCavesRollsThePictureDisc: the same table
// rolls the disc where the chest's biome is the sulfur caves.
// vanilla: LocationCheck
func TestAMineshaftChestInTheSulfurCavesRollsThePictureDisc(t *testing.T) {
	t.Parallel()
	const table = "minecraft:chests/abandoned_mineshaft"
	disc := servertest.MustItem("minecraft:music_disc_bounce")
	sulfur := &rules.LootContext{BiomeAt: func(dx, dy, dz int32) string { return "minecraft:sulfur_caves" }}
	var seed int64
	for s := int64(1); seed == 0 && s < 500; s++ {
		slots := make([]protocol.ItemStack, 27)
		rules.FillLoot(table, sulfur, rules.NewJavaRandom(s), slots)
		for _, st := range slots {
			if !st.Empty() && st.ItemID == disc {
				seed = s
			}
		}
	}
	if seed == 0 {
		t.Fatal("no seed rolled the sulfur-caves disc")
	}
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	// The fill reaches to z=-4: the loot location_check reads the zoomed quart,
	// which for the chest at 4,64,0 lies at z=-1.
	srv.RunLine("fillbiome 0 60 -4 8 68 4 minecraft:sulfur_caves")
	w.AwaitPlay(protocol.IDPlayChunksBiomes)
	for _, st := range structChest(t, w, table, seed) {
		if !st.Empty() && st.ItemID == disc {
			return
		}
	}
	t.Error("the chest in the sulfur caves rolled no picture disc under the seed that rolls one there")
}

// TestAnIceBoxSuspiciousStewCarriesItsEffects: the ice box's stew arrives
// with the effect its table rolled, its seconds twentyfold in ticks.
// vanilla: SetStewEffectFunction.run
func TestAnIceBoxSuspiciousStewCarriesItsEffects(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	stew := servertest.MustItem("minecraft:suspicious_stew")
	nightVision := registry.MustMobEffect("minecraft:night_vision")
	blindness := registry.MustMobEffect("minecraft:blindness")

	var effects []protocol.StewEffect
	for seed := int64(1); seed <= 12 && len(effects) == 0; seed++ {
		for _, s := range structChest(t, w, "minecraft:chests/ancient_city_ice_box", seed) {
			if s.ItemID != stew {
				continue
			}
			effects = s.SuspiciousStewEffectsOf()
			if len(effects) == 0 {
				t.Fatal("the ice box's suspicious stew carries no effects")
			}
		}
	}
	if len(effects) == 0 {
		t.Fatal("no ice box chest rolled a suspicious stew")
	}
	if len(effects) != 1 {
		t.Fatalf("the stew carries %d effects, want the one its table rolled", len(effects))
	}
	// The table's two effects are night vision at 7-10 seconds and
	// blindness at 5-7, both non-instant, so both ride as ticks.
	if effects[0].Effect != nightVision && effects[0].Effect != blindness {
		t.Fatalf("the stew effect ID %d is neither night vision nor blindness", effects[0].Effect)
	}
	if effects[0].Duration < 100 || effects[0].Duration > 200 || effects[0].Duration%20 != 0 {
		t.Fatalf("the stew effect lasts %d ticks, want 5-10 seconds of twenty", effects[0].Duration)
	}
}

// TestAnOutpostGoatHornCarriesItsInstrument: the outpost's horn arrives
// playing one of the four regular goat horns the table names.
// vanilla: SetInstrumentFunction.run
func TestAnOutpostGoatHornCarriesItsInstrument(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	horn := servertest.MustItem("minecraft:goat_horn")
	regular := map[int32]bool{}
	for _, name := range []string{
		"minecraft:ponder_goat_horn", "minecraft:sing_goat_horn",
		"minecraft:seek_goat_horn", "minecraft:feel_goat_horn",
	} {
		regular[registry.MustID("minecraft:instrument", name)] = true
	}

	for seed := int64(1); seed <= 10; seed++ {
		for _, s := range structChest(t, w, "minecraft:chests/pillager_outpost", seed) {
			if s.ItemID != horn {
				continue
			}
			instrument, has := s.InstrumentOf()
			if !has {
				t.Fatal("the outpost's goat horn carries no instrument")
			}
			if !regular[instrument] {
				t.Fatalf("the horn's instrument is ID %d, which is no regular goat horn", instrument)
			}
			return
		}
	}
	t.Fatal("no outpost chest rolled a goat horn")
}

// TestATrialChamberPotRollsItsOwnTable: the pot whose table the trial
// chambers' pots carry rolls it on its first use.
// vanilla: DecoratedPotBlockEntity.getTheItem
func TestATrialChamberPotRollsItsOwnTable(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	const table = "minecraft:pots/trial_chambers/corridor"
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:decorated_pot{LootTable:"`+table+`",LootTableSeed:7L}`)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 3))

	want := make([]protocol.ItemStack, 1)
	if !rules.FillLoot(table, &rules.LootContext{}, rules.NewJavaRandom(7), want) || want[0].Empty() {
		t.Fatal("the trial chamber pot table rolled nothing into the pot's one slot")
	}
	usePot(t, w, protocol.HandMain)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || !got[0].Equal(want[0]) {
		t.Errorf("the pot holds %+v, want the rolled %+v", got, want)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the hand = %+v, want the three sticks kept", got)
	}
}

// TestATrialChamberDispenserFillsFromItsTable: the dispenser the trial
// chambers' water trap carries opens with its table's water bucket.
func TestATrialChamberDispenserFillsFromItsTable(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	servertest.Setblock(t, w.Client, 4, 64, 0,
		`minecraft:dispenser{LootTable:"minecraft:dispensers/trial_chambers/water",LootTableSeed:7L}`)

	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric3x3, true, 4, 64, 0)
	rolled := 0
	for _, s := range slots[:9] {
		if s.Empty() {
			continue
		}
		rolled++
		if s.ItemID != servertest.MustItem("minecraft:water_bucket") {
			t.Errorf("the dispenser slot holds item %d, want the rolled water bucket", s.ItemID)
		}
	}
	if rolled != 1 {
		t.Errorf("the dispenser filled %d slots, want the table's one roll", rolled)
	}
}

// TestASeaChestDiscardsAMapWithNoTreasure: the shipwreck map room and both
// underwater ruins roll a buried treasure map that the table keeps only once
// the search has given it a map. The flat fixture has no buried treasure, so
// the chest opens with the rest of its roll and no map.
// vanilla: ExplorationMapFunction.run, FilteredFunction.run
func TestASeaChestDiscardsAMapWithNoTreasure(t *testing.T) {
	t.Parallel()
	treasureMap := int32(registry.ItemBuriedTreasureMap)
	found := &rules.LootContext{ExplorationMap: func(_ registry.LootExplorationMap, stack *protocol.ItemStack) bool {
		stack.SetMapID(1)
		return true
	}}
	lost := &rules.LootContext{ExplorationMap: func(registry.LootExplorationMap, *protocol.ItemStack) bool { return false }}
	roll := func(table string, ctx *rules.LootContext, seed int64) []protocol.ItemStack {
		slots := make([]protocol.ItemStack, 27)
		rules.FillLoot(table, ctx, rules.NewJavaRandom(seed), slots)
		return slots
	}
	holdsMap := func(slots []protocol.ItemStack) bool {
		for _, s := range slots {
			if !s.Empty() && s.ItemID == treasureMap {
				return true
			}
		}
		return false
	}

	// The ruins roll the map in a pool shared with other entries, so a seed
	// that rolls it is found first; the shipwreck's first pool always does.
	for _, table := range []string{"minecraft:chests/shipwreck_map", "minecraft:chests/underwater_ruin_big", "minecraft:chests/underwater_ruin_small"} {
		var seed int64
		for s := int64(1); seed == 0 && s < 500; s++ {
			if holdsMap(roll(table, found, s)) {
				seed = s
			}
		}
		if seed == 0 {
			t.Fatalf("no seed rolled %s's buried treasure map", table)
		}
		want := roll(table, lost, seed)
		if holdsMap(want) {
			t.Fatalf("%s kept a buried treasure map its search never filled", table)
		}
		t.Run(table, func(t *testing.T) {
			_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
			defer w.Disconnect()
			got := structChest(t, w, table, seed)
			for i := range got {
				if !got[i].Equal(want[i]) {
					t.Errorf("the %s chest's slot %d = %+v, want %+v of the roll without its map", table, i, got[i], want[i])
				}
			}
		})
	}
}
