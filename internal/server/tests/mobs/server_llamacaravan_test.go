package mobs

import (
	"testing"

	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
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

// TestALlamaIgnoresAScaledLeadFiledOutsideItsSearchSections: a scale-12 lead
// on a lead whose box reaches the follower's search window is joined only
// when its position is filed in a section that window touches.
// vanilla: LlamaFollowCaravanGoal.canUse, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestALlamaIgnoresAScaledLeadFiledOutsideItsSearchSections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		x     float64
		moves bool
	}{{"outside", -13.5, false}, {"inside", -9.5, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
			c.MoveToGround(4.5, 64, 5.5, true)
			c.Settle()
			srv.StopTick()

			scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(12)}}}}
			lead := srv.SpawnMobTagged("minecraft:llama", 0.5, 64, 5.5, scale)
			if lead == 0 {
				t.Fatal("the lead was refused")
			}
			srv.SetMobLeashHolder(lead, c.PlayerEntityID)
			follower := srv.SpawnMob("minecraft:llama", tc.x, 64, 5.5)
			c.Settle()
			x0, _, _, _ := srv.MobPos(follower)
			srv.TickMobs(40)
			x1, _, _, _ := srv.MobPos(follower)
			if moved := x1 - x0; tc.moves && moved < 1 || !tc.moves && moved > 1 {
				t.Errorf("the follower moved %f in x over 40 ticks, moves=%v", moved, tc.moves)
			}
		})
	}
}

// TestALlamaReachesAScaledLeadAmongAHerdOfLeashedLlamas: with 64 other
// leashed llamas the search runs through the grid, which still finds a
// scale-16 lead whose box overlaps the window and whose cell is one beyond
// the ordinary llama's reach.
// vanilla: LlamaFollowCaravanGoal.canUse, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestALlamaReachesAScaledLeadAmongAHerdOfLeashedLlamas(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(14.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(16)}}}}
	// The lead's cell is one past the follower's reach by the unscaled width;
	// the fillers and the holder stand outside the follower's window.
	lead := srv.SpawnMobTagged("minecraft:llama", 6.0, 64, 5.5, scale)
	if lead == 0 {
		t.Fatal("the lead was refused")
	}
	srv.SetMobLeashHolder(lead, c.PlayerEntityID)
	for i := range 64 {
		filler := srv.SpawnMob("minecraft:llama", 15.5+float64(i%8), 64, 1.5+float64(i/8))
		srv.SetMobLeashHolder(filler, c.PlayerEntityID)
	}
	follower := srv.SpawnMob("minecraft:llama", -9.5, 64, 5.5)
	c.Settle()
	servertest.HoldTicks(t, srv)
	x0, _, _, ok0 := srv.MobPos(follower)
	servertest.RunTicks(t, srv, 10)
	x1, _, _, ok1 := srv.MobPos(follower)
	if !ok0 || !ok1 {
		t.Fatal("the follower is gone from the world")
	}
	if x1-x0 < 1 {
		t.Errorf("the follower moved %f in x over 10 ticks, want it to approach the lead", x1-x0)
	}
}

// TestACowAcrossTheSectionBoundaryFromAScaledCowIsNotCrammed: with a limit of
// one, an ordinary cow overlapping a scale-16 cow takes cramming damage only
// when the giant is filed in a section its box query reaches.
// vanilla: LivingEntity.pushEntities, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestACowAcrossTheSectionBoundaryFromAScaledCowIsNotCrammed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		x    float64
		hurt bool
	}{{"outside", 18, false}, {"inside", 14, true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
			c.MoveToGround(11.5, 64, 20.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.SetGameRuleValue("max_entity_cramming", 1)
			scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(16)}}}}
			cow := srv.SpawnMob("minecraft:cow", 11.5, 64, 0.5)
			giant := srv.SpawnMobTagged("minecraft:cow", tc.x, 64, 0.5, scale)
			if giant == 0 || !srv.SetMobNoAI(cow) || !srv.SetMobNoAI(giant) || !srv.SetMobInvulnerable(giant) {
				t.Fatal("the cows could not be set up")
			}
			before, ok := srv.MobHealth(cow)
			if !ok {
				t.Fatal("the cow is gone")
			}
			servertest.RunTicks(t, srv, 10)
			after, ok := srv.MobHealth(cow)
			if !ok {
				t.Fatal("the cow is gone")
			}
			if hurt := after < before; hurt != tc.hurt {
				t.Errorf("giant at x=%v: health %v -> %v, want hurt=%v", tc.x, before, after, tc.hurt)
			}
		})
	}
}
