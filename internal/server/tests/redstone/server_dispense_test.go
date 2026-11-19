package redstone

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDispensedItemCanBePickedUpImmediately(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:dropper", "minecraft:dispenser"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Collector")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(4.5, 64, 1.5, true)
			c.Settle()
			if !srv.PlaceContainer(4, 64, 0, block) {
				t.Fatal("could not place container")
			}
			srv.SetBlock(4, 64, 0, servertest.MustState(t, block, map[string]string{"facing": "south"}))
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
				t.Fatal("could not fill container")
			}

			srv.SetBlockAndTell(4, 64, -1, servertest.MustState(t, "minecraft:redstone_block", nil))
			// Redstone schedules the dispense four ticks after the rising edge.
			drop := servertest.ParseItemSpawn(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySpawnEntity))
			if drop.EntityID == 0 {
				t.Fatal("the powered container did not spawn an item")
			}
			collected, collector, count := servertest.ReadPickupItem(c, c.ExpectWithinTicks(srv, 3, protocol.IDPlayPickupItem))
			if collected != drop.EntityID || collector != c.PlayerEntityID || count != 1 {
				t.Fatalf("pickup = (%d, %d, %d), want (%d, %d, 1)", collected, collector, count, drop.EntityID, c.PlayerEntityID)
			}
			if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
				t.Errorf("container still holds %+v", got)
			}
		})
	}
}

// The dispenser's behaviours, driven by hand out of a dispenser facing
// south over the flat world's ground: the projectiles the vanilla
// dispense bootstrap shots (the thrown eggs and snowballs, the tipped and
// the spectral arrows), the shears at a hive and a body, and the flint and
// steel's fire and fuses. Each visit comes through the export hook's
// Dispense, the rising edge the redstone engine would book; the sounds
// and the clicks the visits play are what the connected client hears.

// tippedArrowWithEffect is a stack of minecraft:tipped_arrow carrying one
// custom effect as its potion_contents component, the layout of the wire:
// no potion named, no custom colour, the effect's amplifier and duration,
// the draw booleans on and no custom name.
func tippedArrowWithEffect(t *testing.T, effect string, amplifier, duration int32) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(false) // no potion
	value.PutBool(false) // no custom colour
	value.PutVarInt(1)   // one custom effect
	value.PutVarInt(registry.MustMobEffect(effect))
	value.PutVarInt(amplifier)
	value.PutVarInt(duration)
	value.PutBool(false) // not ambient
	value.PutBool(true)  // show particles
	value.PutBool(true)  // show icon
	value.PutBool(false) // no hidden twin
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:tipped_arrow"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// TestADispenserThrowsItsEgg checks the egg throw: each of the three eggs
// of the game, dispensed out of the south face, births a thrown egg out of
// the face drawn as the item it was, and the slot is spent.
func TestADispenserThrowsItsEgg(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	for _, item := range []string{"minecraft:egg", "minecraft:brown_egg", "minecraft:blue_egg"} {
		if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, item, 2)) {
			t.Fatalf("the dispenser has no block entity to fill with %s", item)
		}
		srv.Dispense(4, 64, 0, protocol.FaceSouth)

		seen := w.ExpectSpawned(protocol.EntityTypeEgg)
		// Born 0.7 along the face and a tenth above the line, moving the
		// way the face points.
		if seen.X < 4.4 || seen.X > 4.6 || seen.Y < 64.55 || seen.Y > 64.65 || seen.Z < 1.1 || seen.Z > 1.3 {
			t.Errorf("the %s was born at (%.3f, %.3f, %.3f), want it out of the south face's line", item, seen.X, seen.Y, seen.Z)
		}
		if seen.Vz < 0.5 || seen.Vz > 1.6 {
			t.Errorf("the %s flies at (%.2f, %.2f, %.2f), want it southbound at the config's power", item, seen.Vx, seen.Vy, seen.Vz)
		}
		if got := w.ExpectMetadataSlot(seen.EntityID); got.ItemID != servertest.ItemID(t, item) || got.Count != 1 {
			name, _ := registry.ItemName(got.ItemID)
			t.Errorf("the %s is drawn as %s x%d, want the one it was dispensed from", item, name, got.Count)
		}
	}
	thrown := srv.ThrownInDimension(world.DimensionName)
	if len(thrown) != 3 || thrown[0].Kind != "minecraft:egg" {
		t.Fatalf("the three eggs left %d thrown bodies, want three eggs (%v)", len(thrown), thrown)
	}

	// A projectile's dispense sounds the launch event, never the dispense
	// click, and none of the eggs dropped.
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the throws played no launch event; events were %v", w.Events)
	}
	if slices.Contains(w.Events, protocol.WorldEventDispense) {
		t.Errorf("the throws played the dispense click; events were %v", w.Events)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the eggs left %d items behind, want none dropped", len(items))
	}
}

// TestADispenserThrowsItsSnowball checks the snowball throw: the visit
// births a thrown snowball out of the face, drawn as a snowball, and the
// slot is spent.
func TestADispenserThrowsItsSnowball(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:snowball", 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	seen := w.ExpectSpawned(protocol.EntityTypeSnowball)
	if seen.Vz < 0.5 || seen.Vz > 1.6 || seen.Z < 1.1 || seen.Z > 1.3 {
		t.Errorf("the snowball was born at (%.3f, %.3f, %.3f) flying (%.2f, %.2f, %.2f), want it aimed south out of the face", seen.X, seen.Y, seen.Z, seen.Vx, seen.Vy, seen.Vz)
	}
	if got := w.ExpectMetadataSlot(seen.EntityID); got.ItemID != servertest.ItemID(t, "minecraft:snowball") {
		name, _ := registry.ItemName(got.ItemID)
		t.Errorf("the snowball is drawn as %s, want the item itself", name)
	}
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the throw played no launch event; events were %v", w.Events)
	}

	thrown := srv.ThrownInDimension(world.DimensionName)
	if len(thrown) != 1 || thrown[0].Kind != "minecraft:snowball" {
		t.Fatalf("the dispense left %v, want one thrown snowball", thrown)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want one snowball spent of two", got)
	}
}

