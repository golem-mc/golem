package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The potent sulfur block, black-box: its state follows the water above and
// the block below, a wet block gasses the swimmers beside it, and a geyser
// pushes bodies up its water column or erupts on a timer.
// vanilla: PotentSulfurBlock, PotentSulfurBlockEntity

const sulfurFloorY = 70

func sulfurStateOf(t *testing.T, srv *server.Server, x, y, z int32) string {
	t.Helper()
	prop, ok := registry.PropNamed("potent_sulfur_state")
	if !ok {
		t.Fatal("the registry has no potent_sulfur_state")
	}
	state := srv.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockPotentSulfur {
		t.Fatalf("block at %d %d %d = %s, want potent sulfur", x, y, z, registry.BlockOf(state).Name())
	}
	return registry.StateValue(state, prop)
}

// buildSulfurPool lays a 7x7 stone floor at the potent sulfur's level around
// (cx, cz) with two source blocks of water over it, except the sulfur's own
// column, and the block named under the sulfur's cell. The sulfur and its two
// water blocks are set by command afterwards, so the shape updates run.
func buildSulfurPool(t *testing.T, srv *server.Server, cx, cz int32, under string) {
	t.Helper()
	stone := servertest.BlockState(t, "minecraft:stone")
	water := servertest.BlockState(t, "minecraft:water")
	for x := cx - 3; x <= cx+3; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			ok := srv.SetBlock(x, sulfurFloorY-1, z, stone) && srv.SetBlock(x, sulfurFloorY, z, stone)
			if x != cx || z != cz {
				ok = ok && srv.SetBlock(x, sulfurFloorY+1, z, water) && srv.SetBlock(x, sulfurFloorY+2, z, water)
			}
			if !ok {
				t.Fatal("the pool's chunk was not loaded")
			}
		}
	}
	// SetBlock reports false for an unchanged cell, so the stone case is a no-op.
	srv.SetBlock(cx, sulfurFloorY-1, cz, servertest.BlockState(t, under))
}

func placeSulfur(c *servertest.Client, cx, cz int32) {
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:potent_sulfur", cx, sulfurFloorY, cz))
	c.Settle()
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:water", cx, sulfurFloorY+1, cz))
	c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:water", cx, sulfurFloorY+2, cz))
	c.Settle()
}

func TestPotentSulfurStateFollowsWaterAndBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:stone")
	buildSulfurPool(t, srv, 20, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	placeSulfur(c, 20, 8)

	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "wet" {
		t.Errorf("sulfur under water = %q, want wet", got)
	}
	if got := sulfurStateOf(t, srv, 20, sulfurFloorY, 8); got != "continuous" {
		t.Errorf("sulfur over lava under water = %q, want continuous", got)
	}
	c.ChatCommand("setblock 8 71 8 minecraft:air")
	c.ChatCommand("setblock 20 71 8 minecraft:air")
	c.Settle()
	for _, x := range []int32{8, 20} {
		if got := sulfurStateOf(t, srv, x, sulfurFloorY, 8); got != "dry" {
			t.Errorf("sulfur at x=%d with the water removed = %q, want dry", x, got)
		}
	}
}

func TestPotentSulfurGasGivesNauseaToASwimmer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Swimmer", servertest.OpsListing(t, "Swimmer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("gamemode survival")
	c.Settle()
	buildSulfurPool(t, srv, 8, 8, "minecraft:stone")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "wet" {
		t.Fatalf("sulfur under water = %q, want wet", got)
	}
	// Two blocks east of the sulfur, the head above the water's top.
	c.MoveToGround(10.5, 72, 8.5, false)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "nausea on the swimmer", func() bool {
		_, _, ok := srv.PlayerEffect("Swimmer", "minecraft:nausea")
		return ok
	})
	if d, amp, _ := srv.PlayerEffect("Swimmer", "minecraft:nausea"); amp != 0 || d <= 0 || d > 80 {
		t.Errorf("nausea = %d ticks, amplifier %d, want at most 80 and 0", d, amp)
	}
}

func TestPotentSulfurContinuousGeyserLaunchesAnItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	srv.SpawnItemEntity(8.5, 71.5, 8.5, "minecraft:stick", 1)
	servertest.UntilTicks(t, srv, 5, "an upward velocity on the item", func() bool {
		items := srv.ItemsInDimension("")
		return len(items) == 1 && items[0].VY > 0.1
	})
}

// A continuous geyser lifts a boat and an armor stand too.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER
func TestPotentSulfurContinuousGeyserLaunchesABoatAndAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	boat := srv.SpawnBoat(8.5, 73, 8.5, "minecraft:oak_boat")
	srv.SpawnArmorStand(8.5, 73, 8.5)
	servertest.RunTicks(t, srv, 1)
	_, boatY, _, _, _, _ := srv.BoatState(boat)
	stands := srv.ArmorStandsInDimension("")
	if len(stands) != 1 {
		t.Fatalf("%d armor stands, want 1", len(stands))
	}
	standY := stands[0].Y
	servertest.RunTicks(t, srv, 5)
	if _, y, _, _, _, _ := srv.BoatState(boat); y <= boatY {
		t.Errorf("boat y = %v after 5 ticks, started at %v: not lifted", y, boatY)
	}
	if got := srv.ArmorStandsInDimension("")[0].Y; got <= standY {
		t.Errorf("armor stand y = %v after 5 ticks, started at %v: not lifted", got, standY)
	}
}

// A body that is not falling fast banks at most one block of fall in the column.
// vanilla: Entity.checkFallDistanceAccumulation
func TestPotentSulfurGeyserCapsAMobsFallDistance(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:lava")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "continuous" {
		t.Fatalf("sulfur over lava under water = %q, want continuous", got)
	}
	zombie := srv.SpawnMob("minecraft:zombie", 8.5, 74, 8.5)
	uuid, _ := srv.MobUUID(zombie)
	c.ChatCommand("data merge entity " + uuid + " {fall_distance:10d}")
	c.Settle()
	if got, _ := srv.MobFallDistance(zombie); got < 5 {
		t.Fatalf("fall distance = %v before the tick, want the 10 set", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got, _ := srv.MobFallDistance(zombie); got > 1 {
		t.Errorf("fall distance = %v after a tick in the column, want at most 1", got)
	}
}

func TestPotentSulfurDormantGeyserErupts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	buildSulfurPool(t, srv, 8, 8, "minecraft:magma_block")
	placeSulfur(c, 8, 8)
	if got := sulfurStateOf(t, srv, 8, sulfurFloorY, 8); got != "dormant" {
		t.Fatalf("sulfur over magma under water = %q, want dormant", got)
	}
	// Two water blocks: the countdown rolls 10*(2-1)+15..30 strides of 20
	// ticks asleep and 2..3 erupting; the first stride lands within 20 ticks.
	wait := func(want string, lo, hi int) {
		t.Helper()
		for n := 1; n <= hi; n++ {
			servertest.RunTicks(t, srv, 1)
			if sulfurStateOf(t, srv, 8, sulfurFloorY, 8) == want {
				if n < lo {
					t.Fatalf("became %s after %d ticks, want at least %d", want, n, lo)
				}
				return
			}
		}
		t.Fatalf("not %s within %d ticks", want, hi)
	}
	wait("erupting", 481, 800)
	wait("dormant", 40, 60)
}
