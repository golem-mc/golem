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
