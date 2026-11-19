package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
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

// TestABabyChickenGrowsUpInAOneWideGap: in a one-block slot between two
// walls, a chick pressed against one wall is nudged off it when it grows
// and stays inside the slot rather than being left in the wall.
func TestABabyChickenGrowsUpInAOneWideGap(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
	srv.StopTick()

	// The free cell is x=1, its walls' faces at 1.0 and 2.0. A chick 0.3
	// wide pressed against the near wall stands just clear at 1.16; the
	// grown 0.4 box reaches 0.96, 0.04 into the wall.
	wall := servertest.MustState(t, "minecraft:stone", nil)
	srv.SetBlock(0, 64, 0, wall)
	srv.SetBlock(2, 64, 0, wall)
	c.ChatCommand(`summon minecraft:chicken 1.16 64 0.5 {Age:-1,NoAI:1b}`)
	chick := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if !chick.Baby {
		t.Fatal("the summoned chick is not a baby, want the age read to make it one")
	}
	if srv.MobBoxBlocked(chick.EntityID) {
		t.Fatal("the baby chick's box already crosses a wall, want it clear")
	}

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
	// The grown box (half-width 0.2) stays within the free cell's faces.
	if m.X-0.2 < 1.0-1e-6 || m.X+0.2 > 2.0+1e-6 {
		t.Errorf("the grown chicken spans %v..%v, want it inside the slot 1.0..2.0", m.X-0.2, m.X+0.2)
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

// TestAGrowingCowInACorridorTooNarrowToFitIsLeftPut: a calf in a corridor
// between two glass-pane lines, too narrow for its grown box, has no clear
// spot within half its width growth to move to, so it is left where it
// stands rather than searched past the panes.
// vanilla: Entity.fudgePositionAfterSizeChange
func TestAGrowingCowInACorridorTooNarrowToFitIsLeftPut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
	srv.StopTick()

	// Pane lines at x=0 and x=1 are 2-pixel walls at 0.4375..0.5625 and
	// 1.4375..1.5625, leaving a corridor 0.875 wide, narrower than the
	// grown cow's 0.9. The north and south arms carry each line along z.
	pane := servertest.MustState(t, "minecraft:glass_pane", map[string]string{"north": "true", "south": "true"})
	srv.SetBlock(0, 64, 0, pane)
	srv.SetBlock(1, 64, 0, pane)
	c.ChatCommand(`summon minecraft:cow 1.05 64 0.5 {Age:-1,NoAI:1b}`)
	calf := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !calf.Baby {
		t.Fatal("the summoned calf is not a baby, want the age read to make it one")
	}
	if srv.MobBoxBlocked(calf.EntityID) {
		t.Fatal("the calf's box already crosses a pane, want it clear")
	}
	before, ok := mobWithID(srv, calf.EntityID)
	if !ok {
		t.Fatal("the calf is not in the world any more")
	}

	srv.TickMobs(1)

	m, ok := mobWithID(srv, calf.EntityID)
	if !ok {
		t.Fatal("the calf is not in the world any more")
	}
	if m.Baby {
		t.Fatalf("the calf's age = %d, want it grown", m.Age)
	}
	if moved(before, m) {
		t.Errorf("the grown cow moved to (%v,%v,%v), want it left at (%v,%v,%v)",
			m.X, m.Y, m.Z, before.X, before.Y, before.Z)
	}
}

// TestAGrowingCowOutOfReachOfItsFreeSpotStaysPut: the grown box would clear
// a wall only at a centre further than half the width growth from where the
// calf stands, so that spot lies outside the searched region and the calf is
// left where it is rather than moved onto the far side of the wall.
func TestAGrowingCowOutOfReachOfItsFreeSpotStaysPut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
	srv.StopTick()

	// A wall face at x=1.0. The grown 0.9 box needs its centre at or
	// before 0.55; a calf at 0.78 may drift only to 0.555, short of it.
	srv.SetBlock(1, 64, 0, servertest.MustState(t, "minecraft:stone", nil))
	c.ChatCommand(`summon minecraft:cow 0.78 64 0.5 {Age:-1,NoAI:1b}`)
	calf := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !calf.Baby {
		t.Fatal("the summoned calf is not a baby, want the age read to make it one")
	}
	before, ok := mobWithID(srv, calf.EntityID)
	if !ok {
		t.Fatal("the calf is not in the world any more")
	}

	srv.TickMobs(1)

	m, ok := mobWithID(srv, calf.EntityID)
	if !ok {
		t.Fatal("the calf is not in the world any more")
	}
	if m.Baby {
		t.Fatalf("the calf's age = %d, want it grown", m.Age)
	}
	if moved(before, m) {
		t.Errorf("the grown cow moved to (%v,%v,%v), want it left at (%v,%v,%v)",
			m.X, m.Y, m.Z, before.X, before.Y, before.Z)
	}
}

// moved says whether a body left where it stood: any axis past a hair.
func moved(before, after server.TestMob) bool {
	return math.Abs(after.X-before.X) > 1e-6 ||
		math.Abs(after.Y-before.Y) > 1e-6 ||
		math.Abs(after.Z-before.Z) > 1e-6
}

// TestMergingIsBabyOffGrowsAZombieUp: merging IsBaby:0b onto a baby zombie or
// husk makes it an adult, and watchers are told the baby flag is off; a merge
// that does not name IsBaby leaves it a baby.
// vanilla: Zombie.readAdditionalSaveData
func TestMergingIsBabyOffGrowsAZombieUp(t *testing.T) {
	t.Parallel()
	huskID, ok := registry.EntityTypeID("minecraft:husk")
	if !ok {
		t.Fatal("no husk entity type")
	}
	for _, tc := range []struct {
		kind string
		typ  int32
	}{
		{"zombie", protocol.EntityTypeZombie},
		{"husk", huskID},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "creative", "Grower", "", 0, servertest.OpsListing(t, "Grower"))
			srv.SetDayTime(18000)
			servertest.HoldTicks(t, srv)

			c.ChatCommand("summon minecraft:" + tc.kind + " 3.5 64 0.5 {}")
			mob := c.ExpectMobSpawn(tc.typ)
			c.ChatCommand("data merge entity @e[type=minecraft:" + tc.kind + ",limit=1] {IsBaby:1b}")
			drainMobMetadata(c, t, &mob)
			if !mob.Baby {
				t.Fatal("IsBaby:1b did not make the zombie a baby")
			}

			c.ChatCommand("data merge entity @e[type=minecraft:" + tc.kind + ",limit=1] {CustomNameVisible:0b}")
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketAbout(mob.EntityID, protocol.IDPlayEntityMetadata))
			if m, _ := mobWithID(srv, mob.EntityID); !m.Baby {
				t.Fatal("a merge that does not name IsBaby grew the zombie up")
			}

			c.ChatCommand("data merge entity @e[type=minecraft:" + tc.kind + ",limit=1] {IsBaby:0b}")
			drainMobMetadata(c, t, &mob)
			if mob.Baby {
				t.Error("IsBaby:0b did not tell the client the zombie grew up")
			}
			if m, _ := mobWithID(srv, mob.EntityID); m.Baby {
				t.Error("the zombie is still a baby after IsBaby:0b")
			}
		})
	}
}

// drainMobMetadata reads packets until the mob's next metadata update has
// been parsed into mob.
func drainMobMetadata(c *servertest.Client, t *testing.T, mob *servertest.SpawnedMob) {
	t.Helper()
	for id, data := range c.Stream() {
		if id == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == mob.EntityID {
			c.ReadMobMetadataInto(mob, data)
			return
		}
	}
	t.Fatal("no metadata update for the zombie")
}
