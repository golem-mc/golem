package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"slices"
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

// TestAFoalGrownByFeedingKeepsTheForcedAgeAsCooldown: a foal whose age a
// feeding carries to 0 becomes an adult at its ForcedAge, a breeding
// cooldown during which a golden carrot puts it in no love.
// vanilla: AgeableMob.ageUp, AbstractHorse.handleEating
func TestAFoalGrownByFeedingKeepsTheForcedAgeAsCooldown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Groom", "minecraft:wheat", 1, servertest.OpsListing(t, "Groom"))
	srv.StopTick()
	c.ChatCommand(`summon minecraft:horse 1.5 64 2.5 {Age:-200,ForcedAge:600,Tame:1b,NoAI:1b}`)
	foal := c.ExpectMobSpawn(protocol.EntityTypeHorse)
	if !foal.Baby {
		t.Fatal("the summoned foal is not a baby, want the age read to make it one")
	}

	// Wheat grows a foal 20 seconds, past the 200 ticks it owes.
	c.UseOn(foal.EntityID)
	c.Settle()
	m, ok := mobWithID(srv, foal.EntityID)
	if !ok {
		t.Fatal("the foal is not in the world any more")
	}
	if m.Baby || m.Age != 600 {
		t.Fatalf("the fed foal's age = %d, want grown at its ForcedAge 600", m.Age)
	}

	srv.GiveSlot("Groom", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:golden_carrot"), Count: 1})
	c.Settle()
	c.UseOn(foal.EntityID)
	c.Settle()
	if m, _ := mobWithID(srv, foal.EntityID); m.InLove != 0 {
		t.Errorf("the horse on its 600-tick cooldown loves for %d, want no love", m.InLove)
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

// TestACreativeFeederKeepsItsFood: a creative player's feeding sets the
// love mode all the same — usePlayerItem's consume spares whoever has
// infinite materials — and the wheat is kept whole.
func TestACreativeFeederKeepsItsFood(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Maker", "minecraft:wheat", 2)
	srv.StopTick()
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.UseOn(cow.EntityID)
	c.AwaitEntityEvent(cow.EntityID, loveHearts)
	if got := srv.InventorySlot("Maker", player.HotbarSlot).Count; got != 2 {
		t.Errorf("a creative feeder has %d wheat left, want the 2 kept", got)
	}
	if m, _ := mobWithID(srv, cow.EntityID); m.InLove != 600 {
		t.Errorf("the creatively fed cow loves for %d, want the 600", m.InLove)
	}
}

// TestAPigsEatSoundComesFromItsSet: Pig.playEatingSound reads the eat sound
// from getSoundSet, so a piglet answers with entity.baby_pig.eat and the big
// and mini sound variants with their own, where the plain grown pig's is
// entity.pig.eat (covered above). A piglet's mouthful grows it, so only the
// sound is awaited; the grown variants fall in love and the hearts go out
// before the sound, which awaitSoundNamed reads past.
func TestAPigsEatSoundComesFromItsSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tag  string
		want string
	}{
		{"piglet", "{Age:-24000}", "minecraft:entity.baby_pig.eat"},
		{"big pig", `{sound_variant:"minecraft:big"}`, "minecraft:entity.pig_big.eat"},
		{"mini pig", `{sound_variant:"minecraft:mini"}`, "minecraft:entity.pig_mini.eat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.BreedingPlayer(t, "survival", "Feeder", "minecraft:carrot", 2, servertest.OpsListing(t, "Feeder"))
			srv.StopTick()
			c.ChatCommand("summon minecraft:pig 1.5 64 2.5 " + tc.tag)
			pig := c.ExpectMobSpawn(protocol.EntityTypePig)
			c.UseOn(pig.EntityID)
			c.AwaitSoundNamed(tc.want)
			if got := srv.InventorySlot("Feeder", player.HotbarSlot).Count; got != 1 {
				t.Errorf("the fed pig kept %d carrot, want the 1 left of 2", got)
			}
		})
	}
}

// TestADeadAnimalIsNotFed: a corpse answers a use with PASS, Mob.interact's
// own alive gate — no love shown, no food spent.
func TestADeadAnimalIsNotFed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Mourner", "minecraft:wheat", 3)
	srv.StopTick()
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.KillMob(cow.EntityID) {
		t.Fatal("the planted cow did not answer /kill")
	}

	c.UseOn(cow.EntityID)
	c.Settle()
	if got := srv.InventorySlot("Mourner", player.HotbarSlot).Count; got != 3 {
		t.Errorf("feeding the corpse spent %d wheat, want the 3 kept", 3-got)
	}
	if m, ok := mobWithID(srv, cow.EntityID); !ok || m.InLove != 0 {
		t.Errorf("the corpse loves for %d, want no love", m.InLove)
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

// TestBredOrbNeedsMobDrops: with mob_drops off finalizeSpawnChildFromBreeding
// drops no experience, so the child is the breeding's only arrival.
func TestBredOrbNeedsMobDrops(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 8)
	srv.SetGameRule("mob_drops", false)
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow1 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow2 := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.UseOn(cow1.EntityID)
	c.AwaitEntityEvent(cow1.EntityID, loveHearts)
	c.UseOn(cow2.EntityID)
	c.AwaitEntityEvent(cow2.EntityID, loveHearts)

	kind, _ := c.AwaitNextSpawn()
	if kind != protocol.EntityTypeCow {
		t.Fatalf("with mob_drops off the breeding's first arrival was entity type %d, want the child alone", kind)
	}
}

