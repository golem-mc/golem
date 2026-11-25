package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

// vanilla: LlamaFollowCaravanGoal.tick, PathNavigation.createPath
func TestALlamaDescendsStepsToJoinItsCaravan(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder",
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)

	// The high platform and the lower floor straddle a section boundary.
	stone := registry.BlockStone.DefaultState()
	for x := int32(0); x <= 15; x++ {
		for z := int32(12); z <= 20; z++ {
			if !srv.SetBlock(x, 149-max(0, min(x-2, 7)), z, stone) {
				t.Fatalf("could not build step at (%d, %d)", x, z)
			}
		}
	}
	srv.SetBlock(11, 143, 16, registry.BlockOakFence.DefaultState())
	c.MoveToGround(11.5, 143, 18.5, true)
	c.Settle()

	giveLead(t, srv, "Herder", 1)
	lead := srv.SpawnMobTagged("minecraft:llama", 11.5, 143, 15.5, nbt.Compound{
		"NoAI": nbt.Byte(1),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(4)},
		}},
	})
	if lead == 0 {
		t.Fatal("the lead llama was refused")
	}
	c.Settle()
	c.UseOn(lead)
	if attached, holder := c.ExpectLinkPacket(); attached != lead || holder != c.PlayerEntityID {
		t.Fatalf("link = (%d, %d), want the lead llama tied to the player", attached, holder)
	}
	c.UseBlock(11, 143, 16)
	knot := c.ExpectKnotSpawn()
	if attached, holder := c.ExpectLinkPacket(); attached != lead || holder != knot.EntityID {
		t.Fatalf("link = (%d, %d), want the lead llama tied to the fence", attached, holder)
	}

	follower := srv.SpawnMob("minecraft:llama", 2.5, 150, 15.5)
	servertest.RunTicks(t, srv, 40)
	x, y, z, ok := srv.MobPos(follower)
	// Vanilla reaches the middle steps in 40 ticks on this fixture.
	const wantX, wantY, wantZ = 7.00, 146.00, 15.51
	if !ok || servertest.Abs64(x-wantX) > 1 || servertest.Abs64(y-wantY) > 1 || servertest.Abs64(z-wantZ) > 1 {
		t.Fatalf("follower after 40 ticks = (%.2f, %.2f, %.2f), want it partway down the steps near (%.2f, %.2f, %.2f)",
			x, y, z, wantX, wantY, wantZ)
	}
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
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
			c.MoveToGround(4.5, 64, 5.5, true)
			c.Settle()
			srv.StopTick()

			scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(12)}}}}
			lead := srv.SpawnMobTagged("minecraft:llama", tc.leadX, 64, 5.5, scale)
			if lead == 0 {
				t.Fatal("the lead was refused")
			}
			srv.SetMobLeashHolder(lead, c.PlayerEntityID)
			// Llamas tied to a knot past the search window keep the list long
			// enough for the grid to cut it.
			knot := srv.SpawnKnotEntity(6, 64, 37)
			for i := range tc.extra {
				far := srv.SpawnMob("minecraft:llama", 0.5+float64(i%8)*1.5, 64, 32.5+float64(i/8)*1.5)
				if far == 0 {
					t.Fatal("a distant llama was refused")
				}
				srv.SetMobLeashHolder(far, knot.ID)
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

// TestSummonedLlamaCaravanApproachMatchesVanilla: a freshly summoned llama
// starts off the ground (vanilla Entity.load) and approaches a stationary
// scale-12 leashed lead among 64 other leashed llamas at vanilla's exact
// tick-by-tick pace, colliding with the scaled lead's box at the same tick.
// vanilla: SummonCommand.createEntity, Entity.load, LlamaFollowCaravanGoal.tick
func TestSummonedLlamaCaravanApproachMatchesVanilla(t *testing.T) {
	t.Parallel()
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder",
				func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
			servertest.HoldTicks(t, srv)
			c.MoveToGround(4.5, 64, 10.5, true)
			c.Settle()

			scale := nbt.Compound{
				"NoAI": nbt.Byte(1),
				"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(12)},
				}},
			}
			lead := srv.SpawnMobTagged("minecraft:llama", 4.1, 64, 5.5, scale)
			if lead == 0 {
				t.Fatal("the lead was refused")
			}
			srv.SetMobLeashHolder(lead, c.PlayerEntityID)
			for i := range 64 {
				filler := srv.SpawnMob("minecraft:llama", 15.5+float64(i%8), 64, 1.5+float64(i/8))
				srv.SetMobNoAI(filler)
				srv.SetMobLeashHolder(filler, c.PlayerEntityID)
			}

			if tagged {
				srv.RunLine(`summon minecraft:llama -10.1 64 5.5 {Tags:["fol"]}`)
			} else {
				srv.RunLine(`summon minecraft:llama -10.1 64 5.5`)
			}
			ids := srv.MobIDsOfKind("minecraft:llama")
			follower := ids[len(ids)-1]
			if srv.MobOnGround(follower) {
				t.Fatalf("summoned follower started on ground, want false before first tick")
			}

			// Tick 1 lands the llama (OnGround becomes true, no path yet).
			// Tick 2 starts walking if (2+id)%2 == 0, else tick 3 does.
			want10, want40 := -10.1+2.0249, -10.1+9.4472
			if (2+follower)%2 == 0 {
				want10, want40 = -10.1+2.3211, -10.1+9.5254
			}
			servertest.RunTicks(t, srv, 10)
			x10, _, _, ok := srv.MobPos(follower)
			if !ok {
				t.Fatal("follower gone after 10 ticks")
			}
			if servertest.Abs64(x10-want10) > 0.01 {
				t.Errorf("after 10 ticks: x = %.4f, want %.4f (id=%d)", x10, want10, follower)
			}
			servertest.RunTicks(t, srv, 30)
			x40, _, _, ok := srv.MobPos(follower)
			if !ok {
				t.Fatal("follower gone after 40 ticks")
			}
			if servertest.Abs64(x40-want40) > 0.01 {
				t.Errorf("after 40 ticks: x = %.4f, want %.4f (id=%d)", x40, want40, follower)
			}
		})
	}
}

