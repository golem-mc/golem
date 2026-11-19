package mobs

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/server/world"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// Bees leave and enter hives on their own, and deliver nectar as honey.
// vanilla: BeehiveBlockEntity.serverTick, Bee.BeeEnterHiveGoal

// dueBee is an occupant one tick short of its release.
func dueBee(nectar bool) nbt.Compound {
	b := storedBee(nectar)
	b["ticks_in_hive"] = nbt.Int(600)
	return b
}

func nestAt(t *testing.T, srv *server.Server, level string, bees ...nbt.Compound) {
	t.Helper()
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level="+level+"]"), bees...)
}

func storedCount(srv *server.Server) int {
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	list, _ := nbt.Get[nbt.List](tag, "bees")
	return len(list.Items)
}

// storedTicks is the time the first stored bee has spent in the nest; a bee
// that left and went back in starts again from 0.
func storedTicks(srv *server.Server) int32 {
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	list, _ := nbt.Get[nbt.List](tag, "bees")
	if len(list.Items) == 0 {
		return -1
	}
	occupant, _ := list.Items[0].(nbt.Compound)
	ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
	return ticks
}

func TestAStoredBeeLeavesByDayAfterItsStay(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0", dueBee(false))
	servertest.RunTicks(t, srv, 1)
	if len(srv.MobIDsOfKind(bee)) != 0 || storedCount(srv) != 1 {
		t.Fatal("the bee left on the tick its stay ran out, not after it")
	}
	servertest.RunTicks(t, srv, 2)
	if len(srv.MobIDsOfKind(bee)) != 1 || storedCount(srv) != 0 {
		t.Fatalf("%d bees out, %d stored", len(srv.MobIDsOfKind(bee)), storedCount(srv))
	}
	if got := srv.BlockAt(5, 64, 2); got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]") {
		t.Fatal("a bee without nectar changed the honey level")
	}
}

func TestStoredBeesStayInAtNightAndWhileItRains(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	nestAt(t, srv, "0", dueBee(false))
	servertest.RunTicks(t, srv, 5)
	if len(srv.MobIDsOfKind(bee)) != 0 || storedCount(srv) != 1 {
		t.Fatal("a bee left the hive at night")
	}
	if got := storedTicks(srv); got != 605 {
		t.Fatalf("the bee has stayed %d ticks, want 605 without a trip out", got)
	}
	srv.SetDayTime(6000)
	srv.SetWeatherState(true, false, 6000, 6000, 0)
	// The rain level takes 20 ticks to climb past the 0.2 that counts.
	servertest.RunTicks(t, srv, 40)
	nestAt(t, srv, "0", dueBee(false))
	servertest.RunTicks(t, srv, 200)
	if len(srv.MobIDsOfKind(bee)) != 0 {
		t.Fatal("a bee left the hive in the rain")
	}
	if got := storedTicks(srv); got != 800 {
		t.Fatalf("the bee has stayed %d ticks, want 800 without a trip out", got)
	}
}

func TestABeeWithNectarRaisesTheHoneyLevelOnLeaving(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "2", dueBee(true))
	servertest.RunTicks(t, srv, 3)
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 1 {
		t.Fatalf("%d bees out", len(ids))
	}
	if flags, _ := srv.MobBeeFlags(ids[0]); flags&0x08 != 0 {
		t.Error("the bee kept its nectar")
	}
	got := srv.BlockAt(5, 64, 2)
	if got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=3]") &&
		got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=4]") {
		t.Fatalf("the honey level did not rise, state %d", got)
	}
}

func TestAFullHiveKeepsItsHoneyLevelWhenABeeDeliversNectar(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "5", dueBee(true))
	servertest.RunTicks(t, srv, 3)
	if len(srv.MobIDsOfKind(bee)) != 1 || srv.BlockAt(5, 64, 2) != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]") {
		t.Fatal("a full hive changed")
	}
}

func TestABlockedFrontKeepsTheTickedBeeInside(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	srv.SetBlock(5, 64, 1, blockNamed(t, "minecraft:stone"))
	nestAt(t, srv, "0", dueBee(false))
	servertest.RunTicks(t, srv, 10)
	if len(srv.MobIDsOfKind(bee)) != 0 || storedCount(srv) != 1 {
		t.Fatal("a bee left through a blocked front")
	}
}