// TestADispenserThrowsItsSplashPotion checks the potion throw: the visit
// births a thrown splash potion out of the face, drawn as the potion it was
// dispensed from with the brewing's contents riding along — the shatter's
// own reading — and the slot is spent.
func TestADispenserThrowsItsSplashPotion(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.PotionStack(t, "minecraft:splash_potion", servertest.PoisonBrewing, 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	seen := w.ExpectSpawned(protocol.EntityTypeSplashPotion)
	// The potion's own dispense config: power 1.375 (the default's 1.1 a
	// quarter more), uncertainty 3.0. The per-axis spread of ±3.0×0.0172275
	// leaves the magnitude near 1.375, well clear of the egg's 1.1.
	speed := math.Sqrt(seen.Vx*seen.Vx + seen.Vy*seen.Vy + seen.Vz*seen.Vz)
	if speed < 1.28 || speed > 1.47 || seen.Z < 1.1 || seen.Z > 1.3 {
		t.Errorf("the potion was born at (%.3f, %.3f, %.3f) flying (%.2f, %.2f, %.2f), want it aimed south out of the face at the potion config's power", seen.X, seen.Y, seen.Z, seen.Vx, seen.Vy, seen.Vz)
	}
	got := w.ExpectMetadataSlot(seen.EntityID)
	if got.ItemID != servertest.ItemID(t, "minecraft:splash_potion") || got.Count != 1 {
		name, _ := registry.ItemName(got.ItemID)
		t.Errorf("the potion is drawn as %s x%d, want the one it was dispensed from", name, got.Count)
	}
	contents, ok := got.PotionContentsOf()
	if !ok || contents.Potion != servertest.PoisonBrewing {
		t.Errorf("the thrown potion's contents are %+v (read %v), want the poison it was brewed with", contents, ok)
	}
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the throw played no launch event; events were %v", w.Events)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the potion left %d items behind, want none dropped", len(items))
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want one potion spent of two", got)
	}
}

// TestADispenserThrowsItsBottleOEnchanting checks the bottle's throw: the
// visit births a thrown experience bottle at the potion config's power, the
// slot is spent, and the bottle bursts into orbs where it lands.
// vanilla: ExperienceBottleItem.createDispenseConfig
func TestADispenserThrowsItsBottleOEnchanting(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:experience_bottle", 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	seen := w.ExpectSpawned(protocol.EntityTypeExperienceBottle)
	speed := math.Sqrt(seen.Vx*seen.Vx + seen.Vy*seen.Vy + seen.Vz*seen.Vz)
	if speed < 1.28 || speed > 1.47 || seen.Z < 1.1 || seen.Z > 1.3 {
		t.Errorf("the bottle was born at (%.3f, %.3f, %.3f) flying (%.2f, %.2f, %.2f), want it aimed south out of the face at the potion config's power", seen.X, seen.Y, seen.Z, seen.Vx, seen.Vy, seen.Vz)
	}
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the throw played no launch event; events were %v", w.Events)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want one bottle spent of two", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the bottle left %d items behind, want none dropped", len(items))
	}

	// 200 ticks is a hang guard; the bottle lands within a few.
	for range 200 {
		if srv.OrbCount() > 0 {
			break
		}
		srv.TickEntities(1)
	}
	if srv.OrbCount() == 0 {
		t.Error("the bottle never burst into orbs")
	}
	if thrown := srv.ThrownInDimension(world.DimensionName); len(thrown) != 0 {
		t.Errorf("%d thrown bodies remain, want the bottle gone", len(thrown))
	}
}

// TestADispenserShootsItsTippedArrow checks the tipped arrow's shot: the
// body goes out as the arrow entity, carrying the poison it was brewed as
// as its colour metadata.
func TestADispenserShootsItsTippedArrow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.TippedArrow(t, "minecraft:poison")) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	seen := w.ExpectSpawned(protocol.EntityTypeArrow)
	// The colour of the poison brewing: the effect's own, opaque.
	poisonID, _ := registry.MobEffectID("minecraft:poison")
	wantColour := int32(uint32(0xFF000000) | uint32(registry.MobEffectOf(poisonID).Color))
	data := w.ExpectPlay(protocol.IDPlayEntityMetadata)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != seen.EntityID {
		t.Fatalf("metadata for entity %d, want the arrow just shot (%d)", got, seen.EntityID)
	}
	if index := r.ReadU8(); index != protocol.MetadataIndexArrowColor {
		t.Fatalf("the arrow's metadata field %d, want its colour", index)
	}
	if fieldType := r.ReadVarInt(); fieldType != protocol.MetadataVarInt {
		t.Fatalf("the arrow's colour type %d, want a varint", fieldType)
	}
	if got := r.ReadVarInt(); got != wantColour {
		t.Errorf("the arrow's colour is %#x, want the poison's %#x", uint64(uint32(got)), uint64(uint32(wantColour)))
	}
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the shot played no launch event; events were %v", w.Events)
	}
	if arrows := srv.ArrowsInDimension(world.DimensionName); len(arrows) != 1 {
		t.Fatalf("the dispense left %d arrows, want the one it shot", len(arrows))
	}
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the slot spent", got)
	}
}

// TestADispenserShootsItsSpectralArrow checks the spectral arrow's shot:
// the body goes out as the spectral arrow entity type — its flight and
// metadata the plain arrow's — and the slot is spent.
func TestADispenserShootsItsSpectralArrow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:spectral_arrow", 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	seen := w.ExpectSpawned(protocol.EntityTypeSpectralArrow)
	if seen.Vz < 0.5 || seen.Vz > 1.6 {
		t.Errorf("the spectral arrow flies at (%.2f, %.2f, %.2f), want it southbound at the config's power", seen.Vx, seen.Vy, seen.Vz)
	}
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventProjectileLaunch) {
		t.Errorf("the shot played no launch event; events were %v", w.Events)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the spectral arrow left %d items, want none dropped", len(items))
	}
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("the dispense left %d arrows, want the one it shot", len(arrows))
	}
	// The selector's snapshot names the shaft the spectral type, so
	// @e[type=spectral_arrow] matches it.
	if got := srv.ArrowTypeName(arrows[0].ID); got != "minecraft:spectral_arrow" {
		t.Errorf("@e[type=spectral_arrow] sees the shaft as %q, want minecraft:spectral_arrow", got)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want one spectral arrow spent of two", got)
	}
}

// TestADispenserShearsASheep checks the shear at a body: the wool comes off
// the sheep in the target cell as item entities, the shears wear one and
// stay, and the visit plays the dispense click of a success.
func TestADispenserShearsASheep(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	// A sheep standing in the cell the face opens onto.
	srv.SpawnMob("minecraft:sheep", 4.5, 64, 1.5)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventDispense) {
		t.Errorf("the shear played no success click; events were %v", w.Events)
	}
	if slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("the shear played the fail click; events were %v", w.Events)
	}
	var wool int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:white_wool" {
			wool += int(it.Count)
		}
	}
	if wool < 1 {
		t.Errorf("the shear left %d wool on the ground, want the fleece it cut", wool)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Count != 1 || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the shears back worn one", got)
	}
}

// TestADispenserShearsABogged checks the shear at a bogged: its mushrooms
// come off at the top of its body and the shears wear and stay.
func TestADispenserShearsABogged(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SpawnMob("minecraft:bogged", 4.5, 64, 1.5)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	var mushrooms int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:red_mushroom" || it.Item == "minecraft:brown_mushroom" {
			mushrooms += int(it.Count)
		}
	}
	if mushrooms < 1 {
		t.Errorf("the shear left %d mushrooms on the ground, want what it cut", mushrooms)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the shears back worn one", got)
	}
}