// TestAScaledLlamaIsCrammedByACowThatDoesNotQueryItsSection: with a limit of
// one, a scale-16 llama whose box reaches a mooshroom takes cramming damage
// while the mooshroom, whose padded query window misses the llama's section,
// does not.
// The fixture is 144 blocks (nine sections, thirty-six grid cells) closer to
// the spawn than where it was observed, so the section and cell offsets match.
// vanilla: LivingEntity.pushEntities, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAScaledLlamaIsCrammedByACowThatDoesNotQueryItsSection(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Herder")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetGameRuleValue("max_entity_cramming", 1)
	scale := nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(16)}}}}
	llama := srv.SpawnMobTagged("minecraft:llama", 20.1, 64, 15.5, scale)
	cow := srv.SpawnMob("minecraft:mooshroom", 13.25, 64, 15.5)
	if llama == 0 || cow == 0 || !srv.SetMobNoAI(llama) || !srv.SetMobNoAI(cow) {
		t.Fatal("the mobs could not be set up")
	}
	llamaBefore, _ := srv.MobHealth(llama)
	cowBefore, _ := srv.MobHealth(cow)
	// The llama dies within 100 ticks, so read both after 20.
	servertest.RunTicks(t, srv, 20)
	llamaAfter, ok1 := srv.MobHealth(llama)
	cowAfter, ok2 := srv.MobHealth(cow)
	if !ok1 || !ok2 {
		t.Fatalf("a mob is gone: llama %v cow %v", ok1, ok2)
	}
	if llamaAfter >= llamaBefore {
		t.Errorf("llama health %v -> %v, want cramming damage", llamaBefore, llamaAfter)
	}
	if cowAfter != cowBefore {
		t.Errorf("mooshroom health %v -> %v, want unchanged", cowBefore, cowAfter)
	}
}
