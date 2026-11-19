package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const (
	silverfishKind = "minecraft:silverfish"
	// spawnAnimStatus is the entity event of Mob.spawnAnim.
	spawnAnimStatus = 20
)

// silverfishTests joins a player in the mode; a stepped scene stops the
// loop, which the digs cannot: their acknowledgement goes out at a tick's end.
func silverfishTests(t *testing.T, mode string, stepped bool) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = mode
	srv, c := servertest.Join(t, cfg, "Miner")
	servertest.GroundThePlayer(c)
	if stepped {
		srv.StopTick()
	}
	srv.SetDifficulty(2)
	return srv, c
}

func silverfishes(srv *server.Server) []server.TestMob {
	var found []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == silverfishKind {
			found = append(found, m)
		}
	}
	return found
}

func blockNamed(t *testing.T, name string) uint16 {
	t.Helper()
	id, ok := registry.BlockStates[name]
	if !ok {
		t.Fatalf("no block %s", name)
	}
	return id
}

func stateName(srv *server.Server, x, y, z int32) string {
	name, _ := servertest.StateProps(srv.BlockAt(x, y, z))
	return name
}

func silverfishTypeID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID(silverfishKind)
	if !ok {
		t.Fatal("no silverfish entity type")
	}
	return id
}

// TestBreakingInfestedStoneReleasesASilverfishAndDropsNothing breaks the
// block bare-handed: one silverfish stands in the cell with the poof, the
// stone is not dropped.
// vanilla: InfestedBlock.spawnAfterBreak
func TestBreakingInfestedStoneReleasesASilverfishAndDropsNothing(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", false)
	srv.SetBlock(3, 64, 3, blockNamed(t, "minecraft:infested_stone"))

	c.BreakBlock(3, 64, 3)
	c.WaitBlockChanged()

	spawned := c.ExpectMobSpawn(silverfishTypeID(t))
	c.AwaitEntityEvent(spawned.EntityID, spawnAnimStatus)
	found := silverfishes(srv)
	if len(found) != 1 {
		t.Fatalf("%d silverfish after the break, want 1", len(found))
	}
	if spawned.X != 3.5 || spawned.Y != 64 || spawned.Z != 3.5 {
		t.Errorf("the silverfish appeared at %v,%v,%v, want 3.5,64,3.5", spawned.X, spawned.Y, spawned.Z)
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Errorf("the break dropped %d items, want none", len(items))
	}
}

// infestedRow lays count infested stones along x from x0 at y 64, z 3.
func infestedRow(t *testing.T, srv *server.Server, x0, count int32) {
	t.Helper()
	for x := x0; x < x0+count; x++ {
		srv.SetBlock(x, 64, 3, blockNamed(t, "minecraft:infested_stone"))
	}
}

// infestedLeft counts the infested stones of the row.
func infestedLeft(srv *server.Server, x0, count int32) int32 {
	var n int32
	for x := x0; x < x0+count; x++ {
		if stateName(srv, x, 64, 3) == "minecraft:infested_stone" {
			n++
		}
	}
	return n
}

// TestAHurtSilverfishCallsTheInfestedBlocksOut: a magic hurt, twenty ticks
// later, breaks infested blocks around it, each break letting another
// silverfish out.
// vanilla: Silverfish.SilverfishWakeUpFriendsGoal
func TestAHurtSilverfishCallsTheInfestedBlocksOut(t *testing.T) {
	t.Parallel()
	srv, _ := silverfishTests(t, "creative", true)
	infestedRow(t, srv, 4, 6)
	id := srv.SpawnMob(silverfishKind, 3.5, 64, 3.5)

	srv.HurtMobByWorld(id, 1, "minecraft:magic")
	srv.TickMobs(8)
	if left := infestedLeft(srv, 4, 6); left != 6 {
		t.Fatalf("%d of 6 infested blocks left after 8 ticks, want all: the call waits twenty", left)
	}
	srv.TickMobs(16)

	left := infestedLeft(srv, 4, 6)
	if left == 6 {
		t.Fatal("no infested block broke after the call")
	}
	if got, want := int32(len(silverfishes(srv))), 1+(6-left); got != want {
		t.Errorf("%d silverfish after %d blocks broke, want %d", got, 6-left, want)
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Errorf("the woken blocks dropped %d items, want none", len(items))
	}
}

// TestASilverfishEntersAdjacentStone: closed into a cavity of stone, an
// idle silverfish eventually merges into a wall: the block turns infested,
// the poof is told and the mob is gone.
// vanilla: Silverfish.SilverfishMergeWithStoneGoal
func TestASilverfishEntersAdjacentStone(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "creative", true)
	stone := blockNamed(t, "minecraft:stone")
	for x := int32(6); x <= 8; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(6); z <= 8; z++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	srv.SetBlock(7, 64, 7, 0)
	id := srv.SpawnMob(silverfishKind, 7.5, 64, 7.5)
	c.Settle()

	for range 40 {
		srv.TickMobs(50)
		if len(silverfishes(srv)) == 0 {
			break
		}
	}

	if n := len(silverfishes(srv)); n != 0 {
		t.Fatalf("%d silverfish after 2000 ticks closed in stone, want it merged", n)
	}
	infested := 0
	beside := map[[3]int32]bool{{6, 64, 7}: true, {8, 64, 7}: true, {7, 63, 7}: true, {7, 65, 7}: true, {7, 64, 6}: true, {7, 64, 8}: true}
	for x := int32(6); x <= 8; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(6); z <= 8; z++ {
				if stateName(srv, x, y, z) == "minecraft:infested_stone" {
					infested++
					if !beside[[3]int32{x, y, z}] {
						t.Errorf("the infested block %d,%d,%d is not beside the cavity", x, y, z)
					}
				}
			}
		}
	}
	if infested != 1 {
		t.Errorf("%d infested blocks, want 1", infested)
	}
	c.AwaitEntityEvent(id, spawnAnimStatus)
}