// placeBeeHive puts a full hive holding one bee at the cell south of the
// dispenser, facing the given way, at midday. The default north faces the
// dispenser, which blocks the way out.
func placeBeeHive(t *testing.T, srv *server.Server, facing string) {
	t.Helper()
	srv.SetDayTime(6000)
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:beehive", map[string]string{"honey_level": "5", "facing": facing}))
	srv.SetBlockEntityTagAt(4, 64, 1, "minecraft:beehive", nbt.Compound{"bees": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"entity_data": nbt.Compound{"id": nbt.String("minecraft:bee")}, "ticks_in_hive": nbt.Int(5), "min_ticks_in_hive": nbt.Int(600)},
	}}})
}

// TestADispenserKeepsHiveBeesIn checks that a dispenser's shear or bottle
// still empties the hive and yields its item, but the bee stays inside at
// night, in rain, or when the hive's front block has collision.
// vanilla: BeehiveBlockEntity.releaseOccupant (BEE_RELEASED)
func TestADispenserKeepsHiveBeesIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		facing string
		setup  func(srv *server.Server)
	}{
		{"front blocked", "north", func(*server.Server) {}},
		{"night", "south", func(srv *server.Server) { srv.SetDayTime(servertest.Midnight) }},
		{"rain", "south", func(srv *server.Server) {
			srv.SetWeatherState(true, false, 100000, 100000, 0)
			srv.TickWeather() // the rain level fades in from 0
		}},
	}
	for _, tc := range cases {
		for _, tool := range []string{"minecraft:shears", "minecraft:glass_bottle"} {
			t.Run(tc.name+"/"+tool, func(t *testing.T) {
				t.Parallel()
				srv, w := servertest.Dispensing(t)
				placeBeeHive(t, srv, tc.facing)
				tc.setup(srv)
				if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, tool, 1)) {
					t.Fatal("the dispenser has no block entity to fill")
				}
				w.DrainRecorded()
				srv.Dispense(4, 64, 0, protocol.FaceSouth)
				w.DrainRecorded()
				if _, props := servertest.StatePropsOf(t, srv, 4, 64, 1); props["honey_level"] != "0" {
					t.Errorf("the hive stands with %v, want its honey emptied", props)
				}
				if bees := srv.MobIDsOfKind("minecraft:bee"); len(bees) != 0 {
					t.Errorf("%d bees came out, want the stored one kept inside", len(bees))
				}
			})
		}
	}
}

// TestADispenserShearsAFullBeehive checks the shear at a hive: a hive the
// face opens onto, full of honey, loses its honey level and drops its
// honeycomb, frees its stored bee, and the shears wear and stay.
func TestADispenserShearsAFullBeehive(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	placeBeeHive(t, srv, "south")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	name, category, _, _, _, _, _ := w.ExpectContainerSound()
	if name != "minecraft:block.beehive.shear" || category != protocol.SoundCategoryBlock {
		t.Errorf("the shear sounded as %q in category %d, want the hive's in the blocks category", name, category)
	}
	w.DrainRecorded()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:beehive" || props["honey_level"] != "0" {
		t.Errorf("the hive stands as %s %v, want its honey level emptied", name, props)
	}
	var comb []server.TestItem
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:honeycomb" {
			comb = append(comb, it)
		}
	}
	if bees := srv.MobIDsOfKind("minecraft:bee"); len(bees) != 1 {
		t.Errorf("%d bees out after the shear, want the stored one", len(bees))
	}
	if len(comb) != 1 || comb[0].Count != 3 {
		t.Errorf("the shear left %v, want one stack of three honeycomb", comb)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the shears back worn one", got)
	}
}

// TestADispenserKeepsItsShearsWithNothingToShear checks the shear's
// failure: a face opening onto plain air keeps the shears unworn behind
// the hollow click, and nothing drops.
func TestADispenserKeepsItsShearsWithNothingToShear(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("the empty shear played no fail click; events were %v", w.Events)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Count != 1 || got[0].Damage != 0 {
		t.Errorf("the dispenser holds %+v, want the shears back unworn", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the shear left %d items, want none dropped", len(items))
	}
}

// TestADispenserLightsAFireWithItsFlintAndSteel checks the flint and
// steel's dispense into air it survives in: the fire stands in the target
// cell, wearing the flint and steel one but keeping it, behind the success
// click of a behaviour vanilla never marks failed.
func TestADispenserLightsAFireWithItsFlintAndSteel(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:fire" || props["age"] != "0" {
		t.Errorf("the target stands as %s %v, want a fresh fire", name, props)
	}
	if !slices.Contains(w.Events, protocol.WorldEventDispense) {
		t.Errorf("the dispense played no success click; events were %v", w.Events)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Count != 1 || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the flint and steel back worn one", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the dispense left %d items, want none dropped", len(items))
	}
}

// TestADispensersFlintAndSteelRollsUnbreaking checks that a dispensed tool
// wears through the enchantment roll a held one takes: Unbreaking III spares
// each point with chance 3/4, a plain flint and steel wears on every use.
// vanilla: ItemStack.hurtAndBreak / EnchantmentHelper.processDurabilityChange
func TestADispensersFlintAndSteelRollsUnbreaking(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		unbroken bool
	}{{"unbreaking", true}, {"plain", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			stack := servertest.ItemStack(t, "minecraft:flint_and_steel", 1)
			if tc.unbroken {
				stack = servertest.EnchantedStack(t, "minecraft:flint_and_steel", "minecraft:unbreaking", 3)
			}
			if !srv.SetInventoryAt(4, 64, 0, 0, stack) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			// The first use lights a fire in the target cell; the later ones
			// face the fire and still count as a use.
			for range 40 {
				srv.Dispense(4, 64, 0, protocol.FaceSouth)
				w.DrainRecorded()
			}
			got := srv.InventoryAt(4, 64, 0)
			if servertest.NothingIn(got) || got[0].ItemID != stack.ItemID {
				t.Fatalf("the dispenser holds %+v, want the flint and steel back", got)
			}
			if tc.unbroken {
				// Wearing all 40 times has probability 4^-40.
				if got[0].Damage < 1 || got[0].Damage >= 40 {
					t.Errorf("Unbreaking III flint and steel has Damage %d after 40 uses, want 1..39", got[0].Damage)
				}
			} else if got[0].Damage != 40 {
				t.Errorf("plain flint and steel has Damage %d after 40 uses, want 40", got[0].Damage)
			}
		})
	}
}

// TestADispenserLightsItsCampfireAndCandle checks the flint and steel's
// dispense onto the wicks vanilla lights in place of a fire: an unlit
// campfire and an unlit candle both take the lit state, the flint and
// steel wearing with each visit.
func TestADispenserLightsItsCampfireAndCandle(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"}))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 1); props["lit"] != "true" {
		t.Errorf("the campfire stands %v, want it lit", props)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the flint and steel worn once", got)
	}

	w.DrainRecorded()
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:candle", map[string]string{"lit": "false", "candles": "2"}))
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 1); props["lit"] != "true" {
		t.Errorf("the candle stands %v, want it lit", props)
	}
	got = srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Damage != 2 {
		t.Errorf("the dispenser holds %+v, want the flint and steel worn twice", got)
	}
}

