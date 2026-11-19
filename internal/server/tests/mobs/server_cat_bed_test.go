package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// Dye IDs of the collar metadata.
// vanilla: DyeColor.getId
const (
	dyeBlue = 11
	dyeRed  = 14
)

// stateOf is the state of the block with the properties given.
func stateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state of %s with %v", name, props)
	}
	return id
}

// catState reads the cat's server-side bed and seat state.
func catState(t *testing.T, srv *server.Server, id int32) server.CatBeds {
	t.Helper()
	st, ok := srv.CatState(id)
	if !ok {
		t.Fatalf("cat %d is not in the herd", id)
	}
	return st
}

// TestAnOwnersDyeRecoloursACatsCollar: the owner's dye sets the collar and is
// spent; the colour it already wears is no change and sits the cat; a
// stranger's dye does nothing.
// vanilla: Cat.mobInteract
func TestAnOwnersDyeRecoloursACatsCollar(t *testing.T) {
	t.Parallel()
	const name = "Dyer"
	srv, c := catYard(t, name, "minecraft:blue_dye")
	mine := spawnCat(t, srv, c, 1.5, 2.5)
	other := spawnCat(t, srv, c, -1.5, 2.5)
	srv.SetMobOwner(mine.EntityID, protocol.OfflineUUID(name))
	srv.SetMobOwner(other.EntityID, protocol.OfflineUUID("Stranger"))
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:blue_dye"), Count: 5})

	c.UseOn(other.EntityID)
	c.UseOn(mine.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 2, &mine, &other)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 4 {
		t.Errorf("%d dyes left after one click on each cat, want 4", got)
	}
	if !mine.HasCatCollar || mine.CatCollar != dyeBlue {
		t.Errorf("the owner's cat tells collar %d (told %v), want blue", mine.CatCollar, mine.HasCatCollar)
	}
	if other.HasCatCollar || catState(t, srv, other.EntityID).Collar != "red" {
		t.Errorf("a stranger's dye recoloured the collar")
	}

	c.UseOn(mine.EntityID)
	c.Settle()
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 4 {
		t.Errorf("the colour already worn spent a dye: %d left, want 4", got)
	}
	if st, _ := srv.MobTamable(mine.EntityID); !st.OrderedToSit {
		t.Error("a dye the collar already wears did not sit the cat")
	}
}

// TestATameCatLiesOnABedButNotWhenOrderedToSit: a tame cat walks to a bed and
// lies on it, told by the bed-pose metadata; one ordered to sit stays put.
// The goal's search starts three layers below the cat's feet, so the cats
// stand on a stone platform over the bed.
// vanilla: CatLieOnBedGoal
func TestATameCatLiesOnABedButNotWhenOrderedToSit(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	stone := stateOf(t, "minecraft:stone", nil)
	for _, at := range [][2]int32{{1, 5}, {1, 6}, {2, 5}, {2, 6}} {
		for y := int32(64); y <= 66; y++ {
			if at[0] == 2 && y == 66 {
				continue
			}
			srv.SetBlock(at[0], y, at[1], stone)
		}
	}
	srv.SetBlock(5, 64, 6, servertest.BedStateName("red", "head", "south"))
	srv.SetBlock(5, 64, 5, servertest.BedStateName("red", "foot", "south"))
	srv.SpawnMob("minecraft:cat", 1.5, 67, 5.5)
	lier := c.ExpectMobSpawn(protocol.EntityTypeCat)
	srv.SpawnMob("minecraft:cat", 1.5, 67, 6.5)
	sitter := c.ExpectMobSpawn(protocol.EntityTypeCat)
	for _, m := range []servertest.SpawnedMob{lier, sitter} {
		srv.SetMobOwner(m.EntityID, protocol.OfflineUUID(name))
	}
	c.UseOn(sitter.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 300, &lier, &sitter)
	if st := catState(t, srv, lier.EntityID); !st.Lying || st.SittingPose {
		t.Errorf("the tame cat lies=%v sitting=%v after 300 ticks above a bed, want lying", st.Lying, st.SittingPose)
	}
	if !lier.CatLying {
		t.Error("the lying cat's metadata does not say so")
	}
	if st := catState(t, srv, sitter.EntityID); st.Lying {
		t.Error("a cat ordered to sit lay down on the bed")
	}
}

