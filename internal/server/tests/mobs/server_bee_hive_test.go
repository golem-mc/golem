package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// A broken or harvested hive frees its bees at the player.
// vanilla: BeehiveBlock.playerDestroy, BeehiveBlockEntity.emptyAllLivingFromHive

func hiveState(t *testing.T, name, props string) uint16 {
	t.Helper()
	m := map[string]string{}
	for kv := range strings.SplitSeq(strings.Trim(props, "[]"), ",") {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	id, ok := registry.FindState(name, m)
	if !ok {
		t.Fatalf("no state %s%s", name, props)
	}
	return id
}

// storedBee is one hive occupant: a bee carrying nectar or not.
func storedBee(nectar bool) nbt.Compound {
	data := nbt.Compound{"id": nbt.String(bee)}
	if nectar {
		data["HasNectar"] = nbt.Byte(1)
	}
	return nbt.Compound{"entity_data": data, "ticks_in_hive": nbt.Int(5), "min_ticks_in_hive": nbt.Int(600)}
}

// beesFreed ticks until a bee exists and returns the bees then, before any
// sting ends the anger.
func beesFreed(t *testing.T, srv *server.Server, want int) []int32 {
	t.Helper()
	for range 60 {
		servertest.RunTicks(t, srv, 1)
		if len(srv.MobIDsOfKind(bee)) >= want {
			break
		}
	}
	return srv.MobIDsOfKind(bee)
}

func putHive(srv *server.Server, x, y, z int32, state uint16, bees ...nbt.Compound) {
	srv.SetBlock(x, y, z, state)
	items := make([]nbt.Tag, len(bees))
	for i, b := range bees {
		items[i] = b
	}
	srv.SetBlockEntityTagAt(x, y, z, "minecraft:beehive", nbt.Compound{"bees": nbt.List{Elem: nbt.TypeCompound, Items: items}})
}

// vanilla: BeehiveBlockEntity.addOccupant, releaseOccupant
func TestABeeEnteringItsNestLeavesNoBodyUntilReleased(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	nestAt(t, srv, "0")
	before := srv.MobCount()
	c.ChatCommand("summon minecraft:bee 5.5 64 1.5 {hive_pos:[I;5,64,2]}")
	c.Settle()
	if got := srv.MobCount(); got != before+1 {
		t.Fatalf("%d bodies after summoning, want %d", got, before+1)
	}
	servertest.UntilTicks(t, srv, 40, "the bee entering its nest", func() bool { return storedCount(srv) == 1 })
	// MobIDsOfKind reads tracking records; MobCount also catches untracked bodies.
	for range 20 {
		if got := srv.MobCount(); got != before {
			t.Fatalf("%d bodies while the bee is stored, want %d", got, before)
		}
		if ids := srv.MobIDsOfKind(bee); len(ids) != 0 || storedCount(srv) != 1 {
			t.Fatalf("bees outside %v, stored %d; want none outside and one stored", ids, storedCount(srv))
		}
		servertest.RunTicks(t, srv, 1)
	}
	// A nectar-free occupant stays at least 600 ticks, then waits for daylight.
	servertest.RunTicks(t, srv, 600)
	if got := srv.MobCount(); got != before || storedCount(srv) != 1 {
		t.Fatalf("%d bodies, %d stored after the night; want %d bodies and one stored", got, storedCount(srv), before)
	}
	srv.SetDayTime(6000)
	servertest.UntilTicks(t, srv, 5, "the bee leaving its nest", func() bool { return storedCount(srv) == 0 })
	for range 20 {
		if got := srv.MobCount(); got != before+1 {
			t.Fatalf("%d bodies after release, want %d", got, before+1)
		}
		if ids := srv.MobIDsOfKind(bee); len(ids) != 1 || storedCount(srv) != 0 {
			t.Fatalf("bees outside %v, stored %d; want one outside and none stored", ids, storedCount(srv))
		}
		servertest.RunTicks(t, srv, 1)
	}
}

func TestABrokenNestFreesItsBeesAtThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]"), storedBee(true), storedBee(false))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 2 {
		t.Fatalf("%d bees freed, want 2", len(ids))
	}
	for _, id := range ids {
		if got := srv.MobTargetID(id); got != playerID {
			t.Fatalf("freed bee %d targets %d, want the player %d", id, got, playerID)
		}
		if srv.MobAngerTicks(id) <= 0 {
			t.Fatalf("freed bee %d is calm", id)
		}
		x, _, z, _ := srv.MobPos(id)
		if z > 2 || z < 0.5 || x < 4.5 || x > 6.5 {
			t.Fatalf("freed bee %d at (%v, %v), want in front of the north face", id, x, z)
		}
	}
	nectar := 0
	for _, id := range ids {
		if f, _ := srv.MobBeeFlags(id); f&0x08 != 0 {
			nectar++
		}
	}
	if nectar != 1 {
		t.Fatalf("%d freed bees carry nectar, want the one stored with it", nectar)
	}
}

