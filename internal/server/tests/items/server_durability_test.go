package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The wear tests: what a creative player's infinite materials spare. Each
// use runs in both modes, so survival's one wear is the control the creative
// zero is read against.

// durabilityConfig is the wear tests' world: offline, in the mode asked for.
func durabilityConfig(mode string) *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = mode
	return cfg
}

// forEachWearMode runs work in creative and survival, handing it the wear
// the mode should spend: none for a creative player's infinite materials,
// one for survival.
func forEachWearMode(t *testing.T, name string, work func(t *testing.T, srv *server.Server, c *servertest.Client, want int32)) {
	t.Helper()
	for _, tc := range []struct {
		mode string
		want int32
	}{{"creative", 0}, {"survival", 1}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, durabilityConfig(tc.mode), name)
			work(t, srv, c, tc.want)
		})
	}
}

// TestACreativeBowShotSpendsNoBow fires an arrow in both modes: a survival
// bow takes the shot's wear, a creative one none.
func TestACreativeBowShotSpendsNoBow(t *testing.T) {
	t.Parallel()
	forEachWearMode(t, "Archer", func(t *testing.T, srv *server.Server, c *servertest.Client, want int32) {
		servertest.GiveBowAndArrows(srv, "Archer")
		c.MoveTo(0.5, 64, 0.5, -90, 0)

		// The draw is counted out in hand-driven ticks, as the bow tests
		// do: a running loop could pass the full draw's twenty between a
		// poll and the release.
		srv.StopTick()
		servertest.DrawBow(c, -90, 0)
		c.Settle()
		for i := 0; srv.BowTicks("Archer") < 15; i++ {
			if i == 15 {
				t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks("Archer"))
			}
			srv.TickSurvival()
		}
		servertest.ReleaseBow(c)
		// Not a poll of BowTicks: the release zeroes the draw before it
		// fires the arrow and wears the bow.
		c.Settle()
		srv.StartTick()

		if got := srv.InventorySlot("Archer", 36).Damage; got != want {
			t.Errorf("the bow's wear = %d, want %d", got, want)
		}
	})
}

// TestACreativeShearingSpendsNoShears shears a sheep in both modes: a
// survival pair of shears takes a wear, a creative pair none.
func TestACreativeShearingSpendsNoShears(t *testing.T) {
	t.Parallel()
	forEachWearMode(t, "Shearer", func(t *testing.T, srv *server.Server, c *servertest.Client, want int32) {
		c.MoveToGround(2.5, 64, 5.5, true)
		c.Settle()

		srv.GiveSlot("Shearer", player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shears"), Count: 1})
		sheep := srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
		c.ExpectMobSpawn(protocol.EntityTypeSheep)
		c.UseOn(sheep)
		c.Settle()

		if got := srv.InventorySlot("Shearer", player.HotbarSlot).Damage; got != want {
			t.Errorf("the shears' wear = %d, want %d", got, want)
		}
	})
}

// TestACreativeSwordBlowSpendsNoBlade hits a cow in both modes: a survival
// blade takes the blow's wear, a creative one none.
func TestACreativeSwordBlowSpendsNoBlade(t *testing.T) {
	t.Parallel()
	forEachWearMode(t, "Fencer", func(t *testing.T, srv *server.Server, c *servertest.Client, want int32) {
		// Twice, so the last reported move is none and nothing sweeps by
		// speed.
		c.MoveToGround(2.5, 64, 5.5, true)
		c.MoveToGround(2.5, 64, 5.5, true)
		c.Settle()

		srv.GiveSlot("Fencer", player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
		servertest.AwaitFullSwing(t, srv, "Fencer")
		cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 5.5)
		c.ExpectMobSpawn(protocol.EntityTypeCow)
		c.Attack(cow)
		c.Settle()

		if got := srv.InventorySlot("Fencer", player.HotbarSlot).Damage; got != want {
			t.Errorf("the sword's wear = %d, want %d", got, want)
		}
	})
}

// TestAnUnbreakableToolTakesNoWear digs an ice block with a plain diamond
// pickaxe and with an unbreakable one: the plain tool's wear is the control
// the unbreakable zero is read against.
func TestAnUnbreakableToolTakesNoWear(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, durabilityConfig("survival"), "Icebreaker")
	servertest.GroundThePlayer(c)

	ice := registry.BlockStates["minecraft:ice"]
	srv.SetBlock(1, 63, 1, ice)
	srv.GiveSlot("Icebreaker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_pickaxe"), Count: 1})
	c.BreakBlock(1, 63, 1)
	c.WaitBlockChanged()
	if got := srv.InventorySlot("Icebreaker", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the plain pickaxe's wear = %d, want 1", got)
	}

	srv.SetBlock(2, 63, 2, ice)
	srv.GiveSlot("Icebreaker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_pickaxe"), Count: 1, Unbreakable: true})
	c.BreakBlock(2, 63, 2)
	c.WaitBlockChanged()
	if got := srv.InventorySlot("Icebreaker", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("the unbreakable pickaxe's wear = %d, want none", got)
	}
}

