package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The dried ghast, black-box: the hydration staircase a waterlogged block
// climbs on the 5000-tick visits its random ticks book, the ghastling the
// top of the staircase hatches, the drying a block the water left walks
// back down, the placement's facing and waterlogging, and the flood's
// waterlogging of a block it reaches. The dice are pinned the way the farm
// tests pin them, the ticket rule turned up so a handful of passes covers
// the section, and the wait per step is the vanilla delay itself: the
// random tick a hydrated block lands books the block's visit
// HYDRATION_TICK_DELAY out, and the visit is what steps it.
// vanilla: DriedGhastBlock.randomTick

// driedGhastSeed is the seed the dried ghast tests pin the dice with.
const driedGhastSeed = int64(23)

const (
	// driedGhastTickets is the random tick rule the tests run at: 4096
	// dice a randomly-ticking section a pass, so a pass all but covers the
	// section the dried ghast stands in and one of them books its visit.
	driedGhastTickets = 4096

	// driedGhastPasses is the random passes each hop runs; driedGhastHops
	// bounds the waits a stage is given. Twelve passes throw far more dice
	// than a section holds twice over, so a stage that needed all ten of
	// its hops is one the dice left standing far longer than they ever
	// should.
	driedGhastPasses = 12
	driedGhastHops   = 10

	// driedGhastDelay is the ticks between the random tick's booking and
	// the visit it books, vanilla's HYDRATION_TICK_DELAY, grown by the
	// passes of the hop itself so the visit the last pass books comes due
	// within the hop too.
	driedGhastDelay = 5000
)

// driedGhastHop drives the block's rhythm once: the random passes that are
// to land on the block and book its visit, then the physics ticks that see
// the visit due.
func driedGhastHop(srv *server.Server) {
	srv.TickRandomTicks(driedGhastPasses)
	srv.TickPhysics(driedGhastDelay + driedGhastPasses)
}

// driedGhastState reads the properties of the dried ghast at the cell,
// failing the test when the block is not one.
func driedGhastState(t *testing.T, srv *server.Server, x, y, z int32) map[string]string {
	t.Helper()
	name, props, ok := registry.BlockStateName(srv.BlockAt(x, y, z))
	if !ok || name != "minecraft:dried_ghast" {
		t.Fatalf("the block at %d, %d, %d is not a dried ghast", x, y, z)
	}
	return registry.ParseStateProps(props)
}

// awaitDriedGhastHydration waits the block's visits out: one hop of the
// rhythm at a time until the hydration reaches the wanted stage — up a
// waterlogged staircase or down a drying one — and fails the test if the
// visits never carried it there.
func awaitDriedGhastHydration(t *testing.T, srv *server.Server, x, y, z int32, want int) map[string]string {
	t.Helper()
	for range driedGhastHops {
		driedGhastHop(srv)
		props := driedGhastState(t, srv, x, y, z)
		if hydration, err := strconv.Atoi(props["hydration"]); err == nil && hydration == want {
			return props
		}
	}
	t.Fatalf("the dried ghast never reached hydration %d in %d hops", want, driedGhastHops)
	return nil
}

