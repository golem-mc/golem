package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

func TestAShearedFullNestFreesItsBeesAndKeepsNone(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:shears")
	servertest.HoldTicks(t, srv)
	actor.MoveToGround(2.5, 64, 0.5, true)
	actor.Settle()
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]"), storedBee(false))
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed, want 1", len(ids))
	}
	if srv.MobTargetID(ids[0]) == 0 {
		t.Fatal("a bee freed by a shear does not go for the player")
	}
	actor.Settle() // the use is handled to its end
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	if list, _ := nbt.Get[nbt.List](tag, "bees"); len(list.Items) != 0 {
		t.Fatalf("the hive still stores %d bees", len(list.Items))
	}
	if got := srv.BlockAt(5, 64, 2); got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]") {
		t.Fatalf("the nest is %d, want honey level 0", got)
	}
}

// TestAFreedBeeWaitsOutMeleeAttackGoalsWindowBeforeItStings checks that the
// first goal pass, run before the anger starts, uses up the 20-tick window.
func TestAFreedBeeWaitsOutMeleeAttackGoalsWindowBeforeItStings(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:shears")
	servertest.HoldTicks(t, srv)
	actor.MoveToGround(2.5, 64, 0.5, true)
	actor.Settle()
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]"), storedBee(false))
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	for i := range 20 {
		servertest.RunTicks(t, srv, 1)
		if h := srv.PlayerHealth("Actor"); h < 20 {
			t.Fatalf("stung %d ticks after the shear, want no sting before tick 20", i+1)
		}
	}
	servertest.UntilTicks(t, srv, 40, "the sting", func() bool { return srv.PlayerHealth("Actor") < 20 })
}

func TestACampfireBelowKeepsTheBeesInAShearedNest(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:shears")
	servertest.HoldTicks(t, srv)
	actor.MoveToGround(2.5, 64, 0.5, true)
	actor.Settle()
	srv.SetBlock(5, 62, 2, hiveState(t, "minecraft:campfire", "[facing=north,lit=true,signal_fire=false,waterlogged=false]"))
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]"), storedBee(false))
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	servertest.RunTicks(t, srv, 40)
	if ids := srv.MobIDsOfKind(bee); len(ids) != 0 {
		t.Fatalf("%d bees left a nest over a lit campfire", len(ids))
	}
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	if list, _ := nbt.Get[nbt.List](tag, "bees"); len(list.Items) != 1 {
		t.Fatalf("the nest stores %d bees, want 1", len(list.Items))
	}
	if got := srv.BlockAt(5, 64, 2); got != hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]") {
		t.Fatalf("the nest is %d, want honey level 0", got)
	}
}

func nestState(t *testing.T) uint16 {
	return hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]")
}

