package gamemode

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestSpectatorToCreativeLandsNearGround switches a flying spectator to
// creative over the flat world's stone: half a block up the Player Abilities
// packet the switch sends has the flying bit clear, five blocks up it stays
// set.
// vanilla: ServerPlayerGameMode.changeGameModeForPlayer, isInRangeOfGround
func TestSpectatorToCreativeLandsNearGround(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	c.Join("Ghost")
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		name       string
		y          float64
		wantFlying bool
	}{
		{"five blocks up", world.GroundLevel + 5, true},
		{"half a block up", world.GroundLevel + 0.5, false},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			c.ChatCommand("gamemode spectator")
			if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayAbilities); !ok {
				t.Fatal("no Player Abilities after the switch to spectator")
			}
			c.Settle()
			c.ChatCommand(fmt.Sprintf("tp @s 0.5 %v 0.5", tc.y))
			servertest.ExpectTeleport(t, c)
			if got := srv.PlayerPosition("Ghost"); got.Y != tc.y {
				t.Fatalf("the spectator is at y %v, want %v", got.Y, tc.y)
			}
			c.ChatCommand("gamemode creative")
			data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayAbilities)
			if !ok {
				t.Fatal("no Player Abilities after the switch to creative")
			}
			if got := protocol.NewReader(data).ReadI8()&protocol.AbilityFlying != 0; got != tc.wantFlying {
				t.Errorf("flying bit = %v, want %v", got, tc.wantFlying)
			}
			c.Settle()
		})
	}
}
