package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The breeding of the animals, driven the way the rest of the mobs are —
// through a real connection. A right-click with the kind's food sets the
// love mode (Animal.mobInteract, the hearts of entity status 18
// broadcastEntityEvent answers), two loving adults of a kind find each
// other and a baby is born at the mother with both parents cooled down for
// five minutes, a fed baby grows a tenth of the age it owes at a feeding,
// and a baby follows the grown body of its kind around (FollowParentGoal).

// loveHearts is the entity status Animal.setInLove and
// finalizeSpawnChildFromBreeding broadcast, status 18, which a client
// answers with the burst of heart particles of a loving body.
const loveHearts = 18

// mobWithID finds the one mob of the world standing under the entity ID, or
// answers false where none is.
func mobWithID(srv *server.Server, id int32) (server.TestMob, bool) {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m, true
		}
	}
	return server.TestMob{}, false
}

// blocksAway is the plain distance between two places, for a test's message.
func blocksAway(x0, y0, z0, x1, y1, z1 float64) float64 {
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// TestEachAnimalFallsInLoveForItsFood: a right-click with the kind's own
// food (Animal.mobInteract answered through isFood — the same items the
// animal is tempted by) sets the love mode and broadcasts the hearts of
// entity status 18, while a right-click with another kind's food does
// nothing at all: no love, nothing spent.
func TestEachAnimalFallsInLoveForItsFood(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind       string
		entityType int32
		food       string
		otherFood  string
	}{
		{"minecraft:cow", protocol.EntityTypeCow, "minecraft:wheat", "minecraft:carrot"},
		{"minecraft:pig", protocol.EntityTypePig, "minecraft:carrot", "minecraft:wheat"},
		{"minecraft:sheep", protocol.EntityTypeSheep, "minecraft:wheat", "minecraft:wheat_seeds"},
		{"minecraft:chicken", protocol.EntityTypeChicken, "minecraft:wheat_seeds", "minecraft:potato"},
		{"minecraft:rabbit", protocol.EntityTypeRabbit, "minecraft:carrot", "minecraft:wheat"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Feeder", tc.otherFood, 1)
			// With the world still, every count the test reads is exact.
			srv.StopTick()
			srv.SpawnMob(tc.kind, 1.5, 64, 2.5)
			beast := c.ExpectMobSpawn(tc.entityType)
			if beast.Baby {
				t.Fatal("the planted animal is a baby; plant it elsewhere")
			}

			// Another kind's food is no food of this one's: nothing
			// happens, and the item is kept.
			c.UseOn(beast.EntityID)
			c.Settle()
			if got := srv.InventorySlot("Feeder", player.HotbarSlot).Count; got != 1 {
				t.Errorf("%s fed its other-kind food kept %d of it, want the 1 untouched", tc.kind, got)
			}
			if m, _ := mobWithID(srv, beast.EntityID); m.InLove != 0 {
				t.Errorf("%s loves for %d after its other-kind food, want no love", tc.kind, m.InLove)
			}

			// Its own food sets the love mode and shows the hearts.
			srv.GiveSlot("Feeder", player.HotbarSlot,
				protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.food), Count: 3})
			c.UseOn(beast.EntityID)
			// setInLove's hearts go out ahead of playEatingSound.
			c.AwaitEntityEvent(beast.EntityID, loveHearts)
			if tc.kind == "minecraft:pig" {
				// Alone of the four the pig has an eat sound of its
				// own (Pig.playEatingSound), played after the hearts.
				// A spawned pig's sound variant is drawn from the
				// chunk, so its set's own eat sound is any of the
				// four; TestAPigsEatSoundComesFromItsSet pins each.
				_, x, y, z, volume := c.AwaitOneSoundNamed(
					"minecraft:entity.pig.eat", "minecraft:entity.pig_big.eat",
					"minecraft:entity.pig_mini.eat", "minecraft:entity.baby_pig.eat")
				if volume != 1 {
					t.Errorf("the pig's eat sound played at volume %v, want its full voice", volume)
				}
				if x != 12 || y != 64*8 || z != 20 {
					t.Errorf("the pig's eat sound at (%d,%d,%d), want the pig's place (12,512,20)", x, y, z)
				}
			}
			if got := srv.InventorySlot("Feeder", player.HotbarSlot).Count; got != 2 {
				t.Errorf("%s fed its food kept %d of it, want the 2 left of 3", tc.kind, got)
			}
			m, ok := mobWithID(srv, beast.EntityID)
			if !ok || m.InLove != 600 {
				t.Errorf("%s's love mode = %d (standing %v), want the 600 of Animal.setInLove", tc.kind, m.InLove, ok)
			}

			// A hurt the body takes ends the love at once, vanilla's
			// Animal.actuallyHurt resetting it.
			c.Attack(beast.EntityID)
			c.Settle()
			if m, _ := mobWithID(srv, beast.EntityID); m.InLove != 0 {
				t.Errorf("%s still loves for %d after the blow, want the hurt's end of it", tc.kind, m.InLove)
			}
		})
	}
}

