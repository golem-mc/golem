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
		leadX float64
		moves bool
		extra int
	}{
		{"outside", -13.5, 0.5, false, 0},
		{"inside", -9.5, 0.5, true, 0},
		// Four grid cells apart, past the unscaled push radius.
		{"inside with a long list", -10.1, 4.1, true, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.MobTestConfig())
			c := connect()
			c.Join("Herder")
			c.MoveToGround(4.5, 64, 5.5, true)
			c.Settle()
			var keeper *servertest.Client
			if tc.extra > 0 {
				// A second player in range of the distant llamas, so
				// their leashes hold.
				keeper = connect()
				keeper.Join("Keeper")
				keeper.MoveToGround(6.5, 64, 36.5, true)
				keeper.Settle()
			}
			srv.StopTick()

			scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(12)}}}}
			lead := srv.SpawnMobTagged("minecraft:llama", tc.leadX, 64, 5.5, scale)
			if lead == 0 {
				t.Fatal("the lead was refused")
			}
			srv.SetMobLeashHolder(lead, c.PlayerEntityID)
			// Leashed llamas past the search window keep the list long
			// enough for the grid to cut it.
			for i := range tc.extra {
				far := srv.SpawnMob("minecraft:llama", 0.5+float64(i%8)*1.5, 64, 32.5+float64(i/8)*1.5)
				if far == 0 {
					t.Fatal("a distant llama was refused")
				}
				srv.SetMobLeashHolder(far, keeper.PlayerEntityID)
			}
			follower := srv.SpawnMob("minecraft:llama", tc.x, 64, 5.5)
			c.Settle()
			x0, _, _, _ := srv.MobPos(follower)
			x1 := x0
			for range 40 {
				srv.TickMobs(1)
				x1, _, _, _ = srv.MobPos(follower)
				if tc.moves && x1-x0 >= 1 {
					break
				}
			}
			if moved := x1 - x0; tc.moves && moved < 1 || !tc.moves && moved > 1 {
				t.Errorf("the follower moved %f in x over 40 ticks, moves=%v", moved, tc.moves)
			}
		})
	}
}