// TestADispenserPrimesTNTWithItsFlintAndSteel checks the flint and steel's
// dispense at a TNT block: the target becomes a primed body with the
// default burn, the block gone from its place, and the flint and steel
// worn but kept.
func TestADispenserPrimesTNTWithItsFlintAndSteel(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:tnt", nil))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	name, _ := servertest.StatePropsOf(t, srv, 4, 64, 1)
	if name != "minecraft:air" {
		t.Errorf("the target stands as %s, want the TNT gone from its place", name)
	}
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 {
		t.Fatalf("the dispense left %d primed bodies, want the one it lit", len(tnts))
	}
	if tnts[0].Fuse != 80 {
		t.Errorf("the body burns with %d fuse, want a full eighty", tnts[0].Fuse)
	}
	got := srv.InventoryAt(4, 64, 0)
	if servertest.NothingIn(got) || got[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the flint and steel back worn one", got)
	}
}

// TestADispenserReshapesTheNeighboursOfTheTNTItPrimes checks that priming
// TNT through the dispenser is a full block write: an oak fence beside the
// TNT reconnects, where a write skipping the neighbour shape update would
// leave its old connection.
func TestADispenserReshapesTheNeighboursOfTheTNTItPrimes(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:tnt", nil))
	// The fence east of the TNT connects west to it; a raw SetBlock leaves
	// the property as written.
	srv.SetBlock(5, 64, 1, servertest.MustState(t, "minecraft:oak_fence", map[string]string{"west": "true"}))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if _, props := servertest.StatePropsOf(t, srv, 5, 64, 1); props["west"] != "false" {
		t.Errorf("the fence beside the primed TNT stands %v, want its west connection gone with the block", props)
	}
}

// TestADispenserTipsItsArrowAtScale checks the potion a dispensed tipped
// arrow leaves in the body it hits: the effect at the tip's own amplifier,
// its duration scaled by the tipped arrow's POTION_DURATION_SCALE.
func TestADispenserTipsItsArrowAtScale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		potion    string
		ticks     int32
		amplifier int32
	}{
		{"minecraft:poison", 112, 0},
		{"minecraft:strong_poison", 54, 1},
	} {
		t.Run(tc.potion, func(t *testing.T) {
			srv, w := servertest.Dispensing(t)

			sheep := srv.SpawnMob("minecraft:sheep", 4.5, 64, 3.5)
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.TippedArrow(t, tc.potion)) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			w.DrainRecorded()
			srv.Dispense(4, 64, 0, protocol.FaceSouth)
			srv.TickEntities(10)

			duration, amplifier, ok := srv.MobEffectOf(sheep, "minecraft:poison")
			if !ok || duration != tc.ticks || amplifier != tc.amplifier {
				t.Errorf("the %s tipped arrow left poison for %d ticks at amplifier %d (present %v), want %d at %d",
					tc.potion, duration, amplifier, ok, tc.ticks, tc.amplifier)
			}
		})
	}
}

// TestADispenserTippedArrowScalesAShortCustomEffect checks a custom effect
// shorter than the scale's own denominator: withScaledDuration leaves a
// duration of zero or infinite alone and floors the rest to at least a tick,
// so a few-tick effect still lands as one and an infinite one stays infinite.
func TestADispenserTippedArrowScalesAShortCustomEffect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		duration int32
		want     int32
	}{
		{"four ticks keep one", 4, 1},
		{"seven ticks keep one", 7, 1},
		{"infinite stays infinite", -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, w := servertest.Dispensing(t)

			sheep := srv.SpawnMob("minecraft:sheep", 4.5, 64, 3.5)
			if !srv.SetInventoryAt(4, 64, 0, 0, tippedArrowWithEffect(t, "minecraft:poison", 0, tc.duration)) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			w.DrainRecorded()
			srv.Dispense(4, 64, 0, protocol.FaceSouth)
			srv.TickEntities(10)

			duration, amplifier, ok := srv.MobEffectOf(sheep, "minecraft:poison")
			if !ok || duration != tc.want || amplifier != 0 {
				t.Errorf("the duration-%d custom effect left poison for %d ticks at amplifier %d (present %v), want %d at 0",
					tc.duration, duration, amplifier, ok, tc.want)
			}
		})
	}
}

// TestADispenserSpectralArrowGlowsItsTarget checks that a spectral shaft's
// landed hit leaves the glowing SpectralArrow.doPostHurtEffects gives it.
func TestADispenserSpectralArrowGlowsItsTarget(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	sheep := srv.SpawnMob("minecraft:sheep", 4.5, 64, 3.5)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:spectral_arrow", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	srv.TickEntities(10)

	duration, amplifier, ok := srv.MobEffectOf(sheep, "minecraft:glowing")
	if !ok || duration != 200 || amplifier != 0 {
		t.Errorf("the spectral arrow left glowing for %d ticks at amplifier %d (present %v), want 200 at 0", duration, amplifier, ok)
	}
}

// TestADispenserLightsItsNetherPortalsFrameWithFlintAndSteel checks the
// flint and steel's dispense in the nether: the face opening into the cell
// a complete obsidian frame stands behind lights the portal in place of a
// fire, wearing the flint and steel — the one branch of the behaviour the
// overworld's ground cannot stand for.
func TestADispenserLightsItsNetherPortalsFrameWithFlintAndSteel(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.CarryIntoNether(t, srv, c, "Watcher", 8, 10)

	obsidian := rules.ObsidianState
	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:dispenser") {
		t.Fatal("the nether did not take the dispenser")
	}
	// A closed frame around the interior south of the dispenser, running
	// along X: the (8..9, 40..42, 9) cells, the floor and ceiling above and
	// below them, the pillars on both sides. The interior starts air, which
	// SetBlock cannot say by its own return.
	for _, cell := range [][3]int32{
		{8, 39, 9}, {9, 39, 9}, {8, 43, 9}, {9, 43, 9},
		{7, 40, 9}, {7, 41, 9}, {7, 42, 9},
		{10, 40, 9}, {10, 41, 9}, {10, 42, 9},
	} {
		if !srv.SetBlockInDimension(world.NetherName, cell[0], cell[1], cell[2], obsidian) {
			t.Fatalf("the nether did not take the frame block at %v", cell)
		}
	}
	for _, x := range []int32{8, 9} {
		for _, y := range []int32{40, 41, 42} {
			srv.SetBlockInDimension(world.NetherName, x, y, 9, 0)
		}
	}
	flint, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	if !srv.SetInventoryInDimension(world.NetherName, 8, 40, 8, 0,
		protocol.ItemStack{ItemID: flint, Count: 1}) {
		t.Fatal("the dispenser took no flint and steel")
	}
	c.Settle()

	srv.DispenseInDimension(world.NetherName, 8, 40, 8, protocol.FaceSouth)

	for _, x := range []int32{8, 9} {
		for _, y := range []int32{40, 41, 42} {
			if state := srv.BlockInDimension(world.NetherName, x, y, 9); state != rules.NetherPortalX {
				name, _, _ := registry.BlockStateName(state)
				t.Errorf("the interior at (%d, %d, 9) is %s, want the lit portal", x, y, name)
			}
		}
	}
	slots := srv.InventoryInDimension(world.NetherName, 8, 40, 8)
	if len(slots) == 0 || slots[0].ItemID != flint || slots[0].Count != 1 || slots[0].Damage != 1 {
		t.Errorf("the dispenser holds %+v, want the flint and steel back worn one", slots)
	}

	c.Disconnect()
}