// TestTheBreedCooldownTicksDown: a bred parent's positive age counts back
// down one a tick, the 6000-tick wait Animal.setAge gives it.
func TestTheBreedCooldownTicksDown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 8)
	srv.SpawnMob("minecraft:cow", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	cow1 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow2 := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cow1.EntityID)
	c.AwaitEntityEvent(cow1.EntityID, loveHearts)
	c.UseOn(cow2.EntityID)
	c.AwaitEntityEvent(cow2.EntityID, loveHearts)
	c.AwaitSpawn(protocol.EntityTypeCow) // the calf, once the parents met
	servertest.UntilTicks(t, srv, 300, "condition", func() bool {
		m, _ := mobWithID(srv, cow1.EntityID)
		return m.Age > 0
	})

	srv.StopTick()
	before, _ := mobWithID(srv, cow1.EntityID)
	srv.TickMobs(200)
	after, _ := mobWithID(srv, cow1.EntityID)
	if before.Age <= 0 || after.Age != before.Age-200 {
		t.Errorf("the cooldown fell from %d to %d over 200 ticks, want it down 200", before.Age, after.Age)
	}
}

// TestBredLambsWearTheirParentsMixedColour: Sheep.getBreedOffspring hands the
// lamb the mixed colour of its parents — DyeColor.getMixedColor, the colour
// the data pack's two-dye crafting mixes the pair's dye items to, red and
// blue wool breeding purple — and where no recipe mixes the pair, blue and
// yellow (no crafting of green out of other dyes), the dice pick one of the
// parents, so the lamb never wears a colour neither parent had.
func TestBredLambsWearTheirParentsMixedColour(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tags      [2]string
		parents   [2]uint8
		want      []uint8
		wantNames string
	}{
		{"red and blue", [2]string{"{Color:14b}", "{Color:11b}"},
			[2]uint8{14, 11}, []uint8{10}, "purple"},
		{"blue and yellow", [2]string{"{Color:11b}", "{Color:4b}"},
			[2]uint8{11, 4}, []uint8{11, 4}, "the blue or the yellow of its parents"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:wheat", 8, servertest.OpsListing(t, "Rancher"))
			c.ChatCommand("summon minecraft:sheep 1.5 64 2.5 " + tc.tags[0])
			first := c.ExpectMobSpawn(protocol.EntityTypeSheep)
			c.ChatCommand("summon minecraft:sheep 2.5 64 2.5 " + tc.tags[1])
			second := c.ExpectMobSpawn(protocol.EntityTypeSheep)
			// The parents wear the colours their tags named, or the mix
			// below would be a mix of something else.
			for i, parent := range []servertest.SpawnedMob{first, second} {
				if !parent.HasSheepByte || parent.SheepByte&0x0f != tc.parents[i] {
					c.T.Fatalf("parent %d wore byte %#02x, want the %s its tag named",
						i+1, parent.SheepByte, servertest.SheepDyeNameOf(tc.parents[i]))
				}
			}

			c.UseOn(first.EntityID)
			c.AwaitEntityEvent(first.EntityID, loveHearts)
			c.UseOn(second.EntityID)
			c.AwaitEntityEvent(second.EntityID, loveHearts)

			// The breeding's orb arrives ahead of the lamb —
			// finalizeSpawnChildFromBreeding's order — and its metadata
			// rides behind the lamb's reading the way any orb's does.
			lamb := c.AwaitSpawn(protocol.EntityTypeSheep)
			if lamb.EntityID == first.EntityID || lamb.EntityID == second.EntityID {
				t.Fatalf("the born lamb (id %d) is a parent come again", lamb.EntityID)
			}
			entries := c.AwaitMetadataFor(lamb.EntityID)
			colourByte := servertest.MetaOf(entries, protocol.MetadataIndexSheepColour)
			if colourByte == nil || !slices.Contains(tc.want, colourByte.ByteVal&0x0f) {
				wore := "white, the byte's own default"
				if colourByte != nil {
					wore = servertest.SheepDyeNameOf(colourByte.ByteVal & 0x0f)
				}
				c.T.Errorf("the lamb of the pair wore %s, want %s", wore, tc.wantNames)
			}
		})
	}
}

// TestTwoBredTurtlesShowNoBreedingHearts: Turtle.TurtleBreedGoal.breed sends
// no love hearts; the orb is the first sign of the breeding, and neither
// parent shows hearts between the feeding and it.
func TestTwoBredTurtlesShowNoBreedingHearts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Shellfeeder", "minecraft:seagrass", 64)
	turtleType, ok := registry.EntityTypeID("minecraft:turtle")
	if !ok {
		t.Fatal("no turtle entity type")
	}
	srv.SpawnMob("minecraft:turtle", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:turtle", 2.5, 64, 2.5)
	t1 := c.ExpectMobSpawn(turtleType)
	t2 := c.ExpectMobSpawn(turtleType)
	c.UseOn(t1.EntityID)
	c.AwaitEntityEvent(t1.EntityID, loveHearts)
	c.UseOn(t2.EntityID)
	c.AwaitEntityEvent(t2.EntityID, loveHearts)
	for packetID, data := range c.Stream() {
		switch packetID {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if id, status := r.ReadI32(), r.ReadU8(); status == loveHearts && (id == t1.EntityID || id == t2.EntityID) {
				t.Fatalf("turtle %d showed love hearts for the breeding", id)
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeExperienceOrb {
				return
			}
		}
	}
	t.Fatal("the turtles never bred")
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
