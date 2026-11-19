package redstone

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The redstone ore lights when it is used, hit or stepped on, and a random
// tick unlights it.
// vanilla: RedStoneOreBlock

const growthTickSpeed = 4000

var redstoneOres = []string{"minecraft:redstone_ore", "minecraft:deepslate_redstone_ore"}

func oreLitState(t *testing.T, name string, lit bool) uint16 {
	t.Helper()
	v := "false"
	if lit {
		v = "true"
	}
	return servertest.CropsStateOf(t, name, map[string]string{"lit": v})
}

// redstoneOreWorld joins a player and puts the ore at (5, 64, 0), unlit.
func redstoneOreWorld(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner", servertest.OpsListing(t, "Miner"))
	srv.StopTick()
	if !srv.SetBlock(5, 64, 0, oreLitState(t, name, false)) {
		t.Fatal("the ore's chunk was not loaded")
	}
	c.Settle()
	return srv, c
}

func TestRedstoneOreLightsOnUse(t *testing.T) {
	t.Parallel()
	for _, name := range redstoneOres {
		t.Run(name, func(t *testing.T) {
			srv, c := redstoneOreWorld(t, name)
			c.UseBlock(5, 64, 0)
			c.AwaitBlockState(5, 64, 0, oreLitState(t, name, true))
			if srv.BlockAt(5, 64, 0) != oreLitState(t, name, true) {
				t.Error("the ore is not lit after the click")
			}
		})
	}
}

func TestRedstoneOreLightsOnHit(t *testing.T) {
	t.Parallel()
	for _, name := range redstoneOres {
		t.Run(name, func(t *testing.T) {
			srv, c := redstoneOreWorld(t, name)
			c.StartDigging(5, 64, 0)
			c.AwaitBlockState(5, 64, 0, oreLitState(t, name, true))
			if srv.BlockAt(5, 64, 0) != oreLitState(t, name, true) {
				t.Error("the ore is not lit after the hit")
			}
		})
	}
}

func TestRedstoneOreLightsWhenSteppedOn(t *testing.T) {
	t.Parallel()
	for _, name := range redstoneOres {
		t.Run(name, func(t *testing.T) {
			srv, c := redstoneOreWorld(t, name)
			c.MoveToGround(5.5, 65, 0.5, true)
			c.SettleKeeping()
			srv.TickSurvival()
			c.AwaitBlockState(5, 64, 0, oreLitState(t, name, true))
		})
	}
}

func TestRedstoneOreStaysDarkUnderASneakingStep(t *testing.T) {
	t.Parallel()
	srv, c := redstoneOreWorld(t, "minecraft:redstone_ore")
	c.SendSneak(true)
	c.MoveToGround(5.5, 65, 0.5, true)
	c.SettleKeeping()
	srv.TickSurvival()
	c.Settle()
	if srv.BlockAt(5, 64, 0) != oreLitState(t, "minecraft:redstone_ore", false) {
		t.Error("a sneaking step lit the ore")
	}
}

func TestRedstoneOreGoesDarkOnRandomTicks(t *testing.T) {
	t.Parallel()
	for _, name := range redstoneOres {
		t.Run(name, func(t *testing.T) {
			srv, c := redstoneOreWorld(t, name)
			srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), growthTickSpeed)
			if !srv.SetBlock(5, 64, 0, oreLitState(t, name, true)) {
				t.Fatal("the ore's chunk was not loaded")
			}
			dark := oreLitState(t, name, false)
			for range 500 {
				if srv.BlockAt(5, 64, 0) == dark {
					break
				}
				srv.TickRandomTicks(1)
			}
			if srv.BlockAt(5, 64, 0) != dark {
				t.Fatal("the lit ore never went dark")
			}
			c.AwaitBlockState(5, 64, 0, dark)
		})
	}
}

func TestRedstoneOreLightIsNine(t *testing.T) {
	if registry.LightEmission(oreLitState(t, "minecraft:redstone_ore", true)) != 9 {
		t.Error("a lit ore does not give light 9")
	}
	if registry.LightEmission(oreLitState(t, "minecraft:redstone_ore", false)) != 0 {
		t.Error("an unlit ore gives light")
	}
}

func TestRedstoneOreLightsAndStillTakesABlockPlacedAgainstIt(t *testing.T) {
	t.Parallel()
	srv, c := redstoneOreWorld(t, "minecraft:redstone_ore")
	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.PlaceBlock(5, 64, 0, protocol.FaceTop)
	c.AwaitBlockState(5, 65, 0, servertest.CropsStateOf(t, "minecraft:stone", nil))
	if srv.BlockAt(5, 64, 0) != oreLitState(t, "minecraft:redstone_ore", true) {
		t.Error("the ore is not lit after a block was placed against it")
	}
}

func TestRedstoneOreStaysDarkUnderASpectator(t *testing.T) {
	t.Parallel()
	srv, c := redstoneOreWorld(t, "minecraft:redstone_ore")
	c.ChatCommand("gamemode spectator Miner")
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		return srv.PlayerGameMode("Miner") == protocol.GameModeSpectator
	})
	c.Settle()
	c.MoveToGround(5.5, 65, 0.5, true)
	c.SettleKeeping()
	srv.TickSurvival()
	c.Settle()
	if srv.BlockAt(5, 64, 0) != oreLitState(t, "minecraft:redstone_ore", false) {
		t.Error("a spectator lit the ore")
	}
}

func TestRedstoneOreLightsAndEndsAnEmptyHandClick(t *testing.T) {
	t.Parallel()
	srv, c := redstoneOreWorld(t, "minecraft:redstone_ore")
	c.UseBlock(5, 64, 0)
	c.AwaitBlockState(5, 64, 0, oreLitState(t, "minecraft:redstone_ore", true))
	c.Settle()
	if srv.BlockAt(5, 65, 0) != 0 {
		t.Error("an empty hand placed something after the click")
	}
}

// bodyOnOreWorld joins a player 6 blocks off unlit ore laid in the ground at
// (x, 63, 0), with the loop held.
func bodyOnOreWorld(t *testing.T, x int32) *server.Server {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner")
	c.MoveToGround(float64(x)+6.5, 64, 0.5, true)
	c.SettleKeeping()
	servertest.HoldTicks(t, srv)
	if !srv.SetBlock(x, 63, 0, oreLitState(t, "minecraft:redstone_ore", false)) {
		t.Fatal("the ore's chunk was not loaded")
	}
	return srv
}

// oreLit reports whether the ore at (x, 63, 0) is lit.
func oreLit(t *testing.T, srv *server.Server, x int32) func() bool {
	return func() bool {
		_, props := servertest.StatePropsOf(t, srv, x, 63, 0)
		return props["lit"] == "true"
	}
}

func TestRedstoneOreLightsUnderAMob(t *testing.T) {
	t.Parallel()
	srv := bodyOnOreWorld(t, 2)
	srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 5, "the zombie lights the ore", oreLit(t, srv, 2))
}

func TestRedstoneOreLightsUnderADroppedItem(t *testing.T) {
	t.Parallel()
	srv := bodyOnOreWorld(t, 4)
	// The spawn height is the item's centre: from 64 its box would start
	// inside the ore and be pushed out sideways.
	srv.SpawnItemEntity(4.5, 64.25, 0.5, "minecraft:stone", 1)
	servertest.UntilTicks(t, srv, 20, "the item lights the ore", oreLit(t, srv, 4))
}
