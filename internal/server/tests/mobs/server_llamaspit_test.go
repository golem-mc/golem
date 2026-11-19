package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestALlamaSpitsAtItsAttackerAndHurtsThem: a llama the player hits targets
// them, spits, the spit hurts the player and the llama drops its target.
// vanilla: Llama.spit, LlamaSpit.onHitEntity, RangedAttackGoal
func TestALlamaSpitsAtItsAttackerAndHurtsThem(t *testing.T) {
	t.Parallel()
	const name = "Spitter"
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), name)
	c.Settle()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition(name)
	id := srv.SpawnMobSeeded("minecraft:llama", pos.X+4, pos.Y, pos.Z, 12)
	servertest.RunTicks(t, srv, 1)
	c.Attack(id)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the llama targets its attacker", func() bool {
		return srv.MobTargetID(id) != 0
	})
	spat := false
	servertest.UntilTicks(t, srv, 200, "a llama_spit flies", func() bool {
		for _, it := range srv.ThrownInDimension("minecraft:overworld") {
			if it.Kind == "minecraft:llama_spit" {
				spat = true
			}
		}
		return spat
	})
	servertest.UntilTicks(t, srv, 100, "the spit hurts the player", func() bool {
		return srv.PlayerHealth(name) < 20
	})
	if got := srv.PlayerHealth(name); got > 19 || got < 17 {
		t.Errorf("player health %v after the spit, want 20 less its damage of 1 to 3", got)
	}
	servertest.UntilTicks(t, srv, 40, "the llama drops its target", func() bool {
		return srv.MobTargetID(id) == 0
	})
}