// TestTwoFedCowsBreed: two cows fed wheat show the hearts and a baby cow is
// born at the mother — spawnChildFromBreeding — with an experience orb
// dropped beside her (the 1-7 of finalizeSpawnChildFromBreeding), and both
// parents take the five-minute breed cooldown, in which feeding again is
// refused: nothing spent, nothing shown.
func TestTwoFedCowsBreed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64)
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow1 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow2 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow1.Baby || cow2.Baby {
		t.Fatal("a planted cow is a baby; plant them elsewhere")
	}

	c.UseOn(cow1.EntityID)
	c.AwaitEntityEvent(cow1.EntityID, loveHearts)
	c.UseOn(cow2.EntityID)
	c.AwaitEntityEvent(cow2.EntityID, loveHearts)
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		return srv.InventorySlot("Rancher", player.HotbarSlot).Count == 62
	})

	// The two loving cows walk together and the courtship comes to its
	// breeding within the thirty seconds the love mode lasts.
	// finalizeSpawnChildFromBreeding drops the orb before spawnChildFromBreeding
	// stands the child up, so the orb is the first arrival.
	kind, orb := c.AwaitNextSpawn()
	if kind != protocol.EntityTypeExperienceOrb {
		t.Fatalf("the breeding's first arrival was entity type %d, want the experience orb before the child", kind)
	}
	calf := c.AwaitSpawn(protocol.EntityTypeCow)
	if calf.EntityID == cow1.EntityID || calf.EntityID == cow2.EntityID {
		t.Fatalf("the born cow (id %d) is a parent come again", calf.EntityID)
	}
	if flag := servertest.MetaOf(c.AwaitMetadataFor(calf.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("the bred cow is not a baby")
	}
	if d := blocksAway(calf.X, calf.Y, calf.Z, orb.X, orb.Y, orb.Z); d > 2.5 {
		t.Errorf("the experience orb lies %v blocks from the breeding, want it at the mother's feet", d)
	}

	mother, ok1 := mobWithID(srv, cow1.EntityID)
	other, ok2 := mobWithID(srv, cow2.EntityID)
	baby, ok3 := mobWithID(srv, calf.EntityID)
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("the herd keeps %v of the breeding's three", []bool{ok1, ok2, ok3})
	}
	// The calf keeps beside its parents — it follows the nearest grown
	// body of its kind — and neither parent loves any more.
	if d := math.Min(
		blocksAway(baby.X, baby.Y, baby.Z, mother.X, mother.Y, mother.Z),
		blocksAway(baby.X, baby.Y, baby.Z, other.X, other.Y, other.Z)); d > 5 {
		t.Errorf("the calf stands %v blocks from its parents, want it beside them", d)
	}
	if mother.InLove != 0 || other.InLove != 0 {
		t.Errorf("the parents still love (%d and %d), want the breeding's end of it", mother.InLove, other.InLove)
	}
	// Both get the cooldown, the 6000 ticking a down while the world runs;
	// the waits above cannot have spent more than a few hundred of it.
	for name, parent := range map[string]server.TestMob{"first": mother, "second": other} {
		if parent.Age <= 5500 || parent.Age > 6000 {
			t.Errorf("the %s parent's age = %d, want the 6000 tick breed cooldown", name, parent.Age)
		}
	}
	if baby.Age >= 0 || baby.Age < entity.BabyAgeStart {
		t.Errorf("the calf's age = %d, want a newborn's below %d", baby.Age, entity.BabyAgeStart)
	}

	// Feeding a cooled-down parent again is refused, the item kept.
	srv.StopTick()
	c.UseOn(cow1.EntityID)
	c.Settle()
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Count; got != 62 {
		t.Errorf("feeding a cooled-down cow spent its wheat: %d left, want the 62", got)
	}
	if m, _ := mobWithID(srv, cow1.EntityID); m.InLove != 0 {
		t.Errorf("the cooled-down cow was set loving for %d, want no love", m.InLove)
	}
	if n := srv.MobCount(); n != 3 {
		t.Errorf("the world holds %d cows, want the parents and the calf", n)
	}
}