// TestADriedGhastHydratesAndHatchesAGhastling checks the whole staircase: a
// waterlogged dried ghast climbs one hydration step per booked visit, each
// step telling the transition sound, and the visit that finds it at the
// top spends it — the cell is left the water the block held, and a baby
// happy ghast stands in it, faced the way the block was, its arrival told
// with the ghastling's own sound.
// vanilla: DriedGhastBlock.tick, DriedGhastBlock.spawnGhastling
func TestADriedGhastHydratesAndHatchesAGhastling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dried_ghast",
		map[string]string{"facing": "north", "hydration": "0", "waterlogged": "true"})

	for want := 1; want <= 3; want++ {
		props := awaitDriedGhastHydration(t, srv, 5, 64, 0, want)
		if props["waterlogged"] != "true" || props["facing"] != "north" {
			t.Errorf("the hydrated block = %v, want the north-facing waterlogged one it climbed from", props)
		}
		if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.transition"); pitch != 1.0 {
			t.Errorf("the hydration step played at pitch %v, want vanilla's 1.0", pitch)
		}
	}

	// The fourth visit finds the block at the top and spends it.
	water := servertest.CropsStateOf(t, "minecraft:water", map[string]string{"level": "0"})
	for range driedGhastHops {
		if got := srv.BlockAt(5, 64, 0); got == water {
			break
		}
		driedGhastHop(srv)
	}
	if got := srv.BlockAt(5, 64, 0); got != water {
		t.Fatalf("the fully hydrated block never spent itself in %d hops", driedGhastHops)
	}
	ghastling := c.ExpectMobSpawn(protocol.EntityTypeHappyGhast)
	if !ghastling.HasBaby {
		t.Error("the hatched happy ghast is not a ghastling (its baby flag is unset)")
	}
	if ghastling.X != 5.5 || ghastling.Y != 64 || ghastling.Z != 0.5 {
		t.Errorf("the ghastling arrived at (%v, %v, %v), want the spent cell's bottom centre (5.5, 64, 0.5)",
			ghastling.X, ghastling.Y, ghastling.Z)
	}
	// The arrival is an entity sound, so the client follows it with the
	// ghastling rather than pinning it to the spent cell.
	id, pitch := awaitEntitySound(t, c, "minecraft:entity.ghastling.spawn")
	if id != ghastling.EntityID {
		t.Errorf("the arrival sound names entity %d, want the ghastling %d", id, ghastling.EntityID)
	}
	if pitch != 1.0 {
		t.Errorf("the ghastling's arrival played at pitch %v, want vanilla's 1.0", pitch)
	}
	mobs := srv.Mobs()
	if len(mobs) != 1 {
		t.Fatalf("mobs in the world = %d, want the one ghastling", len(mobs))
	}
	m := mobs[0]
	if m.Kind != "minecraft:happy_ghast" || !m.Baby || m.Age != entity.BabyAgeStart {
		t.Errorf("the hatch = kind %s, baby %t, age %d, want a newborn happy ghast", m.Kind, m.Baby, m.Age)
	}
	if m.X != 5.5 || m.Y != 64 || m.Z != 0.5 {
		t.Errorf("the ghastling stands at (%v, %v, %v), want (5.5, 64, 0.5)", m.X, m.Y, m.Z)
	}
	if m.Yaw != 180 || m.HeadYaw != 180 {
		t.Errorf("the ghastling faces yaw %v, head %v, want the north the block carried (180)", m.Yaw, m.HeadYaw)
	}
}

// TestADriedGhastDriesWithoutWater checks the way down: the visit a partly
// hydrated block books dries it one step in silence, its facing kept, and a
// dry block at the bottom of the staircase books nothing — the vanilla
// random tick's own gate, waterlogged or hydrated only.
// vanilla: DriedGhastBlock.tick, DriedGhastBlock.randomTick
func TestADriedGhastDriesWithoutWater(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dried_ghast",
		map[string]string{"facing": "east", "hydration": "3", "waterlogged": "false"})
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:dried_ghast",
		map[string]string{"facing": "west", "hydration": "0", "waterlogged": "false"})

	props := awaitDriedGhastHydration(t, srv, 5, 64, 0, 2)
	if props["waterlogged"] != "false" || props["facing"] != "east" {
		t.Errorf("the dried block = %v, want the east-facing dry one it dried from", props)
	}
	// The same staircase walked on down through hydration one to the
	// bottom, where only the dry unhydrated block is left.
	for _, want := range []int{1, 0} {
		props := awaitDriedGhastHydration(t, srv, 5, 64, 0, want)
		if props["waterlogged"] != "false" || props["facing"] != "east" {
			t.Errorf("the block dried to hydration %d = %v, want the east-facing dry one", want, props)
		}
	}
	// The dry unhydrated block beside it books nothing and stands as placed.
	bottom := driedGhastState(t, srv, 6, 64, 0)
	if bottom["hydration"] != "0" || bottom["waterlogged"] != "false" || bottom["facing"] != "west" {
		t.Errorf("the dry unhydrated block = %v, want the west-facing newborn left standing", bottom)
	}
}