// TestATameCatSitsOnAChestAndALitFurnaceOnly: a free chest and a lit furnace
// are seats, an unlit furnace is not.
// vanilla: CatSitOnBlockGoal.isValidTarget
func TestATameCatSitsOnAChestAndALitFurnaceOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		props map[string]string
		sits  bool
	}{
		{"chest", "minecraft:chest", map[string]string{"facing": "north"}, true},
		{"lit furnace", "minecraft:furnace", map[string]string{"facing": "north", "lit": "true"}, true},
		{"unlit furnace", "minecraft:furnace", map[string]string{"facing": "north", "lit": "false"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "Keeper"
			srv, c := catYard(t, name, "")
			srv.SetBlock(4, 64, 3, stateOf(t, tc.block, tc.props))
			cat := spawnCat(t, srv, c, 1.5, 2.5)
			srv.SetMobOwner(cat.EntityID, protocol.OfflineUUID(name))
			advanceCat(t, srv, c, 200, &cat)
			if st := catState(t, srv, cat.EntityID); st.SittingPose != tc.sits {
				t.Errorf("a tame cat beside a %s sits = %v, want %v", tc.name, st.SittingPose, tc.sits)
			}
		})
	}
}

// huntYard joins a player and spawns the hunter and its prey, a wild
// hunter outside the sixteen blocks it flees a player from.
func huntYard(t *testing.T, hunter, prey string, tame bool) (*server.Server, *servertest.Client, servertest.SpawnedMob, servertest.SpawnedMob) {
	t.Helper()
	const name = "Watcher"
	srv, c := catYard(t, name, "")
	types := map[string]int32{
		"minecraft:cat": protocol.EntityTypeCat, "minecraft:ocelot": protocol.EntityTypeOcelot,
		"minecraft:rabbit": protocol.EntityTypeRabbit, "minecraft:chicken": protocol.EntityTypeChicken,
	}
	// A tame cat stays near its owner, so its prey is brought there.
	x := 40.5
	if tame {
		x = 4.5
	}
	srv.SpawnMob(hunter, x, 64, 0.5)
	h := c.ExpectMobSpawn(types[hunter])
	srv.SpawnMob(prey, x+3, 64, 0.5)
	p := c.ExpectMobSpawn(types[prey])
	if tame {
		srv.SetMobOwner(h.EntityID, protocol.OfflineUUID(name))
	}
	return srv, c, h, p
}

// TestWildFelinesHuntTheirPrey: a wild cat kills a rabbit and an ocelot a
// chicken; a tame cat leaves the rabbit be.
// vanilla: Cat.registerGoals, Ocelot.registerGoals, OcelotAttackGoal
func TestWildFelinesHuntTheirPrey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		hunter, prey string
		tame, kills  bool
	}{
		{"minecraft:cat", "minecraft:rabbit", false, true},
		{"minecraft:ocelot", "minecraft:chicken", false, true},
		{"minecraft:cat", "minecraft:rabbit", true, false},
	} {
		t.Run(tc.hunter+" "+tc.prey, func(t *testing.T) {
			t.Parallel()
			srv, c, _, prey := huntYard(t, tc.hunter, tc.prey, tc.tame)
			advanceCat(t, srv, c, 400)
			health, alive := srv.MobHealth(prey.EntityID)
			if killed := !alive || health <= 0; killed != tc.kills {
				t.Errorf("%s hunting a %s (tame %v): prey killed = %v (health %v), want %v",
					tc.hunter, tc.prey, tc.tame, killed, health, tc.kills)
			}
		})
	}
}

// TestAVillageOfBedsSpawnsAStrayCat: the spawner rolls a cat near a player in
// a village with more than four beds and fewer than five cats, and in no
// other.
// vanilla: CatSpawner.tick, spawnInVillage
func TestAVillageOfBedsSpawnsAStrayCat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		beds  int
		cats  int
		spawn bool
		claim bool
	}{
		{"five beds", 5, 0, true, true},
		{"four beds", 4, 0, false, true},
		{"five cats", 5, 5, false, true},
		{"five unclaimed beds", 5, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := catYard(t, "Mayor", "")
			for i := range tc.beds {
				z := int32(2 * i)
				srv.SetBlock(6, 64, z+1, servertest.BedStateName("red", "head", "south"))
				srv.SetBlock(6, 64, z, servertest.BedStateName("red", "foot", "south"))
			}
			for range tc.cats {
				srv.SpawnMob("minecraft:cat", -6.5, 64, 0.5)
			}
			if tc.claim {
				for i := range tc.beds {
					srv.SpawnMob("minecraft:villager", 6.5, 64, float64(2*i)+1.5)
				}
				servertest.UntilTicks(t, srv, 600, "the villagers claim the beds", func() bool {
					return srv.ClaimedPOIsAround(6, 64, 4, 16) >= tc.beds
				})
			}
			before := srv.MobCount()
			for seed := int64(1); seed <= 12; seed++ {
				srv.CatSpawnerPass(seed)
			}
			if got := srv.MobCount() - before; (got > 0) != tc.spawn {
				t.Errorf("%d new cats over 12 passes with %d beds and %d cats, want spawn = %v", got, tc.beds, tc.cats, tc.spawn)
			}
		})
	}
}

