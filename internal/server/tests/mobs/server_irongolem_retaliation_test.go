package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: IronGolem.registerGoals/canAttack, HurtByTargetGoal
func TestAnIronGolemRetaliatesAgainstItsSurvivalAttacker(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       string
		difficulty uint8
		built      bool
		spectate   bool
		retaliates bool
	}{
		{name: "survival", mode: "survival", difficulty: 2, retaliates: true},
		{name: "player built", mode: "survival", difficulty: 2, built: true},
		{name: "peaceful", mode: "survival", difficulty: 0},
		{name: "creative", mode: "creative", difficulty: 2},
		{name: "spectator", mode: "spectator", difficulty: 2},
		{name: "spectator after hit", mode: "survival", difficulty: 2, spectate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "GolemAttacker"
			cfg := servertest.MobTestConfig()
			cfg.GameMode = tc.mode
			srv, c, playerID := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
			srv.SetSpawning(false)
			srv.SetDifficulty(tc.difficulty)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			servertest.AwaitFullSwing(t, srv, name)
			c.ChatCommand(fmt.Sprintf("summon minecraft:iron_golem 2.5 64 0.5 {PlayerCreated:%t}", tc.built))
			id := c.AwaitSpawn(protocol.EntityTypeIronGolem).EntityID
			if got := srv.MobTargetID(id); got != 0 {
				t.Fatalf("unprovoked golem targets %d, want nobody", got)
			}

			c.Attack(id)
			c.Settle()
			health, ok := srv.MobHealth(id)
			if !ok || (health < 100) != (tc.mode != "spectator") {
				t.Fatalf("golem health after attack = %v, present %v; mode %s", health, ok, tc.mode)
			}
			if tc.spectate {
				c.ChatCommand("gamemode spectator")
				c.Settle()
				if got := srv.PlayerGameMode(name); got != protocol.GameModeSpectator {
					t.Fatalf("game mode = %d, want spectator", got)
				}
			}

			if !tc.retaliates {
				// Check each tick so a briefly selected target cannot go unnoticed.
				for range 60 {
					servertest.RunTicks(t, srv, 1)
					if got := srv.MobTargetID(id); got != 0 {
						t.Fatalf("exempt attacker targeted: %d", got)
					}
					if got := srv.PlayerHealth(name); got != 20 {
						t.Fatalf("exempt attacker health = %v, want 20", got)
					}
				}
				return
			}

			// Retreat while ticks are held: a stationary melee swing cannot pass
			// the pursuit assertion, and knockback cannot move the golem toward us.
			c.MoveToGround(-5.5, 64, 0.5, true)
			c.Settle()
			servertest.UntilTicks(t, srv, 20, "the golem to target its attacker", func() bool {
				return srv.MobTargetID(id) == playerID
			})
			servertest.UntilTicks(t, srv, 60, "the golem to pursue its attacker", func() bool {
				m, found := servertest.MobByID(srv, id)
				return found && m.X < 0.5 && srv.MobTargetID(id) == playerID
			})
			servertest.UntilTicks(t, srv, 60, "the golem to damage its attacker", func() bool {
				return srv.PlayerHealth(name) < 20
			})
		})
	}
}