func TestASilkTouchedNestFreesNoBees(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	srv.GiveSlot("Slayer", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:silk_touch", 1))
	putHive(srv, 5, 64, 2, nestState(t), storedBee(false))
	c.Settle()
	c.BreakBlock(5, 64, 2)
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if got := srv.BlockAt(5, 64, 2); got != 0 {
		t.Fatalf("the nest is still %d", got)
	}
	if ids := srv.MobIDsOfKind(bee); len(ids) != 0 {
		t.Fatalf("%d bees left a silk-touched nest, want none", len(ids))
	}
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

// vanilla: BeehiveBlock.playerWillDestroy
func TestACreativeBreakDropsTheNestWithItsBeesAndHoney(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	c.ChatCommand("gamemode creative")
	c.Settle()
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=3]"), storedBee(false))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	drop := servertest.ExpectOneDroppedItem(t, srv, "minecraft:bee_nest")
	if bees, ok := drop.Stack.BeesOf(); !ok || len(bees) != 1 {
		t.Errorf("the dropped nest holds bees %v (ok %v), want one", bees, ok)
	}
	if props, ok := drop.Stack.BlockStatePropertiesOf(); !ok || props["honey_level"] != "3" {
		t.Errorf("the dropped nest's block state = %v (ok %v), want honey_level 3", props, ok)
	}
}

func TestACreativeBreakOfAnEmptyNestDropsNothing(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	c.ChatCommand("gamemode creative")
	c.Settle()
	srv.SetBlock(5, 64, 2, nestState(t))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	if got := srv.BlockAt(5, 64, 2); got != 0 {
		t.Fatalf("the nest is still %d", got)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

func TestACampfireDoesNotStopTheBeesOfABrokenNest(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyNormal)
	srv.SetBlock(5, 62, 2, hiveState(t, "minecraft:campfire", "[facing=north,lit=true,signal_fire=false,waterlogged=false]"))
	putHive(srv, 5, 64, 2, nestState(t), storedBee(false))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed over a campfire, want 1", len(ids))
	}
	if got := srv.MobTargetID(ids[0]); got != playerID {
		t.Fatalf("the freed bee targets %d, want the player %d", got, playerID)
	}
}

func TestABottledFullNestFreesItsBees(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.ToolScene(t, "minecraft:glass_bottle")
	servertest.HoldTicks(t, srv)
	actor.MoveToGround(2.5, 64, 0.5, true)
	actor.Settle()
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=5]"), storedBee(false))
	actor.UseBlock(5, 64, 2)
	actor.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed, want 1", len(ids))
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:honey_bottle") {
		t.Fatalf("the hand holds item %d, want a honey bottle", got.ItemID)
	}
	if got := srv.BlockAt(5, 64, 2); got != nestState(t) {
		t.Fatalf("the nest is %d, want honey level 0", got)
	}
	tag, _ := srv.BlockEntityTagAt(5, 64, 2)
	if list, _ := nbt.Get[nbt.List](tag, "bees"); len(list.Items) != 0 {
		t.Fatalf("the hive still stores %d bees", len(list.Items))
	}
}

func TestABlockedFrontCellReleasesTheBeeInsideTheHive(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, nestState(t), storedBee(false))
	srv.SetBlock(5, 64, 1, blockNamed(t, "minecraft:stone"))
	c.BreakBlock(5, 64, 2)
	c.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed, want 1", len(ids))
	}
	x, _, z, _ := srv.MobPos(ids[0])
	if x < 5.4 || x > 5.6 || z < 2.4 || z > 2.6 {
		t.Fatalf("the bee is at (%v, %v), want the hive's own cell (5.5, 2.5)", x, z)
	}
}

func TestASpectatorIsNotTheTargetOfAngryBees(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	// By day, so the calm bees do not go into the nest.
	srv.SetDayTime(6000)
	srv.SetDifficulty(rules.DifficultyNormal)
	c := connect()
	playerID := c.JoinAs("Slayer")
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Settle()
	spec := connect().Join("Watcher")
	spec.MoveToGround(3.5, 64, 0.5, true)
	spec.Settle()
	srv.RunLine("gamemode spectator Watcher")
	c.Settle()
	servertest.HoldTicks(t, srv)
	putHive(srv, 5, 64, 2, nestState(t))
	var calm []int32
	for i := range 8 {
		calm = append(calm, srv.SpawnMob(bee, 6.5+float64(i%3), 64, 0.5+float64(i/3)))
	}
	servertest.RunTicks(t, srv, 1)
	c.BreakBlock(5, 64, 2)
	c.Settle()
	for range 60 {
		if srv.BlockAt(5, 64, 2) == 0 {
			break
		}
		servertest.RunTicks(t, srv, 1)
	}
	for _, id := range calm {
		if got := srv.MobTargetID(id); got != playerID {
			t.Fatalf("bee %d targets %d, want the survival player %d", id, got, playerID)
		}
	}
}

// A released bee reads HasNectar and Age leniently, so a float tag still counts.
// vanilla: Bee.readAdditionalSaveData, AgeableMob.readAdditionalSaveData
func TestAHiveBeeWithFloatNectarAndAgeKeepsBoth(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	stored := storedBee(false)
	data := stored["entity_data"].(nbt.Compound)
	data["HasNectar"] = nbt.Float(1)
	data["Age"] = nbt.Float(-100.5)
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]"), stored)
	c.BreakBlock(5, 64, 2)
	c.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed, want 1", len(ids))
	}
	if f, _ := srv.MobBeeFlags(ids[0]); f&0x08 == 0 {
		t.Fatal("the freed bee lost its nectar")
	}
	if !srv.MobIsBaby(ids[0]) {
		t.Fatal("the freed bee is not a baby")
	}
}