func nestState(t *testing.T) uint16 {
	return hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]")
}

// TestASilkTouchedNestDropsItsBeesAndHoney: the silk-touched nest's item
// carries its occupants and honey level through copy_components and
// copy_state. vanilla: BeehiveBlockEntity.collectImplicitComponents,
// CopyBlockState.run
func TestASilkTouchedNestDropsItsBeesAndHoney(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.GiveSlot("Slayer", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:silk_touch", 1))
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]"), storedBee(false))
	c.Settle()
	c.BreakBlock(5, 64, 2)
	c.Settle()
	// beeWorld holds the loop; a survival dig of a nest takes ticks.
	servertest.UntilTicks(t, srv, 40, "the nest broken", func() bool { return srv.BlockAt(5, 64, 2) == 0 })
	drop := servertest.ExpectOneDroppedItem(t, srv, "minecraft:bee_nest")
	if bees, ok := drop.Stack.BeesOf(); !ok || len(bees) != 1 {
		t.Errorf("the dropped nest holds bees %v (ok %v), want one", bees, ok)
	}
	if props, ok := drop.Stack.BlockStatePropertiesOf(); !ok || props["honey_level"] != "5" {
		t.Errorf("the dropped nest's block state = %v (ok %v), want honey_level 5", props, ok)
	}
}

func TestACreativeBreakFreesNoBees(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	c.ChatCommand("gamemode creative")
	c.Settle()
	putHive(srv, 5, 64, 2, nestState(t), storedBee(false))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if got := srv.BlockAt(5, 64, 2); got != 0 {
		t.Fatalf("the nest is still %d", got)
	}
	if ids := srv.MobIDsOfKind(bee); len(ids) != 0 {
		t.Fatalf("%d bees left a nest broken in creative, want none", len(ids))
	}
}

// Stored bees' clocks read the same on every tick and after a rewrite of the
// hive's data.
// vanilla: BeehiveBlockEntity.tickOccupants
func TestAHiveReadsItsBeesClockEveryTickAndAfterARewrite(t *testing.T) {
	t.Parallel()
	_, _, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, registry.BlockBeehive.DefaultState(), storedBee(false))
	ticks := func() int32 {
		tag, _ := srv.BlockEntityTagAt(5, 64, 2)
		list, _ := nbt.Get[nbt.List](tag, "bees")
		bee, _ := nbt.CompoundView(list.Items[0])
		n, _ := nbt.IntValue(bee["ticks_in_hive"])
		return n
	}
	for want := int32(6); want <= 8; want++ {
		servertest.RunTicks(t, srv, 1)
		if got := ticks(); got != want {
			t.Fatalf("ticks_in_hive = %d, want %d", got, want)
		}
	}
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	srv.SetBlockEntityTagAt(5, 64, 2, "minecraft:beehive", tag)
	servertest.RunTicks(t, srv, 2)
	if got := ticks(); got != 10 {
		t.Fatalf("ticks_in_hive after a rewrite = %d, want 10", got)
	}
}