func TestAReleasedBeeRemembersItsHiveAndFliesBackWithNectar(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0")
	c.ChatCommand("summon minecraft:bee 5.5 65 4.5 {HasNectar:1b,hive_pos:[I;5,64,2]}")
	c.Settle()
	for range 40 {
		servertest.RunTicks(t, srv, 10)
		if len(srv.MobIDsOfKind(bee)) == 0 {
			break
		}
	}
	if len(srv.MobIDsOfKind(bee)) != 0 || storedCount(srv) != 1 {
		t.Fatalf("%d bees out, %d stored", len(srv.MobIDsOfKind(bee)), storedCount(srv))
	}
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	list, _ := nbt.Get[nbt.List](tag, "bees")
	occupant, _ := list.Items[0].(nbt.Compound)
	if minTicks, _ := nbt.IntValue(occupant["min_ticks_in_hive"]); minTicks != 2400 {
		t.Errorf("a bee with nectar stays %d ticks, want 2400", minTicks)
	}
}

func TestABeeWithNectarFindsANearbyNestOnItsOwn(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0")
	c.ChatCommand("summon minecraft:bee 5.5 65 7.5 {HasNectar:1b}")
	c.Settle()
	for range 60 {
		servertest.RunTicks(t, srv, 10)
		if storedCount(srv) > 0 {
			break
		}
	}
	if storedCount(srv) != 1 {
		t.Fatal("the bee never went into the nest")
	}
}

func TestABeesHiveFlowerAndErrandsAreSavedAndLoaded(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, _ := servertest.Start(t, cfg)
	script := `summon minecraft:bee 100.5 64 0.5 {NoAI:1b,hive_pos:[I;5,64,2],flower_pos:[I;7,64,3],TicksSincePollination:77,CannotEnterHiveTicks:55,CropsGrownSincePollination:4}` + "\n"
	console.New(strings.NewReader(script), io.Discard, io.Discard, srv).Serve()
	srv.Shutdown()
	var saved nbt.Compound
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: world.ChunkOf(100.5), Z: world.ChunkOf(0.5)}) {
		saved = compound
	}
	for key, want := range map[string]nbt.Tag{
		"hive_pos": nbt.IntArray{5, 64, 2}, "flower_pos": nbt.IntArray{7, 64, 3},
		"TicksSincePollination": nbt.Int(77), "CannotEnterHiveTicks": nbt.Int(55), "CropsGrownSincePollination": nbt.Int(4),
	} {
		if !reflect.DeepEqual(saved[key], want) {
			t.Errorf("%s saved as %v, want %v", key, saved[key], want)
		}
	}
}

func TestTwoNectarBeesLeavingTogetherRaiseTheHoneyOnce(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "1", dueBee(true), dueBee(true))
	servertest.RunTicks(t, srv, 3)
	if len(srv.MobIDsOfKind(bee)) != 2 {
		t.Fatalf("%d bees out", len(srv.MobIDsOfKind(bee)))
	}
	if got := srv.BlockAt(5, 64, 2); got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=2]") {
		t.Fatalf("two deliveries on one tick left state %d, want one level more", got)
	}
}

func TestBeesArrivingTogetherFillAHiveOnlyToThree(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(6000)
	nestAt(t, srv, "0", storedBee(false), storedBee(false))
	for range 4 {
		c.ChatCommand("summon minecraft:bee 5.5 64.5 1.5 {HasNectar:1b}")
	}
	c.Settle()
	for range 30 {
		servertest.RunTicks(t, srv, 10)
		if n := storedCount(srv); n > rules.HiveMaxOccupants {
			t.Fatalf("%d bees stored, the cap is %d", n, rules.HiveMaxOccupants)
		}
	}
	if got := srv.MobCount(); got != 3 || len(srv.MobIDsOfKind(bee)) != 3 {
		t.Fatalf("%d bodies, tracked bees %v; want the three bees refused entry", got, srv.MobIDsOfKind(bee))
	}
}

// A reader on another goroutine walks the stored occupants while the tick
// advances their clocks; run with -race.
func TestHiveClocksAdvanceWhileAnotherGoroutineReadsTheOccupants(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetDayTime(18000)
	nestAt(t, srv, "0", storedBee(false), storedBee(false))
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			default:
			}
			tag, _ := srv.BlockEntityTagAt(5, 64, 2)
			list, _ := nbt.Get[nbt.List](tag, "bees")
			for _, item := range list.Items {
				occupant, _ := item.(nbt.Compound)
				nbt.IntValue(occupant["ticks_in_hive"])
			}
		}
	}()
	before := storedTicks(srv)
	servertest.RunTicks(t, srv, 50)
	close(done)
	<-finished
	if got := storedTicks(srv); got != before+50 {
		t.Fatalf("the clock went from %d to %d in 50 ticks", before, got)
	}
}
