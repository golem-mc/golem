package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The growing up of a body whose box changes size. Vanilla's
// Entity.refreshDimensions does not just enlarge the box where the body
// stands: a grown box that would cross a block is shifted to the nearest
// spot its new size stands clear of, so a baby pressed against a fence or a
// wall grows up on its own side of it and not inside it.

// TestABabyChickenGrowsUpBesideAFence: a chick standing flush against a
// fence post grows up and is moved back off the post, rather than left
// overlapping it — where the next move would carry it through.
func TestABabyChickenGrowsUpBesideAFence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
	srv.StopTick()

	// The post of a lone oak fence runs x 0.375..0.625 of its cell
	// (FenceBlock's 6..10 sixteenths), so at cell x=1 its near face is
	// 1.375. A chick 0.3 wide pressed against it stands just clear at
	// 1.22; the grown 0.4 box reaches 1.42, past the face.
	srv.SetBlock(1, 64, 0, servertest.MustState(t, "minecraft:oak_fence", nil))
	c.ChatCommand(`summon minecraft:chicken 1.22 64 0.5 {Age:-1,NoAI:1b}`)
	chick := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if !chick.Baby {
		t.Fatal("the summoned chick is not a baby, want the age read to make it one")
	}
	if srv.MobBoxBlocked(chick.EntityID) {
		t.Fatal("the baby chick's box already crosses the fence, want it clear")
	}

	// The age -1 crosses nought on the next mob tick, the growing.
	srv.TickMobs(1)

	m, ok := mobWithID(srv, chick.EntityID)
	if !ok {
		t.Fatal("the chick is not in the world any more")
	}
	if m.Baby {
		t.Fatalf("the chick's age = %d, want it grown", m.Age)
	}
	if srv.MobBoxBlocked(chick.EntityID) {
		t.Errorf("the grown chicken's box crosses a block at x=%v", m.X)
	}
	// Its own side of the fence: the grown half-width 0.2 stays clear of
	// the post's near face at 1.375.
	if m.X+0.2 > 1.375+1e-6 {
		t.Errorf("the grown chicken reaches x=%v, past the fence's near face at 1.375", m.X+0.2)
	}
}

// TestMergingTheGrownAgeMovesTheMob: `/data merge … {Age:0}` on a chick
// pressed against a fence grows it up and moves it clear, the box refresh
// AgeableMob.setAge runs where the age crosses the grown line — the same
// move a feeding or a tick would make, without either.
func TestMergingTheGrownAgeMovesTheMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
	srv.StopTick()

	srv.SetBlock(1, 64, 0, servertest.MustState(t, "minecraft:oak_fence", nil))
	c.ChatCommand(`summon minecraft:chicken 1.22 64 0.5 {Age:-1,NoAI:1b}`)
	chick := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if !chick.Baby {
		t.Fatal("the summoned chick is not a baby, want the age read to make it one")
	}
	if srv.MobBoxBlocked(chick.EntityID) {
		t.Fatal("the baby chick's box already crosses the fence, want it clear")
	}
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:chicken", "entity.minecraft.chicken", chick.Uuid))

	c.ChatCommand("data merge entity @e[type=minecraft:chicken,limit=1] {Age:0}")
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:chicken", "entity.minecraft.chicken", chick.Uuid))

	m, ok := mobWithID(srv, chick.EntityID)
	if !ok {
		t.Fatal("the chick is not in the world any more")
	}
	if m.Baby {
		t.Fatalf("the chick's age = %d, want it grown", m.Age)
	}
	if srv.MobBoxBlocked(chick.EntityID) {
		t.Errorf("the grown chicken's box crosses a block at x=%v", m.X)
	}
}