// TestAFedBabyGrowsUpFaster: feeding a baby is what speeds its growing up
// (Animal.mobInteract's ageUp branch): a tenth of the age it still owes at
// every feeding, on the AgeableMob.getSpeedUpSecondsWhenFeeding arithmetic,
// and no love mode comes of it — no hearts for a calf's mouthful.
func TestAFedBabyGrowsUpFaster(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "minecraft:wheat", 64, servertest.OpsListing(t, "Grower"))
	srv.StopTick()
	c.ChatCommand(`summon minecraft:cow 1.5 64 2.5 {Age:-24000}`)
	calf := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !calf.Baby {
		t.Fatal("the summoned calf is a baby, want the age read to make it one")
	}

	// Two wheat, two tenths: the 2400 of the 24000 a newborn owes, then
	// the 2160 of the 21600 left standing.
	c.UseOn(calf.EntityID)
	c.UseOn(calf.EntityID)
	c.Settle()
	if got := srv.InventorySlot("Grower", player.HotbarSlot).Count; got != 62 {
		t.Fatalf("the two feedings left %d wheat, want 62", got)
	}
	m, ok := mobWithID(srv, calf.EntityID)
	if !ok {
		t.Fatal("the calf is not in the world any more")
	}
	if m.Age != -19440 {
		t.Errorf("the twice-fed calf's age = %d, want -19440 (the -24000 grown 2400 then 2160)", m.Age)
	}
	if m.InLove != 0 {
		t.Errorf("the fed calf loves for %d, want no love mode of a feeding", m.InLove)
	}
	if !m.Baby {
		t.Error("the fed calf is grown, want the baby it still is")
	}
	// vanilla: AgeableMob.addAdditionalSaveData
	if got := entityData(t, c, "@e[type=minecraft:cow,limit=1] ForcedAge"); got != "4560" {
		t.Errorf("the fed calf's ForcedAge = %s, want the 4560 ticks the feedings grew it", got)
	}
}