// TestPlacingADriedGhastFacesItsPlacer checks the placement: the block faces
// back across the placer's look, the water in the cell it is placed into
// waterlogs it, and the setPlacedBy sound tells which of the two placements
// it was — into water or onto dry land — at the plain pitch vanilla's own
// two sounds play at.
// vanilla: DriedGhastBlock.getStateForPlacement, DriedGhastBlock.setPlacedBy
func TestPlacingADriedGhastFacesItsPlacer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper")

	servertest.GiveFarmItem(t, c, "minecraft:dried_ghast")
	water := servertest.CropsStateOf(t, "minecraft:water", map[string]string{"level": "0"})
	if !srv.SetBlock(9, 64, 0, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	// Looking due south: the block faces back at the placer, north.
	c.FacePlayer(8.5, 64, 0.5, 0, 0)
	c.PlaceBlock(9, 63, 0, protocol.FaceTop)
	if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.place_in_water"); pitch != 1.0 {
		t.Errorf("the placement into water played at pitch %v, want vanilla's 1.0", pitch)
	}
	wet := driedGhastState(t, srv, 9, 64, 0)
	if wet["facing"] != "north" || wet["waterlogged"] != "true" || wet["hydration"] != "0" {
		t.Errorf("the block placed into the water = %v, want a north-facing waterlogged newborn", wet)
	}

	// The dry cell one block further south: the same look, the same north.
	c.FacePlayer(11.5, 64, 0.5, 0, 0)
	c.PlaceBlock(12, 63, 0, protocol.FaceTop)
	if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.place"); pitch != 1.0 {
		t.Errorf("the dry placement played at pitch %v, want vanilla's 1.0", pitch)
	}
	dry := driedGhastState(t, srv, 12, 64, 0)
	if dry["facing"] != "north" || dry["waterlogged"] != "false" {
		t.Errorf("the block placed on dry land = %v, want the dry north-facing one", dry)
	}
}

// TestAPourOntoADriedGhastSoundsItsWaterlogging checks the bucket's half of
// the arrival: the poured water a dried ghast's own cell takes waterlogs it
// — the one waterlogged block whose placeLiquid is not silent telling its
// own sound beside the bucket's empty.
// vanilla: BucketItem.emptyContents, DriedGhastBlock.placeLiquid
func TestAPourOntoADriedGhastSoundsItsWaterlogging(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SetFarmBlock(t, c, srv, 9, 64, 0, "minecraft:dried_ghast",
		map[string]string{"facing": "east", "hydration": "0", "waterlogged": "false"})
	servertest.GiveFarmItem(t, c, "minecraft:water_bucket")

	// Hovering over the block, the look straight down pours into it.
	c.FacePlayer(9.5, 65.5, 0.5, 0, 90)
	servertest.UseHeld(c, 0, 90)
	if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.place_in_water"); pitch != 1.0 {
		t.Errorf("the poured waterlogging played at pitch %v, want vanilla's 1.0", pitch)
	}
	// The sound goes out ahead of the write it tells; the settle waits the
	// pour out.
	c.Settle()
	props := driedGhastState(t, srv, 9, 64, 0)
	if props["waterlogged"] != "true" || props["facing"] != "east" {
		t.Errorf("the poured block = %v, want the east-facing wet one placeLiquid leaves", props)
	}
}

// TestWaterFlowingOntoADriedGhastWaterlogsIt checks the flood's half of the
// waterlogging: the source a flood hands a dry dried ghast waterlogs it —
// the vanilla placeLiquid's flag, and the in-water sound — and the
// waterlogged block the flood made hydrates on the visits like one placed
// wet.
// vanilla: DriedGhastBlock.placeLiquid
func TestWaterFlowingOntoADriedGhastWaterlogsIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dried_ghast",
		map[string]string{"facing": "east", "hydration": "0", "waterlogged": "false"})
	// A source on either side: the flood they hem the block in with
	// computes a source at the block's own cell, the one its waterlogging
	// takes.
	servertest.SetFarmBlock(t, c, srv, 4, 64, 0, "minecraft:water", map[string]string{"level": "0"})
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:water", map[string]string{"level": "0"})
	srv.TickPhysics(40)

	if props := driedGhastState(t, srv, 5, 64, 0); props["waterlogged"] != "true" {
		t.Errorf("the block the flood reached = %v, want the waterlogged one placeLiquid leaves", props)
	}
	if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.place_in_water"); pitch != 1.0 {
		t.Errorf("the flood's waterlogging played at pitch %v, want vanilla's 1.0", pitch)
	}

	props := awaitDriedGhastHydration(t, srv, 5, 64, 0, 1)
	if props["facing"] != "east" {
		t.Errorf("the waterlogged block = %v, want the east-facing one the flood reached", props)
	}
	if pitch := servertest.AwaitSoundPitch(t, c, "minecraft:block.dried_ghast.transition"); pitch != 1.0 {
		t.Errorf("the hydration step played at pitch %v, want vanilla's 1.0", pitch)
	}
}