// TestADispenserWashesOutTheBlockItFaces checks that a filled bucket
// dispensed at a non-solid block empties: the pour's canBeReplaced(Fluid)
// takes every non-solid block as well as the replaceable tag's, so a poppy
// and a torch - neither replaceable - are washed out where the placement
// rule would leave them, and the block's loot is dropped. Short grass,
// replaceable already, is dropped too; the seed makes its one-in-eight seeds
// land. Lava has no LiquidBlockContainer answer, so the rail it faces is
// washed out where the water case would waterlog it.
// vanilla: DispenserBlock's bucket behavior, BucketItem.emptyContents,
// BlockBehaviour.canBeReplaced(BlockState, Fluid)
func TestADispenserWashesOutTheBlockItFaces(t *testing.T) {
	t.Parallel()

	bucket := servertest.ItemID(t, "minecraft:bucket")

	cases := []struct{ bucket, block, pour, drop string }{
		{"minecraft:water_bucket", "minecraft:poppy", "minecraft:water", "minecraft:poppy"},
		{"minecraft:water_bucket", "minecraft:torch", "minecraft:water", "minecraft:torch"},
		{"minecraft:water_bucket", "minecraft:short_grass", "minecraft:water", "minecraft:wheat_seeds"},
		{"minecraft:lava_bucket", "minecraft:rail", "minecraft:lava", "minecraft:rail"},
	}
	for _, tc := range cases {
		srv, _ := servertest.Dispensing(t)
		srv.LootSeed(9)

		state, ok := registry.FindState(tc.block, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", tc.block)
		}
		if !srv.SetBlock(4, 64, 1, state) {
			t.Fatalf("the world did not take the %s", tc.block)
		}
		if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, tc.bucket, 1)) {
			t.Fatal("the dispenser has no block entity to fill")
		}

		srv.Dispense(4, 64, 0, protocol.FaceSouth)

		if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != tc.pour || props["level"] != "0" {
			t.Errorf("the %s pour at the %s left %s %v, want a source", tc.bucket, tc.block, name, props)
		}
		if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != bucket {
			t.Errorf("the dispenser's slot after the %s = %+v, want the empty bucket it became", tc.block, got)
		}
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 || items[0].Item != tc.drop || items[0].Count != 1 {
			t.Errorf("the washed-out %s dropped %+v, want one %s", tc.block, items, tc.drop)
		}
	}
}

// TestADispenserWaterlogsTheRailItFaces checks that water at a waterloggable
// block fills the block's own cell rather than washing it out: a rail is a
// SimpleWaterloggedBlock, so the dispensed source leaves a waterlogged rail,
// drops nothing and still empties the bucket, the LiquidBlockContainer branch
// running before the destroy. vanilla: DispenserBlock's bucket behavior,
// BucketItem.emptyContents, SimpleWaterloggedBlock.canPlaceLiquid
func TestADispenserWaterlogsTheRailItFaces(t *testing.T) {
	t.Parallel()

	srv, _ := servertest.Dispensing(t)

	rail, ok := registry.FindState("minecraft:rail", nil)
	if !ok {
		t.Fatal("the block registry has no rail")
	}
	waterlogged, ok := registry.FindState("minecraft:rail", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged rail")
	}
	if !srv.SetBlock(4, 64, 1, rail) {
		t.Fatal("the world did not take the rail")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}

	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if state := srv.BlockAt(4, 64, 1); state != waterlogged {
		t.Errorf("the rail after the pour = %d, want the waterlogged state %d", state, waterlogged)
	}
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != servertest.ItemID(t, "minecraft:bucket") {
		t.Errorf("the dispenser's slot after the rail = %+v, want the empty bucket it became", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the waterlogged rail dropped %+v, want nothing washed out", items)
	}
}

// TestADispenserEvaporatesAtThePlantItFacesInTheNether checks that the
// nether's hiss wins over the wash-out and over the waterlog: a water bucket
// dispensed at a torch or a rail empties with the block kept dry, vanilla's
// ultraWarm branch returning before it destroys the target or fills its own
// cell. vanilla: BucketItem.emptyContents
func TestADispenserEvaporatesAtThePlantItFacesInTheNether(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.CarryIntoNether(t, srv, c, "Watcher", 8, 10)

	bucket := servertest.ItemID(t, "minecraft:bucket")
	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:dispenser") {
		t.Fatal("the nether did not take the dispenser")
	}
	for _, name := range []string{"minecraft:torch", "minecraft:rail"} {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		if !srv.SetBlockInDimension(world.NetherName, 8, 40, 9, state) {
			t.Fatalf("the nether did not take the %s", name)
		}
		if !srv.SetInventoryInDimension(world.NetherName, 8, 40, 8, 0,
			protocol.ItemStack{ItemID: servertest.ItemID(t, "minecraft:water_bucket"), Count: 1}) {
			t.Fatal("the dispenser took no water bucket")
		}
		c.Settle()

		srv.DispenseInDimension(world.NetherName, 8, 40, 8, protocol.FaceSouth)

		if got := srv.BlockInDimension(world.NetherName, 8, 40, 9); got != state {
			t.Errorf("the %s the dispenser faces = %d, want it kept at %d", name, got, state)
		}
		slots := srv.InventoryInDimension(world.NetherName, 8, 40, 8)
		if len(slots) == 0 || slots[0].ItemID != bucket || slots[0].Count != 1 {
			t.Errorf("the dispenser's slot = %+v, want the empty bucket it became", slots)
		}
		if items := srv.ItemsInDimension(world.NetherName); len(items) != 0 {
			t.Errorf("the nether pour dropped %+v, want nothing washed out", items)
		}
	}
	c.Disconnect()
}