// TestABabyFollowsTheAdultOfItsKind: FollowParentGoal makes a baby keep
// within three blocks of the grown body of its own kind — the walk a calf
// seven blocks from an adult takes to close the gap, and no farther than
// the three the following leaves off at.
func TestABabyFollowsTheAdultOfItsKind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Watcher", "", 0, servertest.OpsListing(t, "Watcher"))
	srv.StopTick()
	srv.SpawnMob("minecraft:cow", 2.5, 64, 3.5)
	adult := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand(`summon minecraft:cow 9.5 64 3.5 {Age:-24000}`)
	calf := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !calf.Baby {
		t.Fatal("the summoned calf is a baby, want the age read to make it one")
	}

	// The walk, in batches of world-steps: FollowParentGoal closes the
	// seven-block gap within about forty ticks, while the aimless stroll
	// beside it leaves the gap standing for hundreds.
	closed := false
	for range 10 {
		srv.TickMobs(10)
		a, _ := mobWithID(srv, adult.EntityID)
		b, _ := mobWithID(srv, calf.EntityID)
		if blocksAway(a.X, a.Y, a.Z, b.X, b.Y, b.Z) < 3 {
			closed = true
			break
		}
	}
	a, _ := mobWithID(srv, adult.EntityID)
	b, _ := mobWithID(srv, calf.EntityID)
	if !closed {
		d := blocksAway(a.X, a.Y, a.Z, b.X, b.Y, b.Z)
		t.Fatalf("the calf still stands %v blocks from the adult after 100 ticks, want it closed within three", d)
	}
	if !b.Baby {
		t.Error("the calf grew up on its walk, want the baby it still is")
	}
}

// TestBredYoungOfEachKind: every farm animal breeds a baby of its own kind
// once two fed adults meet — Pig, Sheep and Chicken.getBreedOffspring beside
// the cow's own covered above.
func TestBredYoungOfEachKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind       string
		entityType int32
		food       string
	}{
		{"minecraft:pig", protocol.EntityTypePig, "minecraft:carrot"},
		{"minecraft:sheep", protocol.EntityTypeSheep, "minecraft:wheat"},
		{"minecraft:chicken", protocol.EntityTypeChicken, "minecraft:wheat_seeds"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", tc.food, 8)
			srv.SpawnMob(tc.kind, 1.5, 64, 2.5)
			srv.SpawnMob(tc.kind, 2.5, 64, 2.5)
			first := c.ExpectMobSpawn(tc.entityType)
			second := c.ExpectMobSpawn(tc.entityType)
			if first.Baby || second.Baby {
				t.Fatal("a planted adult is a baby; plant them elsewhere")
			}

			c.UseOn(first.EntityID)
			c.AwaitEntityEvent(first.EntityID, loveHearts)
			c.UseOn(second.EntityID)
			c.AwaitEntityEvent(second.EntityID, loveHearts)

			baby := c.AwaitSpawn(tc.entityType)
			if baby.EntityID == first.EntityID || baby.EntityID == second.EntityID {
				t.Fatalf("the born %s (id %d) is a parent come again", tc.kind, baby.EntityID)
			}
			if flag := servertest.MetaOf(c.AwaitMetadataFor(baby.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
				t.Errorf("the bred %s is not a baby", tc.kind)
			}
		})
	}
}

// TestBreedingCreditsTheFeeder: the player whose food set the love is
// credited animals_bred when the pair breeds; a pair no player fed credits
// nobody.
// vanilla: Animal.finalizeSpawnChildFromBreeding
func TestBreedingCreditsTheFeeder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fed  int
		want int32
	}{{"both fed", 2, 1}, {"one fed", 1, 1}, {"none fed", 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64)
			srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
			srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
			cows := []int32{c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID, c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID}
			for _, id := range cows[:tc.fed] {
				c.UseOn(id)
				c.AwaitEntityEvent(id, loveHearts)
			}
			if tc.fed < 2 {
				// The unfed one is set loving by the world, with no player behind it.
				srv.SetMobInLove(cows[1], 600)
			}
			if tc.fed == 0 {
				srv.SetMobInLove(cows[0], 600)
			}
			servertest.UntilTicks(t, srv, 400, "calf", func() bool { return srv.MobCount() == 3 })
			if got := srv.StatValue("Rancher", registry.StatCustom, "minecraft:animals_bred"); got != tc.want {
				t.Errorf("animals_bred = %d, want %d", got, tc.want)
			}
		})
	}
}

