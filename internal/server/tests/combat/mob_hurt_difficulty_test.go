package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMobCausedHurtsScaleWithDifficulty: a hurt a non-player mob causes
// scales with difficulty. On Hard a witch's Harming I landing on the player
// takes 9, not 6; on Peaceful a fully puffed pufferfish touching the player
// is refused (no hurt, no poison), and on Easy the same sting takes 2.5.
// vanilla: Player.hurtServer, DamageSource.scalesWithDifficulty
func TestMobCausedHurtsScaleWithDifficulty(t *testing.T) {
	t.Parallel()
	const name = "Target"
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID: protocol.OfflineUUID(name).String(), Name: name, Level: 4,
	}))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.SetDifficulty(rules.DifficultyHard)
	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	srv.SetMobNoAI(witchID)
	srv.SetHealth(name, 20)
	servertest.HoldTicks(t, srv)
	srv.ThrowMobPotion("minecraft:harming", 0.5, 66, 0.5, witchID)
	servertest.UntilTicks(t, srv, potionDeadlineTicks, "the harming splash lands", func() bool {
		return srv.PlayerHealth(name) < 20
	})
	if lost := 20 - srv.PlayerHealth(name); lost != 9 {
		t.Errorf("a witch's Harming I on Hard took %v health, want 9 (6 scaled by 1.5)", lost)
	}

	// Past the hurt window before the sting.
	servertest.RunTicks(t, srv, 20)
	srv.SetDifficulty(rules.DifficultyPeaceful)
	srv.SetHealth(name, 20)
	c.ChatCommand("summon minecraft:pufferfish 0.5 64 0.5 {PuffState:2}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:pufferfish", "entity.minecraft.pufferfish")
	// The fish flops off once out of water, so the first ticks are the ones
	// it overlaps the player.
	servertest.RunTicks(t, srv, 2)
	if h := srv.PlayerHealth(name); h != 20 {
		t.Errorf("a pufferfish sting on Peaceful left health %v, want 20", h)
	}
	if _, _, ok := srv.PlayerEffect(name, "minecraft:poison"); ok {
		t.Error("a refused pufferfish sting on Peaceful poisoned the player")
	}

	// Control: a fish does sting on Easy, scaled to min(3/2+1, 3).
	srv.SetDifficulty(rules.DifficultyEasy)
	// A fresh fish on the player: the first has flopped away.
	c.ChatCommand("summon minecraft:pufferfish 0.5 64 0.5 {PuffState:2}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:pufferfish", "entity.minecraft.pufferfish")
	servertest.UntilTicks(t, srv, 2, "the pufferfish stings on Easy", func() bool {
		return srv.PlayerHealth(name) < 20
	})
	if lost := 20 - srv.PlayerHealth(name); lost != 2.5 {
		t.Errorf("a full pufferfish sting on Easy took %v health, want 2.5", lost)
	}
	if _, _, ok := srv.PlayerEffect(name, "minecraft:poison"); !ok {
		t.Error("the landed sting on Easy did not poison the player")
	}
}
