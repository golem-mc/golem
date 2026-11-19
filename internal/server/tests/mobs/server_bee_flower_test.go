package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// A hive remembers a flower for its bees, and fire beside it empties it.
// vanilla: BeehiveBlockEntity.addOccupant, releaseOccupant, setChanged

// handedFlower releases one bee from a hive that remembers a flower, on a
// level dice seeded with seed, and reports whether the bee got the flower.
func handedFlower(t *testing.T, seed int64) bool {
	t.Helper()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	srv.SetLevelSeed(seed)
	srv.SetBlock(5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]"))
	srv.SetBlockEntityTagAt(5, 64, 2, "minecraft:beehive", nbt.Compound{
		"flower_pos": nbt.IntArray{7, 64, 3},
		"bees":       nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{dueBee(false)}},
	})
	servertest.RunTicks(t, srv, 3)
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 1 {
		t.Fatalf("%d bees out, want 1", len(ids))
	}
	mem, _ := srv.MobBeeMemory(ids[0])
	if mem.Flower != nil && *mem.Flower != [3]int32{7, 64, 3} {
		t.Fatalf("flower %v", *mem.Flower)
	}
	return mem.Flower != nil
}

// The hand-off is a nine in ten roll of the level dice; the seeds pin one
// roll under it and one over it.
func TestAHiveHandsItsFlowerToTheBeeItReleasesOnARollUnderNine(t *testing.T) {
	t.Parallel()
	if !handedFlower(t, 2) {
		t.Fatal("a roll under 0.9 left the bee without the flower")
	}
}