// loveCauseArg is the UUID as /data prints a LoveCause int array.
func loveCauseArg(id uuid.UUID) protocol.Text {
	return servertest.NBTArg(nbt.Stringify(nbt.UUIDToIntArray(id)))
}

// expectNoLoveCause queries the mob's LoveCause and expects the no-match failure.
func expectNoLoveCause(c *servertest.Client, id uuid.UUID) {
	c.T.Helper()
	c.ChatCommand("data get entity " + id.String() + " LoveCause")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "LoveCause"))
}

// expectLoveCause queries the cow's LoveCause and expects the cause's UUID.
func expectLoveCause(c *servertest.Client, cow servertest.SpawnedMob, cause uuid.UUID) {
	c.T.Helper()
	c.ChatCommand("data get entity " + cow.Uuid.String() + " LoveCause")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid), loveCauseArg(cause))
}

// TestFedCowSavesItsLoveCause: feeding stores the feeder's UUID as LoveCause,
// which /data get reports.
// vanilla: Animal.addAdditionalSaveData
func TestFedCowSavesItsLoveCause(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64, servertest.OpsListing(t, "Rancher"))
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow.EntityID)
	c.Settle()
	c.ChatCommand("data get entity " + cow.Uuid.String() + " LoveCause")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid), loveCauseArg(protocol.OfflineUUID("Rancher")))
}

// TestRemovedLoveCauseClears: /data remove of LoveCause reloads the compound
// without it, so the cause is gone.
// vanilla: DataCommands.removeData, Animal.readAdditionalSaveData
func TestRemovedLoveCauseClears(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64, servertest.OpsListing(t, "Rancher"))
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow.EntityID)
	c.Settle()
	expectLoveCause(c, cow, protocol.OfflineUUID("Rancher"))
	c.ChatCommand("data remove entity " + cow.Uuid.String() + " LoveCause")
	c.ExpectCommandFeedbackText("commands.data.entity.modified",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	expectNoLoveCause(c, cow.Uuid)
}

// TestVillagerHasNoLoveCause: only Animal kinds hold the field.
// vanilla: Animal.addAdditionalSaveData, AbstractVillager
func TestVillagerHasNoLoveCause(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "", 0, servertest.OpsListing(t, "Rancher"))
	srv.StopTick()
	id := protocol.OfflineUUID("Rancher")
	c.ChatCommand("summon villager 1.5 64 2.5 {LoveCause:" + nbt.Stringify(nbt.UUIDToIntArray(id)) + "}")
	villager := c.AwaitSpawn(protocol.EntityTypeVillager)
	c.Settle()
	expectNoLoveCause(c, villager.Uuid)
}

// setLoveCauses summons two cows in love whose causes are the given UUIDs
// (zero for none), returning them once spawned.
func setLoveCauses(t *testing.T, srv *server.Server, c *servertest.Client, causes [2]uuid.UUID) [2]servertest.SpawnedMob {
	t.Helper()
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cows := [2]servertest.SpawnedMob{c.ExpectMobSpawn(protocol.EntityTypeCow), c.ExpectMobSpawn(protocol.EntityTypeCow)}
	for i, cow := range cows {
		compound := "{InLove:600"
		if causes[i] != (uuid.UUID{}) {
			compound += ",LoveCause:" + nbt.Stringify(nbt.UUIDToIntArray(causes[i]))
		}
		c.ChatCommand("data merge entity " + cow.Uuid.String() + " " + compound + "}")
		c.ExpectCommandFeedbackText("commands.data.entity.modified", servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	}
	return cows
}

// TestMergedLoveCauseCreditsTheBreeder: a LoveCause set by /data merge is
// credited with animals_bred when the pair breeds, and breeding resets only
// InLove, so the parents keep the cause.
// vanilla: Animal.readAdditionalSaveData, finalizeSpawnChildFromBreeding
func TestMergedLoveCauseCreditsTheBreeder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "", 0, servertest.OpsListing(t, "Rancher"))
	servertest.HoldTicks(t, srv)
	id := protocol.OfflineUUID("Rancher")
	cows := setLoveCauses(t, srv, c, [2]uuid.UUID{id, id})
	servertest.UntilTicks(t, srv, 400, "calf", func() bool { return srv.MobCount() == 3 })
	if got := srv.StatValue("Rancher", registry.StatCustom, "minecraft:animals_bred"); got != 1 {
		t.Errorf("animals_bred = %d, want 1", got)
	}
	for _, cow := range cows {
		expectLoveCause(c, cow, id)
	}
}