// giftYard puts the player to bed at midnight with a tame cat beside them
// and lets the cat settle, with the tick loop held.
func giftYard(t *testing.T, seed int64) (*server.Server, *servertest.Client, servertest.SpawnedMob) {
	t.Helper()
	srv, c, cat := sleeperYard(t, seed)
	servertest.RunTicks(t, srv, 60)
	return srv, c, cat
}

// sleeperYard is giftYard before the cat has had a tick.
func sleeperYard(t *testing.T, seed int64) (*server.Server, *servertest.Client, servertest.SpawnedMob) {
	t.Helper()
	const name = "Sleeper"
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, name, servertest.NoTimeSync)
	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	servertest.HoldTicks(t, srv)
	srv.SpawnMobSeeded("minecraft:cat", 4.5, 64, 3.5, seed)
	cat := c.ExpectMobSpawn(protocol.EntityTypeCat)
	srv.SetMobOwner(cat.EntityID, protocol.OfflineUUID(name))
	return srv, c, cat
}

// giftsAfter reports how many gift items the world and the player's
// inventory hold.
func giftsAfter(srv *server.Server) int {
	n := len(srv.ItemsInDimension("minecraft:overworld"))
	for slot := range 46 {
		if st := srv.InventorySlot("Sleeper", slot); st.Count > 0 {
			n++
		}
	}
	return n
}

// TestACatLeavesAMorningGiftWhenItsOwnerSleepsThroughTheNight: a tame cat
// beside a sleeping owner settles on the bed, and when the night passes it
// may leave an item; a wake at midnight leaves none.
// vanilla: Cat.CatRelaxOnOwnerGoal
func TestACatLeavesAMorningGiftWhenItsOwnerSleepsThroughTheNight(t *testing.T) {
	t.Parallel()
	gifted := 0
	const cats = 8
	for seed := range int64(cats) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			srv, c, cat := giftYard(t, seed+1)
			advanceCat(t, srv, c, 1, &cat)
			if !cat.CatRelaxing && !cat.CatLying {
				t.Fatalf("the tame cat did not settle by its sleeping owner: %+v", catState(t, srv, cat.EntityID))
			}
			servertest.RunTicks(t, srv, 120)
			if srv.Sleeping("Sleeper") {
				t.Fatal("the night did not pass")
			}
			servertest.RunTicks(t, srv, 10)
			if giftsAfter(srv) > 0 {
				gifted++
			}
		})
	}
	if gifted == 0 || gifted == cats {
		t.Errorf("%d of %d cats left a gift, want some but not all (chance 0.7)", gifted, cats)
	}
}

// TestACatLeavesNoGiftWhenItsOwnerWakesAtMidnight: the gift chance is zero
// outside the hours around dawn, so a cat whose owner gets up at midnight
// leaves nothing, whatever its dice.
// vanilla: Timelines.OVERWORLD_DAY's cat_waking_up_gift_chance
func TestACatLeavesNoGiftWhenItsOwnerWakesAtMidnight(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			srv, c, _ := giftYard(t, seed)
			c.LeaveBed()
			c.Settle()
			servertest.RunTicks(t, srv, 30)
			if srv.Sleeping("Sleeper") {
				t.Fatal("the owner did not get up")
			}
			if n := giftsAfter(srv); n > 0 {
				t.Errorf("a cat left %d items for an owner who woke at midnight", n)
			}
		})
	}
}

// TestATameCatAvoidsAChestSomeoneHasOpen: a chest a player is looking into is
// no seat.
// vanilla: CatSitOnBlockGoal.isValidTarget, ChestBlockEntity.getOpenCount
func TestATameCatAvoidsAChestSomeoneHasOpen(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	srv.SetBlock(3, 64, 1, stateOf(t, "minecraft:chest", map[string]string{"facing": "north"}))
	c.UseBlock(3, 64, 1)
	c.Settle()
	cat := spawnCat(t, srv, c, 1.5, 2.5)
	srv.SetMobOwner(cat.EntityID, protocol.OfflineUUID(name))
	advanceCat(t, srv, c, 200, &cat)
	if catState(t, srv, cat.EntityID).SittingPose {
		t.Error("a tame cat sat on a chest a player had open")
	}
}