// TestADispenserChargesARespawnAnchor checks the glowstone's dispense: a
// respawn anchor the face opens onto takes one charge and spends the
// glowstone, a full one fails and keeps it, and any other block gets the
// ordinary drop.
// vanilla: DispenseItemBehavior (glowstone), RespawnAnchorBlock.charge
func TestADispenserChargesARespawnAnchor(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SetBlock(4, 64, 1, servertest.AnchorStateOf(1))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:glowstone", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	name, category, sx, sy, sz, _, _ := w.ExpectContainerSound()
	if name != "minecraft:block.respawn_anchor.charge" || category != protocol.SoundCategoryBlock {
		t.Errorf("the charge sounded as %q in category %d, want the anchor's in the blocks category", name, category)
	}
	if sx != 4.5 || sy != 64.5 || sz != 1.5 {
		t.Errorf("the charge sounded at (%v, %v, %v), want the anchor's cell (4.5, 64.5, 1.5)", sx, sy, sz)
	}
	servertest.ExpectAnchorCharges(t, srv, "", 4, 64, 1, 2)
	if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
		t.Errorf("the dispenser holds %+v, want the glowstone spent", got)
	}

	// A full anchor fails: the glowstone stays and the fail click sounds.
	w.DrainRecorded()
	srv.SetBlock(4, 64, 1, servertest.AnchorStateOf(4))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:glowstone", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	if !slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("the full anchor's charge played no fail click; events were %v", w.Events)
	}
	servertest.ExpectAnchorCharges(t, srv, "", 4, 64, 1, 4)
	if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want the glowstone kept", got)
	}

	// Any other block gets the ordinary drop.
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:stone", nil))
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	var dropped int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:glowstone" {
			dropped += int(it.Count)
		}
	}
	if dropped != 1 {
		t.Errorf("the glowstone dropped onto stone = %d, want the one dispensed", dropped)
	}
}

// bottleDispense dispenses one glass bottle south into the cell (4, 64, 1)
// as set up by place, and returns what the dispenser holds after.
func bottleDispense(t *testing.T, place func(srv *server.Server)) (*server.Server, *servertest.WindowedClient, []protocol.ItemStack) {
	t.Helper()
	srv, w := servertest.Dispensing(t)
	place(srv)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:glass_bottle", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	return srv, w, srv.InventoryAt(4, 64, 0)
}

func TestADispenserFillsABottleFromWater(t *testing.T) {
	t.Parallel()
	slab, ok := registry.FindState("minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged oak slab")
	}
	cases := []struct {
		name  string
		state uint16
	}{
		{"a water source", servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"})},
		{"flowing water", servertest.MustState(t, "minecraft:water", map[string]string{"level": "3"})},
		{"a waterlogged block", slab},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w, got := bottleDispense(t, func(srv *server.Server) { srv.SetBlock(4, 64, 1, tc.state) })
			if servertest.NothingIn(got) || got[0].ItemID != servertest.MustItemID(t, "minecraft:potion") || got[0].Count != 1 {
				t.Fatalf("the dispenser holds %+v, want one water bottle", got)
			}
			contents, ok := got[0].PotionContentsOf()
			if !ok || contents.Potion != registry.MustPotion("minecraft:water") {
				t.Errorf("the bottle holds %+v, want the water potion", contents)
			}
			if !slices.Contains(w.Events, protocol.WorldEventDispense) || slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
				t.Errorf("events were %v, want the success click alone", w.Events)
			}
			if len(srv.ItemsInDimension(world.DimensionName)) != 0 {
				t.Errorf("the fill dropped an item, want none")
			}
		})
	}
}

func TestADispenserFillsABottleFromAFullHive(t *testing.T) {
	t.Parallel()
	srv, _, got := bottleDispense(t, func(srv *server.Server) {
		placeBeeHive(t, srv, "south")
	})
	if bees := srv.MobIDsOfKind("minecraft:bee"); len(bees) != 1 {
		t.Errorf("%d bees out after the fill, want the stored one", len(bees))
	}
	if servertest.NothingIn(got) || got[0].ItemID != servertest.MustItemID(t, "minecraft:honey_bottle") || got[0].Count != 1 {
		t.Errorf("the dispenser holds %+v, want one honey bottle", got)
	}
	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:beehive" || props["honey_level"] != "0" {
		t.Errorf("the hive stands as %s %v, want its honey level reset", name, props)
	}
}

func TestADispenserDropsABottleItCannotFill(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		state uint16
	}{
		{"air", servertest.MustState(t, "minecraft:air", nil)},
		{"a hive short of honey", servertest.MustState(t, "minecraft:beehive", map[string]string{"honey_level": "4"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w, got := bottleDispense(t, func(srv *server.Server) { srv.SetBlock(4, 64, 1, tc.state) })
			if !servertest.NothingIn(got) {
				t.Errorf("the dispenser holds %+v, want the bottle thrown out", got)
			}
			var dropped int
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				if it.Item == "minecraft:glass_bottle" {
					dropped++
				}
			}
			if dropped != 1 {
				t.Errorf("%d bottles dropped, want 1", dropped)
			}
			if !slices.Contains(w.Events, protocol.WorldEventDispenseFail) || slices.Contains(w.Events, protocol.WorldEventDispense) {
				t.Errorf("events were %v, want the fail click alone", w.Events)
			}
		})
	}
}

// TestADispenserStandsItsSpawnEggsMob checks a spawn egg fired from a
// dispenser: the mob stands in the cell the face opens onto, dropped onto
// what the cell holds, the egg is spent and nothing is dropped.
// vanilla: SpawnEggItemBehavior.execute
func TestADispenserStandsItsSpawnEggsMob(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		below bool
		wantY float64
	}{
		{"onto the ground", false, 64},
		{"onto a bottom slab", true, 64.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			if tc.below {
				srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:stone_slab", map[string]string{"type": "bottom", "waterlogged": "false"}))
			}
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:chicken_spawn_egg", 2)) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			srv.Dispense(4, 64, 0, protocol.FaceSouth)

			var chickens []server.TestMob
			for _, m := range srv.Mobs() {
				if m.Kind == "minecraft:chicken" {
					chickens = append(chickens, m)
				}
			}
			if len(chickens) != 1 {
				t.Fatalf("the egg stood up %d chickens, want one", len(chickens))
			}
			if c := chickens[0]; c.X != 4.5 || c.Y != tc.wantY || c.Z != 1.5 {
				t.Errorf("the chicken stands at (%v, %v, %v), want (4.5, %v, 1.5)", c.X, c.Y, c.Z, tc.wantY)
			}
			if left := srv.InventoryAt(4, 64, 0)[0]; left.Count != 1 {
				t.Errorf("the slot holds %d eggs, want one", left.Count)
			}
			if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
				t.Errorf("the egg left %d items on the ground, want none", len(items))
			}
			w.DrainRecorded()
			if !slices.Contains(w.Events, protocol.WorldEventDispense) {
				t.Errorf("no dispense click; events were %v", w.Events)
			}
		})
	}
}