// TestBreedingCreditsThePartnersBreeder: when the first parent's cause is
// not online, the partner's cause is credited.
// vanilla: Animal.getLoveCause, finalizeSpawnChildFromBreeding
func TestBreedingCreditsThePartnersBreeder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "", 0, servertest.OpsListing(t, "Rancher"))
	servertest.HoldTicks(t, srv)
	setLoveCauses(t, srv, c, [2]uuid.UUID{protocol.OfflineUUID("Absent"), protocol.OfflineUUID("Rancher")})
	servertest.UntilTicks(t, srv, 400, "calf", func() bool { return srv.MobCount() == 3 })
	if got := srv.StatValue("Rancher", registry.StatCustom, "minecraft:animals_bred"); got != 1 {
		t.Errorf("animals_bred = %d, want 1", got)
	}
}

// TestFoxKitTrustsOnlyOnlineCauses: a parent's cause that is no online player
// is not trusted by the kit.
// vanilla: Fox.FoxBreedGoal.breed
func TestFoxKitTrustsOnlyOnlineCauses(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "", 0, servertest.OpsListing(t, "Rancher"))
	servertest.HoldTicks(t, srv)
	var foxes [2]servertest.SpawnedMob
	causes := [2]uuid.UUID{protocol.OfflineUUID("Absent"), protocol.OfflineUUID("Rancher")}
	for i := range foxes {
		srv.SpawnMob(foxType, 1.5+float64(i), 64, 2.5)
		foxes[i] = c.ExpectMobSpawn(protocol.EntityTypeFox)
		c.ChatCommand("data merge entity " + foxes[i].Uuid.String() + " {InLove:600,LoveCause:" + nbt.Stringify(nbt.UUIDToIntArray(causes[i])) + "}")
		c.ExpectCommandFeedbackText("commands.data.entity.modified", servertest.EntityDisplayComponent("minecraft:fox", "entity.minecraft.fox", foxes[i].Uuid))
	}
	servertest.UntilTicks(t, srv, 400, "kit", func() bool { return srv.MobCount() == 3 })
	checked := 0
	for _, id := range srv.MobIDsOfKind(foxType) {
		if id == foxes[0].EntityID || id == foxes[1].EntityID {
			continue
		}
		checked++
		kit, _ := srv.MobUUID(id)
		want := nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{nbt.UUIDToIntArray(protocol.OfflineUUID("Rancher"))}}
		c.ChatCommand("data get entity " + kit + " Trusted")
		c.ExpectCommandFeedbackText("commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:fox", "entity.minecraft.fox", uuid.MustParse(kit)), servertest.NBTArg(nbt.Stringify(want)))
	}
	if checked != 1 {
		t.Errorf("found %d kits, want 1", checked)
	}
}

// TestMergedZeroLoveCauseIsKept: an all-zero LoveCause is a stored cause.
// vanilla: Animal.readAdditionalSaveData, UUIDUtil.CODEC
func TestMergedZeroLoveCauseIsKept(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 64, servertest.OpsListing(t, "Rancher"))
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand("data merge entity " + cow.Uuid.String() + " {LoveCause:[I;0,0,0,0]}")
	c.ExpectCommandFeedbackText("commands.data.entity.modified", servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	expectLoveCause(c, cow, uuid.UUID{})
}