// awaitEntitySound reads c's stream until the named entity sound effect
// arrives and returns the body it names and the pitch it played at.
func awaitEntitySound(t *testing.T, c *servertest.Client, want string) (int32, float32) {
	t.Helper()
	for id, data := range c.Stream() {
		if id != protocol.IDPlayEntitySoundEffect {
			continue
		}
		r := protocol.NewReader(data)
		wire := r.ReadVarInt()
		name := ""
		if wire > 0 && int(wire) <= len(registry.SoundEvents) {
			name = registry.SoundEvents[wire-1]
		}
		r.ReadVarInt() // category
		entityID := r.ReadVarInt()
		r.ReadF32() // volume
		pitch := r.ReadF32()
		if r.Err() != nil {
			t.Fatalf("the %s entity sound did not read back: %v", want, r.Err())
		}
		if name == want {
			return entityID, pitch
		}
	}
	t.Fatalf("no %s entity sound", want)
	return 0, 0
}

// TestAHatchedGhastlingFacesTheBlocksWay checks the yaw and head yaw a hatch
// gives the ghastling for each horizontal facing the block carries, south
// and north already covered by the staircase test.
// vanilla: DriedGhastBlock.getStateForPlacement
func TestAHatchedGhastlingFacesTheBlocksWay(t *testing.T) {
	cases := []struct {
		facing string
		yaw    float32
	}{
		{"south", 0},
		{"west", 90},
		{"east", 270},
	}
	for _, tc := range cases {
		t.Run(tc.facing, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

			srv.StopTick()
			servertest.SeedPhysics(t, srv, driedGhastSeed)
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)

			servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dried_ghast",
				map[string]string{"facing": tc.facing, "hydration": "3", "waterlogged": "true"})
			driedGhastHop(srv)
			c.ExpectMobSpawn(protocol.EntityTypeHappyGhast)

			mobs := srv.Mobs()
			if len(mobs) != 1 {
				t.Fatalf("mobs in the world = %d, want the one ghastling", len(mobs))
			}
			if m := mobs[0]; m.Yaw != tc.yaw || m.HeadYaw != tc.yaw {
				t.Errorf("the %s-facing ghastling faces yaw %v, head %v, want %v",
					tc.facing, m.Yaw, m.HeadYaw, tc.yaw)
			}
		})
	}
}

// TestAHappyGhastEggStandsAHappyGhastUp checks the kind's egg spends
// itself on a standing body, the flying move control now driving it. The egg
// stands up the grown body; the rare ghastling a vanilla egg may roll is not
// modelled.
func TestAHappyGhastEggStandsAHappyGhastUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Camper", "minecraft:happy_ghast_spawn_egg", 1)
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.Settle()
	if mobs := srv.Mobs(); len(mobs) != 1 {
		t.Errorf("the happy ghast egg stood up %d mobs, want the happy ghast", len(mobs))
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot).Count; got != 0 {
		t.Errorf("eggs left = %d, want the spent egg", got)
	}
}

// TestSummonStandsAHappyGhastUp checks /summon names the kind and the
// creation stands the body up, the flying move control now driving it.
func TestSummonStandsAHappyGhastUp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	c.ChatCommand("summon minecraft:happy_ghast 5.5 64 0.5")
	c.Settle()
	if mobs := srv.Mobs(); len(mobs) != 1 {
		t.Errorf("the summon stood up %d mobs, want the happy ghast", len(mobs))
	}
}

// TestADispenserPoursAthwartADriedGhast checks the dispenser's half of the
// pour: a dispenser whose water bucket opens onto a dried ghast waterlogs it
// in its own cell and plays the in-water sound.
// vanilla: DispenserBlock's bucket behavior, DriedGhastBlock.placeLiquid
func TestADispenserPoursAthwartADriedGhast(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, -1.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()

	srv.StopTick()
	if !srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:dried_ghast", map[string]string{
		"facing": "north", "hydration": "0", "waterlogged": "false"})) {
		t.Fatal("the dried ghast's chunk was not loaded")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:water_bucket", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	w.Sneak(true)
	w.PlaceBlock(4, 64, 0, protocol.FaceTop)
	w.Sneak(false)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	srv.TickPhysics(8)

	if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:dried_ghast" || props["waterlogged"] != "true" {
		t.Errorf("the dispenser's pour left %s %v, want the waterlogged dried ghast", name, props)
	}
	if pitch := servertest.AwaitSoundPitch(t, w.Client, "minecraft:block.dried_ghast.place_in_water"); pitch != 1.0 {
		t.Errorf("the dispenser's waterlogging played at pitch %v, want vanilla's 1.0", pitch)
	}
}
