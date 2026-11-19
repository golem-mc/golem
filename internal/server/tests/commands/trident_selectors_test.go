package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ThrownTrident constructors, EntitySelectorOptions.bootStrap (type)
func TestSelectorsUseAThrownTridentsType(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Thrower", servertest.OpsListing(t, "Thrower"))
	servertest.HoldTicks(t, srv)
	c.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	c.Settle()
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(5, y, 0, registry.BlockStone.DefaultState())
	}
	srv.GiveSlot("Thrower", 36, servertest.ItemStack(t, "minecraft:trident", 1))
	servertest.DrawBow(c, -90, 0)
	servertest.RunTicks(t, srv, 12)
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	spawn := c.AwaitSpawn(protocol.EntityTypeTrident)

	for _, phase := range []string{"flight", "stuck"} {
		if phase == "stuck" {
			servertest.RunTicks(t, srv, 8)
			arrows := srv.ArrowsInDimension("minecraft:overworld")
			if len(arrows) != 1 || arrows[0].ID != spawn.EntityID || arrows[0].X < 4 || arrows[0].X >= 5 || arrows[0].VX != 0 {
				t.Fatalf("trident did not stick in the stone wall: %+v", arrows)
			}
		}
		for _, tc := range []struct {
			filter string
			match  bool
		}{
			{"type=minecraft:trident", true},
			{"type=minecraft:arrow", false},
			{"type=minecraft:spectral_arrow", false},
			{"type=!minecraft:trident,type=!minecraft:player", false},
			{"type=!minecraft:arrow,type=!minecraft:player", true},
			{"name=Trident", true},
		} {
			cmd := "execute if entity @e[" + tc.filter + "]"
			c.ChatCommand(cmd)
			want := servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")
			if tc.match {
				want = conditionalPassCountForTest(1)
			}
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, want) {
				t.Errorf("%s: /%s answered % X, want % X", phase, cmd, got, want)
			}
		}
	}
	c.ChatCommand("kill @e[type=minecraft:trident]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:trident", "entity.minecraft.trident", spawn.Uuid))
}