func TestABlastThatBreaksANestAngersItsBeesAtThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, hiveState(t, "minecraft:bee_nest", "[facing=north,honey_level=0]"), storedBee(false))
	srv.SpawnTNT(5.5, 64, 3.5, 1)
	c.Settle()
	ids := beesFreed(t, srv, 1)
	if len(ids) != 1 {
		t.Fatalf("%d bees freed, want 1", len(ids))
	}
	if got := srv.MobTargetID(ids[0]); got != playerID {
		t.Fatalf("freed bee targets %d, want the player %d", got, playerID)
	}
}

// A blast from an entity that does not free stored bees still angers a bee
// beside the hive.
// vanilla: BeehiveBlock.onExplosionHit
func TestAnEndCrystalBlastThatBreaksANestAngersABeeAtThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyNormal)
	putHive(srv, 5, 64, 2, nestState(t))
	id := srv.SpawnMob(bee, 6.5, 66, 2.5)
	srv.SetMobInvulnerable(id)
	crystal := srv.SpawnEndCrystal(3.5, 64, 3.5)
	c.Attack(crystal)
	c.Settle()
	for range 20 {
		servertest.RunTicks(t, srv, 1)
		if srv.MobTargetID(id) != 0 {
			break
		}
	}
	if got := srv.MobTargetID(id); got != playerID {
		t.Fatalf("bee targets %d, want the player %d", got, playerID)
	}
}

// A leashed bee entering its hive lets go of its lead: the lead lies as an
// item and the hold is told gone.
// vanilla: BeehiveBlockEntity.addOccupant
func TestALeashedBeeEnteringItsHiveDropsItsLead(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	nestAt(t, srv, "0")
	c.ChatCommand("summon minecraft:bee 5.5 64 1.5 {hive_pos:[I;5,64,2]}")
	c.Settle()
	ids := srv.MobIDsOfKind(bee)
	if len(ids) != 1 {
		t.Fatalf("%d bees, want the one summoned", len(ids))
	}
	giveLead(t, srv, "Slayer", 1)
	c.UseOn(ids[0])
	if attached, holding := c.ExpectLinkPacket(); attached != ids[0] || holding != c.PlayerEntityID {
		t.Fatalf("the tie links (%d, %d), want the bee %d held by the player", attached, holding, ids[0])
	}
	for range 40 {
		servertest.RunTicks(t, srv, 1)
		if storedCount(srv) > 0 {
			break
		}
	}
	if storedCount(srv) != 1 {
		t.Fatal("the bee did not enter its hive")
	}
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the entering bee dropped %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != ids[0] || holding != 0 {
		t.Errorf("the entry links (%d, %d), want the bee %d held by none", attached, holding, ids[0])
	}
}

// TestAHiveWithABeeInsideBuzzesNowAndThen: a hive holding a bee plays its
// working sound on a 1-in-200 roll each tick; 2000 ticks miss it with a
// chance under 1 in 20000. vanilla: BeehiveBlockEntity.serverTick
func TestAHiveWithABeeInsideBuzzesNowAndThen(t *testing.T) {
	t.Parallel()
	c, _, srv := beeWorld(t, rules.DifficultyNormal)
	occupant := storedBee(false)
	occupant["min_ticks_in_hive"] = nbt.Int(1000000)
	putHive(srv, 5, 64, 2, nestState(t), occupant)
	srv.SetLevelSeed(1)
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 2000)
	if got := servertest.CountSounds(heard, "minecraft:block.beehive.work", protocol.SoundCategoryBlock); got < 1 {
		t.Fatalf("heard the hive work %d times in 2000 ticks, want at least 1", got)
	}
}