// TestAZeroHardnessBlockTakesNoWear breaks a torch and a flower with an iron
// sword and a stone block with a diamond pickaxe: a block whose destroy speed
// is zero costs the held tool nothing, while the stone still takes the wear.
// vanilla: Item.mineBlock
func TestAZeroHardnessBlockTakesNoWear(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, durabilityConfig("survival"), "Scout")
	servertest.GroundThePlayer(c)

	sword := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1}
	for _, block := range []string{"minecraft:torch", "minecraft:dandelion"} {
		srv.GiveSlot("Scout", player.HotbarSlot, sword)
		srv.SetBlock(1, 63, 1, registry.BlockStates[block])
		c.StartDigging(1, 63, 1)
		c.WaitBlockChanged()
		if got := srv.InventorySlot("Scout", player.HotbarSlot).Damage; got != 0 {
			t.Errorf("the sword after a %s = wear %d, want none", block, got)
		}
	}

	srv.GiveSlot("Scout", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_pickaxe"), Count: 1})
	srv.SetBlock(2, 63, 2, registry.BlockStates["minecraft:stone"])
	c.BreakBlock(2, 63, 2)
	c.WaitBlockChanged()
	if got := srv.InventorySlot("Scout", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the pickaxe's wear after a stone = %d, want 1", got)
	}
}

// TestShearsWearOnZeroHardnessBlocks breaks a dandelion, short grass and tall
// grass with fresh shears and fire with a fresh pair: shears have no
// destroy-speed check, so every block but fire costs one wear, and a creative
// player's infinite materials cost none.
// vanilla: ShearsItem.mineBlock
func TestShearsWearOnZeroHardnessBlocks(t *testing.T) {
	t.Parallel()
	forEachWearMode(t, "Mower", func(t *testing.T, srv *server.Server, c *servertest.Client, want int32) {
		servertest.GroundThePlayer(c)
		fresh := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shears"), Count: 1}
		for _, block := range []string{"minecraft:dandelion", "minecraft:short_grass", "minecraft:tall_grass"} {
			srv.GiveSlot("Mower", player.HotbarSlot, fresh)
			srv.SetBlock(1, 63, 1, registry.BlockStates[block])
			c.StartDigging(1, 63, 1)
			c.WaitBlockChanged()
			if got := srv.InventorySlot("Mower", player.HotbarSlot).Damage; got != want {
				t.Errorf("the shears after a %s = wear %d, want %d", block, got, want)
			}
		}

		// The netherrack floor holds the fire up so its own tick does not
		// put it out before the dig does.
		srv.SetBlock(1, 62, 1, registry.BlockStates["minecraft:netherrack"])
		srv.GiveSlot("Mower", player.HotbarSlot, fresh)
		srv.SetBlock(1, 63, 1, registry.MustState("minecraft:fire", nil))
		c.StartDigging(1, 63, 1)
		c.WaitBlockChanged()
		if got := srv.InventorySlot("Mower", player.HotbarSlot).Damage; got != 0 {
			t.Errorf("the shears after fire = wear %d, want none", got)
		}
	})
}

// TestAnAdventureCarveSpendsTheShears carves a pumpkin in adventure, which
// wears like survival: only a creative player's infinite materials are
// spared, so the shears take one wear.
func TestAnAdventureCarveSpendsTheShears(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, durabilityConfig("adventure"), "Carver")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.GiveSlot("Carver", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shears"), Count: 1})
	srv.SetBlock(3, 64, 5, registry.BlockStates["minecraft:pumpkin"])
	c.PlaceBlockAt(3, 64, 5, protocol.FaceEast, 0.5)
	c.Settle()

	if got := srv.InventorySlot("Carver", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the shears' wear = %d, want 1", got)
	}
}