// TestACatLiesBesideItsOwnerAfterEightGoalTicks: the cat settles beside the
// sleeping owner and lies down eight goal ticks later, which is sixteen game
// ticks as the goal runs every other tick.
// vanilla: Cat.CatRelaxOnOwnerGoal.tick, adjustedTickDelay(16)
func TestACatLiesBesideItsOwnerAfterEightGoalTicks(t *testing.T) {
	t.Parallel()
	srv, _, cat := sleeperYard(t, 1)
	settled, lay := -1, -1
	for tick := range 120 {
		servertest.RunTicks(t, srv, 1)
		st := catState(t, srv, cat.EntityID)
		if st.Relaxing && settled < 0 {
			settled = tick
		}
		if st.Lying {
			lay = tick
			break
		}
	}
	if settled < 0 || lay < 0 {
		t.Fatalf("the cat settled at tick %d and lay at %d, want both", settled, lay)
	}
	if got := lay - settled; got < 13 || got > 19 {
		t.Errorf("the cat lay %d ticks after settling, want about 16", got)
	}
}

// TestATameCatAvoidsADoubleChestSomeoneHasOpen: a player in one half of a
// double chest keeps the cat off both halves.
// vanilla: CatSitOnBlockGoal.isValidTarget, CompoundContainer.startOpen
func TestATameCatAvoidsADoubleChestSomeoneHasOpen(t *testing.T) {
	t.Parallel()
	for _, open := range []bool{true, false} {
		t.Run(fmt.Sprint("open ", open), func(t *testing.T) {
			t.Parallel()
			const name = "Keeper"
			srv, c := catYard(t, name, "")
			srv.SetBlock(3, 64, 1, stateOf(t, "minecraft:chest", map[string]string{"facing": "north", "type": "left"}))
			srv.SetBlock(4, 64, 1, stateOf(t, "minecraft:chest", map[string]string{"facing": "north", "type": "right"}))
			if open {
				c.UseBlock(3, 64, 1)
				c.Settle()
			}
			cat := spawnCat(t, srv, c, 5.5, 2.5)
			srv.SetMobOwner(cat.EntityID, protocol.OfflineUUID(name))
			advanceCat(t, srv, c, 200, &cat)
			if sat := catState(t, srv, cat.EntityID).SittingPose; sat == open {
				t.Errorf("a tame cat beside a double chest (open %v) sits = %v", open, sat)
			}
		})
	}
}

// TestWildFelinesBiteABabyTurtleAshoreButNotAnAdult: a wild cat and an
// ocelot go for a baby turtle on land and leave a grown one be.
// vanilla: Cat.registerGoals, Ocelot.registerGoals, Turtle.BABY_ON_LAND_SELECTOR
func TestWildFelinesBiteABabyTurtleAshoreButNotAnAdult(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		hunter string
		baby   bool
	}{
		{"minecraft:cat", true},
		{"minecraft:ocelot", true},
		{"minecraft:cat", false},
	} {
		t.Run(fmt.Sprint(tc.hunter, " baby ", tc.baby), func(t *testing.T) {
			t.Parallel()
			srv, c := catYard(t, "Watcher", "")
			srv.SpawnMob(tc.hunter, 40.5, 64, 0.5)
			turtle := srv.SpawnMob("minecraft:turtle", 43.5, 64, 0.5)
			srv.SetMobBaby(turtle, tc.baby)
			full, _ := srv.MobHealth(turtle)
			advanceCat(t, srv, c, 400)
			health, alive := srv.MobHealth(turtle)
			if bitten := !alive || health < full; bitten != tc.baby {
				t.Errorf("%s and a turtle (baby %v): bitten = %v (health %v of %v), want %v",
					tc.hunter, tc.baby, bitten, health, full, tc.baby)
			}
		})
	}
}

// TestACatPennedTwoBlocksFromItsSleepingOwnerNeverLies: the cat lies only
// within sqrt(2.5) blocks of its owner, so one penned about 2.1 blocks away
// stays up.
// vanilla: Cat.CatRelaxOnOwnerGoal.tick (distanceToSqr < 2.5)
func TestACatPennedTwoBlocksFromItsSleepingOwnerNeverLies(t *testing.T) {
	t.Parallel()
	srv, c, _ := sleeperYard(t, 1)
	glass := registry.BlockGlass.DefaultState()
	for x := int32(6); x <= 8; x++ {
		for z := int32(0); z <= 2; z++ {
			if x == 7 && z == 1 {
				continue
			}
			srv.SetBlock(x, 64, z, glass)
			srv.SetBlock(x, 65, z, glass)
		}
	}
	id := srv.SpawnMobSeeded("minecraft:cat", 7.5, 64, 1.5, 1)
	c.ExpectMobSpawn(protocol.EntityTypeCat)
	srv.SetMobOwner(id, protocol.OfflineUUID("Sleeper"))
	for range 120 {
		servertest.RunTicks(t, srv, 1)
		if st := catState(t, srv, id); st.Lying || st.Relaxing {
			t.Fatalf("the penned cat settled by its owner: %+v", st)
		}
	}
}
