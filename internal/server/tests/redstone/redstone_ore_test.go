package redstone

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
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