// TestADispenserKeepsAMonsterEggInPeaceful checks that a monster egg spawns
// nothing in peaceful and stays in the slot, with the click and smoke played.
// vanilla: SpawnEggItemBehavior.execute, EntityType.create
func TestADispenserKeepsAMonsterEggInPeaceful(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)
	srv.SetDifficulty(0) // peaceful
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:zombie_spawn_egg", 2)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()

	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:zombie" {
			t.Fatalf("a zombie stands at (%v, %v, %v), want none in peaceful", m.X, m.Y, m.Z)
		}
	}
	if left := srv.InventoryAt(4, 64, 0)[0]; left.Count != 2 {
		t.Errorf("the slot holds %d eggs, want both kept", left.Count)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("%d items on the ground, want none", len(items))
	}
	if !slices.Contains(w.Events, protocol.WorldEventDispense) || slices.Contains(w.Events, protocol.WorldEventDispenseFail) {
		t.Errorf("events were %v, want the success click", w.Events)
	}
}

// TestADispenserFloatsItsBoat checks that a boat goes onto the water in front
// of the dispenser, or onto the air above water, and drops as an item
// anywhere else.
// vanilla: BoatDispenseItemBehavior.execute
func TestADispenserFloatsItsBoat(t *testing.T) {
	t.Parallel()
	water := func(t *testing.T) uint16 {
		return servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"})
	}
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, srv *server.Server)
		boats   int
		wantY   float64
		dropped int
	}{
		{"onto water in front", func(t *testing.T, srv *server.Server) { srv.SetBlock(4, 64, 1, water(t)) }, 1, 65.5, 0},
		{"onto air above water", func(t *testing.T, srv *server.Server) { srv.SetBlock(4, 63, 1, water(t)) }, 1, 64.5, 0},
		{"not onto dry ground", func(*testing.T, *server.Server) {}, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			tc.prepare(t, srv)
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:oak_boat", 2)) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			srv.Dispense(4, 64, 0, protocol.FaceSouth)

			if got := srv.BoatCount(); got != tc.boats {
				t.Fatalf("%d boats stand, want %d", got, tc.boats)
			}
			if tc.boats == 1 {
				x, y, z, yaw, _, ok := srv.BoatState(srv.BoatIDs()[0])
				if !ok || x != 4.5 || y != tc.wantY || z != 1.75 || yaw != 0 {
					t.Errorf("the boat floats at (%v, %v, %v) facing %v, want (4.5, %v, 1.75) facing 0", x, y, z, yaw, tc.wantY)
				}
			}
			if left := srv.InventoryAt(4, 64, 0)[0]; left.Count != 1 {
				t.Errorf("the slot holds %d boats, want one", left.Count)
			}
			if items := srv.ItemsInDimension(world.DimensionName); len(items) != tc.dropped {
				t.Errorf("%d items on the ground, want %d", len(items), tc.dropped)
			}
			w.DrainRecorded()
			if !slices.Contains(w.Events, protocol.WorldEventDispense) {
				t.Errorf("no dispense click; events were %v", w.Events)
			}
		})
	}
}

// TestADispenserFloatsANamedBoatUnderItsName: a dispensed boat takes the
// stack's custom name, hidden.
// vanilla: BoatDispenseItemBehavior.execute, Entity.applyImplicitComponents
func TestADispenserFloatsANamedBoatUnderItsName(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	stack := servertest.ItemStack(t, "minecraft:oak_chest_boat", 1)
	stack.CustomName = &protocol.Text{Text: "Ferry"}
	if !srv.SetInventoryAt(4, 64, 0, 0, stack) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)

	if got := srv.BoatCount(); got != 1 {
		t.Fatalf("%d boats stand, want 1", got)
	}
	tag, ok := srv.BoatSaveTag(srv.BoatIDs()[0])
	if !ok {
		t.Fatal("the boat has no save tag")
	}
	if got := tag["CustomName"]; got != nbt.String("Ferry") {
		t.Errorf("CustomName = %#v, want \"Ferry\"", got)
	}
	if got, ok := tag["CustomNameVisible"]; ok {
		t.Errorf("CustomNameVisible = %#v, want absent", got)
	}
}

