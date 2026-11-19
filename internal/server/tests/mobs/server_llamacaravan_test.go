package mobs

import (
	"testing"

	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// llamaDistance is the horizontal distance between two mobs.
func llamaDistance(t *testing.T, srv *server.Server, a, b int32) float64 {
	t.Helper()
	ax, _, az, okA := srv.MobPos(a)
	bx, _, bz, okB := srv.MobPos(b)
	if !okA || !okB {
		t.Fatal("a llama is gone from the world")
	}
	return servertest.Abs64(ax-bx) + servertest.Abs64(az-bz)
}

// TestALlamaFollowsALeashedLlamaUntilTheLeadGoes: a llama beside a leashed
// llama walks up to it, and lets go when the lead comes off.
// vanilla: LlamaFollowCaravanGoal.canUse, canContinueToUse
func TestALlamaFollowsALeashedLlamaUntilTheLeadGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	lead := srv.SpawnMob("minecraft:llama", 5.5, 64, 5.5)
	follower := srv.SpawnMob("minecraft:llama", 5.5, 64, 11.5)
	c.Settle()
	c.UseOn(lead)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	before := llamaDistance(t, srv, lead, follower)
	srv.TickMobs(40)
	if after := llamaDistance(t, srv, lead, follower); after >= before-1 {
		t.Errorf("the llama is %f from the leashed one after 40 ticks, want it closer than the %f it started", after, before)
	}

	c.UseOn(lead)
	c.ExpectLeashDrop()
	srv.TickMobs(5)
	x, _, z, _ := srv.MobPos(follower)
	srv.TickMobs(5)
	x2, _, z2, _ := srv.MobPos(follower)
	if servertest.Abs64(x2-x)+servertest.Abs64(z2-z) > 1 {
		t.Errorf("the llama kept walking (%f, %f) to (%f, %f) after the lead came off", x, z, x2, z2)
	}
}

// TestACaravanLlamaHitByAPlayerStaysInLine: the caravan goal (2) outranks
// the spit and the panic (3), so a struck follower neither spits nor leaves
// the llama it follows.
// vanilla: Llama.registerGoals, WrappedGoal.canBeReplacedBy
func TestACaravanLlamaHitByAPlayerStaysInLine(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Drover")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Drover", 1)
	lead := srv.SpawnMob("minecraft:llama", 5.5, 64, 5.5)
	follower := srv.SpawnMob("minecraft:llama", 5.5, 64, 11.5)
	c.Settle()
	c.UseOn(lead)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	srv.TickMobs(60)
	if d := llamaDistance(t, srv, lead, follower); d > 4 {
		t.Fatalf("the follower is %f from the lead after 60 ticks, want it in the caravan", d)
	}

	x, _, z, _ := srv.MobPos(follower)
	// From behind, so the knockback pushes the follower toward the lead.
	c.MoveToGround(x, 64, z+1.5, true)
	c.Settle()
	c.Attack(follower)
	c.Settle()
	for tick := range 80 {
		srv.TickMobs(1)
		if tick == 5 && srv.MobTargetID(follower) == 0 {
			t.Fatal("the struck follower has no target, want its attacker")
		}
		for _, it := range srv.ThrownInDimension("minecraft:overworld") {
			if it.Kind == "minecraft:llama_spit" {
				t.Fatalf("a llama_spit flies %d ticks after the hit, want the caravan llama to stay in line", tick+1)
			}
		}
		if d := llamaDistance(t, srv, lead, follower); d > 4 {
			t.Fatalf("the follower is %f from the lead %d ticks after the hit, want it within 4", d, tick+1)
		}
	}
}
