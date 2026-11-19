// The shape tests: what the collision shapes — the modelled boxes, the tall
// fences and walls and the chests' 14-sixteenth rims, and the generated
// shapes no Go model carries — mean for what a client can watch: where a
// spawn egg's mob lands, where a dropped item rests, which pose a body
// pressed against a shape keeps, and whether a placement through a shape's
// empty part goes through.
package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestASpawnEggOnATallShapeSpawnsItsMobOnTheCollision checks the egg
// placement against the shapes whose collision is taller than their cell's
// look: a fence's and a wall's collision tops stand 24 sixteenths tall, so
// the cow an egg places on either lands on the shape's top at the cell
// floor plus 1.5, not on a full cube's 1.0.
func TestASpawnEggOnATallShapeSpawnsItsMobOnTheCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		props map[string]string
	}{
		{"on an oak fence", "minecraft:oak_fence", map[string]string{
			"east": "false", "north": "false", "south": "false",
			"waterlogged": "false", "west": "false"}},
		{"on a cobblestone wall", "minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "none", "south": "none", "up": "true",
			"waterlogged": "false", "west": "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Rancher", "minecraft:cow_spawn_egg", 1)
			srv.SetBlock(2, 64, 3, servertest.MustState(t, tc.block, tc.props))
			c.PlaceBlock(2, 64, 3, protocol.FaceTop)
			cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
			if cow.X != 2.5 || cow.Y != 65.5 || cow.Z != 3.5 {
				t.Errorf("cow spawned at (%v, %v, %v), want (2.5, 65.5, 3.5)",
					cow.X, cow.Y, cow.Z)
			}
			c.ExpectBlockAck()
		})
	}
}

// TestADroppedItemRestsOnAChestsRimAndStaysOnIt drops a stick over a
// chest: the chest's collision is its own 14-sixteenths-open rim, so the
// stick rests on the rim at the chest's floor plus 0.875 and nothing in
// the ground's way moves it from there — not the escape a body buried
// inside a full cube's shape would make.
func TestADroppedItemRestsOnAChestsRimAndStaysOnIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Dropper")
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:chest",
		map[string]string{"facing": "north", "type": "single", "waterlogged": "false"})) {
		t.Fatal("the chest's chunk was not loaded")
	}
	srv.SpawnItemEntity(2.5, 66, 3.5, "minecraft:stick", 1)
	rest := func() (x, y, z float64, ok bool) {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return 0, 0, 0, false
		}
		return items[0].X, items[0].Y, items[0].Z, true
	}
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x, y, z, ok := rest()
		return ok && x == 2.5 && y == 65 && z == 3.5
	})
	x, y, z, _ := rest()
	if x != 2.5 || y != 65 || z != 3.5 {
		t.Errorf("the stick came to rest at (%v, %v, %v), want the chest's rim at (2.5, 65, 3.5)", x, y, z)
	}
	// The rest itself holds: an item lying on the shape neither sinks
	// into the cell nor gets pushed out of it.
	c.Settle()
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x2, y2, z2, ok := rest()
		return ok && x2 == 2.5 && y2 == 65 && z2 == 3.5
	})
}

// TestADroppedItemFallsIntoTheHoppersBowl drops a stick over a hopper: the
// hopper's collision is its own funnel — a mouth whose hole passes the
// stick's box and a floor under it — so the stick falls through the mouth
// and rests inside the bowl, where the coarse whole-cube shape would leave
// it perched on the hopper's cell top.
func TestADroppedItemFallsIntoTheHoppersBowl(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Bowl")
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:hopper",
		map[string]string{"enabled": "false", "facing": "down"})) {
		t.Fatal("the hopper's chunk was not loaded")
	}
	srv.SpawnItemEntity(2.5, 66, 3.5, "minecraft:stick", 1)
	// The box the stick's centre comes to rest at: the funnel floor's top,
	// 11 sixteenths, and the stick's own half size above it.
	const restY = 64 + 11.0/16 + 0.125
	rest := func() (x, y, z float64, ok bool) {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return 0, 0, 0, false
		}
		return items[0].X, items[0].Y, items[0].Z, true
	}
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x, y, z, ok := rest()
		return ok && x == 2.5 && y == restY && z == 3.5
	})
	x, y, z, _ := rest()
	if x != 2.5 || y != restY || z != 3.5 {
		t.Errorf("the stick came to rest at (%v, %v, %v), want in the bowl at (2.5, %v, 3.5)",
			x, y, z, restY)
	}
	// The rest itself holds: a stick inside the bowl stays inside it.
	c.Settle()
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x2, y2, z2, ok := rest()
		return ok && x2 == 2.5 && y2 == restY && z2 == 3.5
	})
}

// TestPlacingACollidingBlockIntoYourOwnCellIsRefused places blocks whose
// collision shape is non-empty (a carpet, a coral block) into the cell the
// player stands in, which is refused, while shapeless blocks (a torch, a
// flower) are accepted.
func TestPlacingACollidingBlockIntoYourOwnCellIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item   string
		placed bool
	}{
		{"minecraft:white_carpet", false},
		{"minecraft:tube_coral_block", false},
		{"minecraft:torch", true},
		{"minecraft:poppy", true},
		{"minecraft:scaffolding", true},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
			srv.StopTick()
			c := connect().Join("Placer")
			srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:grass_block", map[string]string{"snowy": "false"}))
			item, _ := registry.ItemID(tc.item)
			srv.GiveSlot("Placer", player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 2})
			c.PlaceBlock(0, 63, 0, protocol.FaceTop)
			c.SettleKeeping()
			srv.FlushBlockAcks()
			if got := srv.BlockAt(0, 64, 0) != 0; got != tc.placed {
				t.Errorf("%s into the player's own cell placed = %v, want %v", tc.item, got, tc.placed)
			}
		})
	}
}
