package mobs

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A hive remembers a flower for its bees, and fire beside it empties it.
// vanilla: BeehiveBlockEntity.addOccupant, releaseOccupant, setChanged

func TestAHiveWithFireBesideFreesTheBeesThatAreNotDue(t *testing.T) {
	t.Parallel()
	for _, fire := range []bool{false, true} {
		_, _, srv := beeWorld(t, rules.DifficultyNormal)
		srv.SetDayTime(6000)
		if fire {
			srv.SetBlock(6, 64, 2, fireState(t))
		}
		nestAt(t, srv, "0", dueBee(false), storedBee(false))
		servertest.RunTicks(t, srv, 3)
		want := 1
		if fire {
			want = 2
		}
		if got := len(srv.MobIDsOfKind(bee)); got != want || storedCount(srv) != 2-want {
			t.Fatalf("fire=%v: %d bees out, %d stored, want %d out", fire, got, storedCount(srv), want)
		}
	}
}

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

func TestAHiveKeepsItsFlowerFromTheBeeItReleasesOnARollOverNine(t *testing.T) {
	t.Parallel()
	if handedFlower(t, 1) {
		t.Fatal("a roll of 0.9 or more handed the flower over")
	}
}

func TestStoredBeesKeepTheirClocksWhenAnotherBeeLeaves(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0", storedBee(false), dueBee(false))
	servertest.RunTicks(t, srv, 3)
	if got := len(srv.MobIDsOfKind(bee)); got != 1 {
		t.Fatalf("%d bees out, want 1", got)
	}
	// The kept bee started at 5 and has lived 3 ticks of the hive.
	if got := storedTicks(srv); got != 8 {
		t.Fatalf("the bee that stayed has %d ticks in the hive, want 8", got)
	}
}

func TestABeeStoredInAHiveBesideFireIsLetOutAgain(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	srv.SetBlock(6, 64, 2, fireState(t))
	nestAt(t, srv, "0", storedBee(false))
	srv.StoreHiveBee(5, 64, 2, nbt.Compound{"id": nbt.String(bee)})
	servertest.RunTicks(t, srv, 3)
	if storedCount(srv) != 0 || len(srv.MobIDsOfKind(bee)) != 2 {
		t.Fatalf("%d stored, %d out; fire should have freed both", storedCount(srv), len(srv.MobIDsOfKind(bee)))
	}
}

func TestABeeLeavesItsFlowerWithTheHiveItEnters(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0")
	srv.SetBlock(7, 64, 3, blockNamed(t, "minecraft:poppy"))
	c.ChatCommand("summon minecraft:bee 5.5 65 4.5 {HasNectar:1b,hive_pos:[I;5,64,2],flower_pos:[I;7,64,3]}")
	c.Settle()
	for range 40 {
		servertest.RunTicks(t, srv, 10)
		if storedCount(srv) > 0 {
			break
		}
	}
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	if !reflect.DeepEqual(tag["flower_pos"], nbt.IntArray{7, 64, 3}) {
		t.Fatalf("the hive remembers %v, want [7 64 3]", tag["flower_pos"])
	}
}

func TestAnAngryBeeWithNoTargetStaysOutOfItsHive(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0")
	c.ChatCommand("summon minecraft:bee 5.5 65 4.5 {HasNectar:1b,hive_pos:[I;5,64,2],anger_end_time:100000L}")
	c.Settle()
	servertest.RunTicks(t, srv, 200)
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 1 || srv.MobAngerTicks(ids[0]) == 0 {
		t.Fatalf("want one angry bee outside, got %d bees", len(ids))
	}
	if n := storedCount(srv); n != 0 {
		t.Fatalf("%d bees entered the nest while angry, want 0", n)
	}
}

func TestFadingRainKeepsStoredBeesIn(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	srv.SetWeatherState(true, false, 6000, 6000, 0)
	// The rain level is a few hundredths: not raining, but not zero.
	servertest.RunTicks(t, srv, 4)
	if srv.IsRaining() {
		t.Fatal("the rain has already passed 0.2")
	}
	nestAt(t, srv, "0", dueBee(false))
	servertest.RunTicks(t, srv, 3)
	if len(srv.MobIDsOfKind(bee)) != 0 {
		t.Fatal("a bee left the hive while the rain level was above 0")
	}
}

func fireState(t *testing.T) uint16 {
	t.Helper()
	id, ok := registry.FindState("minecraft:fire", map[string]string{"age": "0", "east": "false", "north": "false", "south": "false", "up": "false", "west": "false"})
	if !ok {
		t.Fatal("no fire state")
	}
	return id
}

// A fire block set beside a hive frees its bees at once, due or not.
func TestFireSetBesideAHiveFreesItsBees(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0", storedBee(false), storedBee(false))
	srv.SetBlockAndTell(6, 64, 2, fireState(t))
	servertest.RunTicks(t, srv, 1)
	if got := len(srv.MobIDsOfKind(bee)); got != 2 || storedCount(srv) != 0 {
		t.Fatalf("%d bees out, %d stored, want 2 out", got, storedCount(srv))
	}
}

// A blast breaking a hive frees its bees whatever the time.
func TestABlastBreakingAHiveFreesItsBees(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0", storedBee(false), storedBee(false))
	srv.RunExplosion(5.5, 64.5, 2.5, 4)
	servertest.RunTicks(t, srv, 1)
	if got := len(srv.MobIDsOfKind(bee)); got != 2 {
		t.Fatalf("%d bees out, want 2", got)
	}
}