func TestADispenserPlacesItsMinecart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item, shape string
		below, blocked    bool
		face              int32
		lift              float64
	}{
		{name: "flat", item: "minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
		{name: "ascending", item: "minecart", shape: "ascending_south", face: protocol.FaceSouth, lift: 0.6},
		{name: "below", item: "minecart", shape: "north_south", below: true, face: protocol.FaceSouth, lift: -0.9},
		{name: "ascending_below", item: "minecart", shape: "ascending_south", below: true, face: protocol.FaceSouth, lift: -0.4},
		{name: "down", item: "minecart", shape: "ascending_south", below: true, face: protocol.FaceBottom, lift: -0.9},
		{name: "chest", item: "chest_minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
		{name: "furnace", item: "furnace_minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
		{name: "tnt", item: "tnt_minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
		{name: "hopper", item: "hopper_minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
		{name: "no_rail", item: "minecart", face: protocol.FaceSouth},
		{name: "blocked", item: "minecart", below: true, blocked: true, shape: "north_south", face: protocol.FaceSouth},
		{name: "command_block", item: "command_block_minecart", shape: "north_south", face: protocol.FaceSouth, lift: 0.1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			step := protocol.FaceOffset[tc.face]
			tx, ty, tz := int32(4)+step[0], int32(64)+step[1], step[2]
			srv.SetBlock(tx, ty, tz, servertest.MustState(t, "minecraft:air", nil))
			// The flat fixture has ground directly below the target.
			if tc.below {
				srv.SetBlock(tx, ty-1, tz, servertest.MustState(t, "minecraft:stone", nil))
			}
			if tc.shape != "" {
				ry := ty
				if tc.below {
					ry--
				}
				srv.SetBlock(tx, ry, tz, servertest.MustState(t, "minecraft:rail", map[string]string{"shape": tc.shape}))
			}
			if tc.blocked {
				srv.SetBlock(tx, ty, tz, servertest.MustState(t, "minecraft:stone", nil))
			}
			stack := servertest.ItemStack(t, "minecraft:"+tc.item, 1)
			stack.CustomName = &protocol.Text{Text: "Rail rider"}
			if !srv.SetInventoryAt(4, 64, 0, 0, stack) {
				t.Fatal("missing dispenser")
			}
			srv.Dispense(4, 64, 0, tc.face)
			if tc.shape == "" || tc.blocked {
				items := srv.ItemsInDimension(world.DimensionName)
				if len(items) != 1 || items[0].Item != "minecraft:"+tc.item {
					t.Fatalf("dropped items = %+v", items)
				}
				if carts := srv.MinecartsInDimension(world.DimensionName); len(carts) != 0 {
					t.Fatalf("unexpected carts: %+v", carts)
				}
			} else {
				typ, ok := registry.EntityTypeID("minecraft:" + tc.item)
				if !ok {
					t.Fatal("missing cart entity type")
				}
				seen := w.ExpectSpawned(typ)
				x, y, z := 4.5+float64(step[0])*1.125, 64+float64(step[1])+tc.lift, 0.5+float64(step[2])*1.125
				if seen.X != x || seen.Y != y || seen.Z != z {
					t.Fatalf("cart at (%g,%g,%g), want (%g,%g,%g)", seen.X, seen.Y, seen.Z, x, y, z)
				}
				r := protocol.NewReader(w.ExpectPlay(protocol.IDPlayEntityMetadata))
				if r.ReadVarInt() != seen.EntityID || r.ReadU8() != protocol.MetadataIndexCustomName || r.ReadVarInt() != protocol.MetadataOptionalText {
					t.Fatal("missing cart name metadata")
				}
				name, r := servertest.ReadOptionalText(t, r)
				if name != nbt.String("Rail rider") || r.ReadU8() != protocol.MetadataEnd || r.Err() != nil {
					t.Fatalf("cart name = %q, error = %v", name, r.Err())
				}
				if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
					t.Fatalf("unexpected dropped items: %+v", items)
				}
			}
			if got := srv.InventoryAt(4, 64, 0); !servertest.NothingIn(got) {
				t.Fatalf("unspent slot: %+v", got)
			}
			w.DrainRecorded()
			if !slices.Contains(w.Events, protocol.WorldEventDispense) || !slices.Contains(w.Events, 2000) {
				t.Fatalf("dispense events = %v", w.Events)
			}
		})
	}
}

func TestADispenserLaunchesItsRocketAndWindCharge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item                    string
		typ, event, face        int32
		distance, power, spread float64
	}{
		{"minecraft:firework_rocket", protocol.EntityTypeFireworkRocket, 1004, protocol.FaceTop, 0.5000099999997474, 0.5, 0.018},
		{"minecraft:firework_rocket", protocol.EntityTypeFireworkRocket, 1004, protocol.FaceSouth, 0.5000099999997474, 0.5, 0.018},
		{"minecraft:wind_charge", protocol.EntityTypeWindCharge, 1051, protocol.FaceSouth, 1, 1, 0.11485},
		{"minecraft:wind_charge", protocol.EntityTypeWindCharge, 1051, protocol.FaceBottom, 1, 1, 0.11485},
	} {
		t.Run(tc.item+"/"+fmt.Sprint(tc.face), func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.Dispensing(t)
			if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, tc.item, 2)) {
				t.Fatal("missing dispenser")
			}
			srv.Dispense(4, 64, 0, tc.face)
			seen := w.ExpectSpawned(tc.typ)
			step := protocol.FaceOffset[tc.face]
			for i, got := range []float64{seen.X, seen.Y, seen.Z} {
				want := []float64{4.5, 64.5, 0.5}[i] + float64(step[i])*tc.distance
				if math.Abs(got-want) > 1e-9 {
					t.Errorf("position axis %d = %g, want %g", i, got, want)
				}
			}
			for i, got := range []float64{seen.Vx, seen.Vy, seen.Vz} {
				want := float64(step[i]) * tc.power
				if math.Abs(got-want) > tc.spread+0.001 {
					t.Errorf("velocity axis %d = %g, want %g within %g", i, got, want, tc.spread)
				}
			}
			if tc.item == "minecraft:firework_rocket" {
				// Retain the shot direction rather than accelerating like a hand-launched rocket.
				r := protocol.NewReader(w.ExpectPlay(protocol.IDPlayEntityMetadata))
				if r.ReadVarInt() != seen.EntityID {
					t.Fatal("rocket metadata belongs to another entity")
				}
				shot := false
				for r.Err() == nil {
					index := r.ReadU8()
					if index == protocol.MetadataEnd {
						break
					}
					typ := r.ReadVarInt()
					if index == protocol.MetadataIndexFireworkShotAtAngle {
						shot = r.ReadBool()
					} else {
						servertest.StepOverMetadataValue(r, typ)
					}
				}
				if !shot || r.Err() != nil {
					t.Fatalf("rocket shot-at-angle = %v, error = %v", shot, r.Err())
				}
			}
			if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
				t.Fatalf("unexpected dropped items: %+v", items)
			}
			if got := srv.InventoryAt(4, 64, 0); got[0].Count != 1 {
				t.Fatalf("remaining stack = %+v", got[0])
			}
			w.DrainRecorded()
			if !slices.Contains(w.Events, tc.event) || !slices.Contains(w.Events, 2000) || slices.Contains(w.Events, protocol.WorldEventDispense) {
				t.Fatalf("launch events = %v", w.Events)
			}
		})
	}
}

// firedItemAfterFirstTick powers the container at (4, 64, 0) with a redstone
// block, steps until its item exists and one tick more, and returns the item's
// feet and its horizontal offset from the block's centre.
func firedItemAfterFirstTick(t *testing.T, srv *server.Server) (feetY, offset float64) {
	t.Helper()
	srv.SetBlockAndTell(4, 64, -1, servertest.MustState(t, "minecraft:redstone_block", nil))
	var items []server.TestItem
	servertest.UntilTicks(t, srv, 12, "the powered container spawns its item", func() bool {
		items = srv.ItemsInDimension(world.DimensionName)
		return len(items) > 0
	})
	servertest.RunTicks(t, srv, 1)
	items = srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("%d items after the first tick, want 1", len(items))
	}
	it := items[0]
	return it.Y - 0.125, math.Hypot(it.X-4.5, it.Z-0.5)
}

// TestADropperFiringUpStartsTheItemAboveItsTop checks that an item a dropper
// facing up fires starts at vanilla's feet Y, above the block, so it is not
// pushed out of the block's box on its first tick.
// vanilla: DefaultDispenseItemBehavior.spawnItem
func TestADropperFiringUpStartsTheItemAboveItsTop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Collector")
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") {
		t.Fatal("could not place the dropper")
	}
	srv.SetBlock(4, 64, 0, servertest.MustState(t, "minecraft:dropper", map[string]string{"facing": "up"}))
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:stone", 1)) {
		t.Fatal("could not fill the dropper")
	}
	feetY, offset := firedItemAfterFirstTick(t, srv)
	if feetY < 65.15 {
		t.Errorf("feet y = %v after the first tick, want above 65.15", feetY)
	}
	if offset >= 0.11 {
		t.Errorf("horizontal offset %v from the dropper's centre, want under 0.11", offset)
	}
}

// TestACrafterFiringUpStartsTheItemAboveItsTop is the dropper's check for the
// result a crafter ejects through its up face.
// vanilla: CrafterBlock.dispenseItem
func TestACrafterFiringUpStartsTheItemAboveItsTop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Collector")
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:crafter") {
		t.Fatal("could not place the crafter")
	}
	srv.SetBlock(4, 64, 0, servertest.MustState(t, "minecraft:crafter", map[string]string{"orientation": "up_north"}))
	if !srv.SetInventoryAt(4, 64, 0, 4, servertest.ItemStack(t, "minecraft:oak_log", 3)) {
		t.Fatal("could not fill the crafter")
	}
	feetY, offset := firedItemAfterFirstTick(t, srv)
	if feetY < 65.15 {
		t.Errorf("feet y = %v after the first tick, want above 65.15", feetY)
	}
	if offset >= 0.11 {
		t.Errorf("horizontal offset %v from the crafter's centre, want under 0.11", offset)
	}
}
