package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// catYard joins a survival player holding food in the selected slot, with
// the tick loop held, so nothing moves between a test's steps.
func catYard(t *testing.T, name, food string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", name, food, 64)
	servertest.HoldTicks(t, srv)
	return srv, c
}

// advanceCat runs n ticks and folds the metadata updates the cats drew into
// them.
func advanceCat(t *testing.T, srv *server.Server, c *servertest.Client, n int, mobs ...*servertest.SpawnedMob) {
	t.Helper()
	servertest.AwaitTicks(t, srv, uint64(n))
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		entity := protocol.NewReader(data).ReadVarInt()
		for _, m := range mobs {
			if m.EntityID == entity {
				c.ReadMobMetadataInto(m, data)
			}
		}
	})
}

// catDistance is how far the cat with the entity ID stands from (x, z).
func catDistance(t *testing.T, srv *server.Server, id int32, x, z float64) float64 {
	t.Helper()
	cx, _, cz, ok := srv.MobPos(id)
	if !ok {
		t.Fatalf("cat %d is not in the herd", id)
	}
	return math.Hypot(cx-x, cz-z)
}

func spawnCat(t *testing.T, srv *server.Server, c *servertest.Client, x, z float64) servertest.SpawnedMob {
	t.Helper()
	srv.SpawnMob("minecraft:cat", x, 64, z)
	return c.ExpectMobSpawn(protocol.EntityTypeCat)
}

// TestRawFishTamesACatOneTimeInThree: each cod fed to a wild cat is spent
// and answers with the smoke (status 6) or the hearts (status 7); a tamed
// cat belongs to the feeder and sits.
// vanilla: Cat.mobInteract, Cat.tryToTame
func TestRawFishTamesACatOneTimeInThree(t *testing.T) {
	t.Parallel()
	const name = "Tamer"
	srv, c := catYard(t, name, "minecraft:cod")
	const cats = 12
	var mobs []servertest.SpawnedMob
	for i := range cats {
		angle := float64(i) * 2 * math.Pi / cats
		mobs = append(mobs, spawnCat(t, srv, c, 0.5+2*math.Cos(angle), 0.5+2*math.Sin(angle)))
	}
	for _, m := range mobs {
		c.UseOn(m.EntityID)
	}
	status := map[int32]uint8{}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		entity, event := r.ReadI32(), r.ReadU8()
		if event == protocol.EntityEventTamed || event == protocol.EntityEventTameFailed {
			status[entity] = event
		}
	})
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 64-cats {
		t.Errorf("%d cod left after feeding %d cats once each, want %d", got, cats, 64-cats)
	}
	tamed := 0
	var told []*servertest.SpawnedMob
	for i := range mobs {
		told = append(told, &mobs[i])
	}
	advanceCat(t, srv, c, 4, told...)
	for i := range mobs {
		m := &mobs[i]
		ev, ok := status[m.EntityID]
		if !ok {
			t.Fatalf("cat %d answered its fish with neither smoke nor hearts", i)
		}
		isTame := m.TamableFlags&protocol.TamableTame != 0
		if isTame != (ev == protocol.EntityEventTamed) {
			t.Errorf("cat %d: event %d but tame flag %v", i, ev, isTame)
		}
		if isTame {
			tamed++
			if m.Owner != protocol.OfflineUUID(name) {
				t.Errorf("cat %d is owned by %v, want its feeder", i, m.Owner)
			}
			if m.TamableFlags&protocol.TamableSitting == 0 {
				t.Errorf("cat %d tamed but not sitting (flags %#x)", i, m.TamableFlags)
			}
		} else if m.Owner != uuid.Nil || m.TamableFlags != 0 {
			t.Errorf("cat %d refused the fish yet shows owner %v flags %#x", i, m.Owner, m.TamableFlags)
		}
	}
	if tamed == 0 || tamed == cats {
		t.Errorf("%d of %d cats tamed, want some but not all at one in three", tamed, cats)
	}
}

// TestACatIgnoresFoodItDoesNotEat: a wild cat fed a bone is neither tamed
// nor fed.
func TestACatIgnoresFoodItDoesNotEat(t *testing.T) {
	t.Parallel()
	const name = "Tamer"
	srv, c := catYard(t, name, "minecraft:bone")
	cat := spawnCat(t, srv, c, 2.5, 0.5)
	for range 6 {
		c.UseOn(cat.EntityID)
	}
	advanceCat(t, srv, c, 2, &cat)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 64 {
		t.Errorf("%d bones left, want all 64 untouched", got)
	}
	if cat.TamableFlags != 0 {
		t.Errorf("tame flags %#x after bones, want none", cat.TamableFlags)
	}
}

// TestAnOwnerSitsAndStandsTheirCat: an empty-handed click by the owner flips
// the sit order; anyone else's click does nothing.
// vanilla: Cat.mobInteract
func TestAnOwnerSitsAndStandsTheirCat(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	mine := spawnCat(t, srv, c, 2.5, 0.5)
	srv.SetMobOwner(mine.EntityID, protocol.OfflineUUID(name))
	other := spawnCat(t, srv, c, -1.5, 0.5)
	srv.SetMobOwner(other.EntityID, protocol.OfflineUUID("Stranger"))
	advanceCat(t, srv, c, 4, &mine)

	c.UseOn(mine.EntityID)
	c.UseOn(other.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 4, &mine, &other)
	if mine.TamableFlags&protocol.TamableSitting == 0 {
		t.Errorf("the owner's click left the cat standing (flags %#x)", mine.TamableFlags)
	}
	if other.TamableFlags&protocol.TamableSitting != 0 {
		t.Error("a stranger's click sat down someone else's cat")
	}
	c.UseOn(mine.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 4, &mine)
	if mine.TamableFlags&protocol.TamableSitting != 0 {
		t.Errorf("the second click left the cat sitting (flags %#x)", mine.TamableFlags)
	}
}

// TestAHurtTameCatEatsFishAndAHealthyOneLoves: the owner's fish heals a
// hurt cat by its nutrition; at full health it starts the cat's love.
// vanilla: Cat.mobInteract, TamableAnimal.feed
func TestAHurtTameCatEatsFishAndAHealthyOneLoves(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "minecraft:cod")
	cat := spawnCat(t, srv, c, 2.5, 0.5)
	srv.SetMobOwner(cat.EntityID, protocol.OfflineUUID(name))
	srv.SetMobHealth(cat.EntityID, 4)

	c.UseOn(cat.EntityID)
	c.Settle()
	if h, _ := srv.MobHealth(cat.EntityID); h != 6 {
		t.Errorf("health %v after a cod, want 4 + its nutrition of 2", h)
	}
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 63 {
		t.Errorf("%d cod left, want 63", got)
	}
	if m, _ := mobWithID(srv, cat.EntityID); m.InLove != 0 {
		t.Error("a hurt cat fell in love instead of eating")
	}
	srv.SetMobHealth(cat.EntityID, 10)
	c.UseOn(cat.EntityID)
	c.AwaitEntityEvent(cat.EntityID, loveHearts)
	if m, _ := mobWithID(srv, cat.EntityID); m.InLove != 600 {
		t.Errorf("love %d for a healthy cat fed fish, want 600", m.InLove)
	}
}

// TestATameCatFollowsItsOwnerUnlessSitting: an owner ten blocks off is
// walked to, past twelve the cat teleports beside them, and a sitting cat
// stays put.
// vanilla: FollowOwnerGoal, TamableAnimal.tryToTeleportToOwner
func TestATameCatFollowsItsOwnerUnlessSitting(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	walker := spawnCat(t, srv, c, 1.5, 2.5)
	sitter := spawnCat(t, srv, c, -1.5, 2.5)
	jumper := spawnCat(t, srv, c, 3.5, -2.5)
	for _, m := range []servertest.SpawnedMob{walker, sitter, jumper} {
		srv.SetMobOwner(m.EntityID, protocol.OfflineUUID(name))
	}
	c.UseOn(sitter.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 4, &sitter)
	sx, _, sz, _ := srv.MobPos(sitter.EntityID)

	c.MoveToGround(11.5, 64, 0.5, true)
	c.Settle()
	// The cat may stroll off again once it has arrived, so the closest
	// approach counts.
	closest := math.Inf(1)
	for range 8 {
		servertest.AwaitTicks(t, srv, 10)
		closest = min(closest, catDistance(t, srv, walker.EntityID, 11.5, 0.5))
	}
	if closest > 7 {
		t.Errorf("the following cat came no nearer than %.1f blocks to its owner, want it walked close", closest)
	}
	if x, _, z, _ := srv.MobPos(sitter.EntityID); x != sx || z != sz {
		t.Errorf("the sitting cat moved from (%.1f,%.1f) to (%.1f,%.1f)", sx, sz, x, z)
	}

	c.MoveToGround(20.5, 64, 0.5, true)
	c.Settle()
	servertest.AwaitTicks(t, srv, 30)
	if d := catDistance(t, srv, jumper.EntityID, 20.5, 0.5); d > 4.5 {
		t.Errorf("the far cat stands %.1f blocks from its owner, want a teleport to within three", d)
	}
}

// TestAWildCatRunsFromAPlayerButATameOneStays: a wild cat flees a player in
// sight at sixteen blocks; a tame one does not.
// vanilla: Cat.CatAvoidEntityGoal
func TestAWildCatRunsFromAPlayerButATameOneStays(t *testing.T) {
	t.Parallel()
	const name = "Walker"
	srv, c := catYard(t, name, "")
	wild := spawnCat(t, srv, c, 4.5, 0.5)
	tame := spawnCat(t, srv, c, -4.5, 0.5)
	srv.SetMobOwner(tame.EntityID, protocol.OfflineUUID(name))
	servertest.AwaitTicks(t, srv, 40)
	if d := catDistance(t, srv, wild.EntityID, 0.5, 0.5); d < 8 {
		t.Errorf("the wild cat is %.1f blocks from the player after 40 ticks, want it fled to 8", d)
	}
	if d := catDistance(t, srv, tame.EntityID, -4.5, 0.5); d > 3 {
		t.Errorf("the tame cat wandered %.1f blocks, want it to stay by the player", d)
	}
}

// TestCatsSpawnInSomeVariantButAllBlackUnderADimMoon: under a new moon a
// spawned cat wears one of the ten variants with an unconditional spawn
// condition, never all_black, and the ten turn up across a crowd.
// vanilla: Cat.finalizeSpawn, VariantUtils.selectVariantToSpawn
func TestCatsSpawnInSomeVariantButAllBlackUnderADimMoon(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Watcher", "")
	srv.SetDayTime(4 * 24000)
	seen := map[int32]bool{}
	for i := range 40 {
		cat := spawnCat(t, srv, c, float64(i%8)*2-8, float64(i/8)*2-4)
		if cat.Variant == 0 {
			t.Fatalf("cat %d spawned all_black under a new moon", i)
		}
		seen[cat.Variant] = true
	}
	if len(seen) < 5 {
		t.Errorf("%d distinct variants among 40 cats (%v), want a spread", len(seen), seen)
	}
}

// TestAllBlackCatsSpawnUnderAFullMoon: with the variant dice fixed, a cat that
// draws all_black under a full moon draws something else under a dim one.
// vanilla: CatVariants.bootstrap, MoonBrightnessCheck
func TestAllBlackCatsSpawnUnderAFullMoon(t *testing.T) {
	t.Parallel()
	// Seed 4 draws all_black among eleven variants and, with all_black out
	// of the draw, white among ten.
	const seed = 4
	for _, tc := range []struct {
		day      int64
		allBlack bool
	}{{8, true}, {4, false}} {
		srv, c := catYard(t, "Watcher", "")
		srv.SetDayTime(tc.day * 24000)
		srv.SpawnMobSeeded("minecraft:cat", 0.5, 64, 4.5, seed)
		if got := c.ExpectMobSpawn(protocol.EntityTypeCat).Variant == 0; got != tc.allBlack {
			t.Errorf("day %d: all_black %v, want %v", tc.day, got, tc.allBlack)
		}
	}
}

// saveAndReload summons one mob by the command, saves it with its chunk, and
// hands the saved compound and the mob that joins back to the checks.
func saveAndReload(t *testing.T, command string, typeID int32, id string, saved func(nbt.Compound), back func(servertest.SpawnedMob)) {
	t.Helper()
	const name = "Keeper"
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, name))
	c := connect().Join(name)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand(command)
	c.ExpectMobSpawn(typeID)
	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, id, saved)

	second := connect()
	second.Login(name)
	second.Configure()
	mob, ok := second.EnterWorldSeeingMob(name, typeID)
	if !ok {
		t.Fatalf("the %s was not restored with its chunk", id)
	}
	back(mob)
}

// TestATameCatSurvivesASaveAndReload: the owner, the sit order and the variant
// are written as vanilla's save data names them and come back with the chunk.
// vanilla: TamableAnimal.addAdditionalSaveData, VariantUtils.writeVariant
func TestATameCatSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	owner := protocol.OfflineUUID("Keeper")
	words := nbt.UUIDToIntArray(owner)
	command := fmt.Sprintf("summon minecraft:cat 4.5 64 5.5 {Owner:[I;%d,%d,%d,%d],Sitting:1b,variant:\"minecraft:red\"}",
		words[0], words[1], words[2], words[3])
	saveAndReload(t, command, protocol.EntityTypeCat, "minecraft:cat", func(compound nbt.Compound) {
		if got, ok := entityOwnerOf(compound); !ok || got != owner {
			t.Errorf("saved Owner = %v, want %v", compound["Owner"], owner)
		}
		if got := compound["Sitting"]; got != nbt.Byte(1) {
			t.Errorf("saved Sitting = %v, want 1b", got)
		}
		if got := compound["variant"]; got != nbt.String("minecraft:red") {
			t.Errorf("saved variant = %v, want minecraft:red", got)
		}
	}, func(cat servertest.SpawnedMob) {
		if cat.Owner != owner || cat.TamableFlags&protocol.TamableTame == 0 {
			t.Errorf("restored cat owner %v flags %#x, want tame under %v", cat.Owner, cat.TamableFlags, owner)
		}
		if cat.Variant != 7 {
			t.Errorf("restored cat variant %d, want red (7)", cat.Variant)
		}
	})
}

// TestATrustingOcelotSurvivesASaveAndReload: the trust is saved as the
// Trusting byte and comes back.
// vanilla: Ocelot.addAdditionalSaveData
func TestATrustingOcelotSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	saveAndReload(t, "summon minecraft:ocelot 4.5 64 5.5 {Trusting:1b}", protocol.EntityTypeOcelot, "minecraft:ocelot",
		func(compound nbt.Compound) {
			if got := compound["Trusting"]; got != nbt.Byte(1) {
				t.Errorf("saved Trusting = %v, want 1b", got)
			}
		}, func(ocelot servertest.SpawnedMob) {
			if !ocelot.Trusting {
				t.Error("restored ocelot is not trusting")
			}
		})
}

// entityOwnerOf reads a saved Owner int array.
func entityOwnerOf(compound nbt.Compound) (uuid.UUID, bool) {
	words, ok := protocol.UUIDWords(compound["Owner"])
	if !ok {
		return uuid.Nil, false
	}
	return nbt.UUIDFromIntArray(words)
}

// TestATemptedCatCrouchesAndAFleeingOneSprints: a wild cat walking to a
// player's fish is told in the crouching pose, and one running from a
// player within seven blocks is told sprinting.
// vanilla: Cat.customServerAiStep
func TestATemptedCatCrouchesAndAFleeingOneSprints(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Lure", "minecraft:cod")
	lured := spawnCat(t, srv, c, 6.5, 0.5)
	advanceCat(t, srv, c, 8, &lured)
	if lured.Pose != protocol.PoseSneaking {
		t.Errorf("lured cat pose %d, want %d (crouching)", lured.Pose, protocol.PoseSneaking)
	}
	if lured.BaseFlags&protocol.EntityFlagSprinting != 0 {
		t.Error("lured cat is told sprinting")
	}

	srv2, c2 := catYard(t, "Walker", "")
	fleeing := spawnCat(t, srv2, c2, 4.5, 0.5)
	advanceCat(t, srv2, c2, 8, &fleeing)
	if fleeing.BaseFlags&protocol.EntityFlagSprinting == 0 {
		t.Error("cat fleeing a player 4 blocks away is not told sprinting")
	}
	if fleeing.Pose == protocol.PoseSneaking {
		t.Error("fleeing cat is told crouching")
	}
}

// summonClassicCat summons a cat with the classic sound set, so its sound
// names are fixed, and reads its spawn.
func summonClassicCat(t *testing.T, srv *server.Server, c *servertest.Client, x, z float64, tags string) servertest.SpawnedMob {
	t.Helper()
	srv.RunLine(fmt.Sprintf("summon minecraft:cat %v 64 %v {sound_variant:\"minecraft:classic\"%s}", x, z, tags))
	return c.ExpectMobSpawn(protocol.EntityTypeCat)
}

// ownerTag is the Owner field naming the player.
func ownerTag(name string) string {
	w := nbt.UUIDToIntArray(protocol.OfflineUUID(name))
	return fmt.Sprintf(",Owner:[I;%d,%d,%d,%d]", w[0], w[1], w[2], w[3])
}

// TestATameCatInLovePurrs: a tame cat in love purrs for its idle sound
// instead of its ambient meow.
// vanilla: Cat.getAmbientSound
func TestATameCatInLovePurrs(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	cat := summonClassicCat(t, srv, c, 2.5, 0.5, ownerTag(name)+",Sitting:1b")
	srv.SetMobInLove(cat.EntityID, 600)
	var sounds []string
	if !c.ReadTicks(srv, 590, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		s := servertest.SoundOf(data)
		sounds = append(sounds, s)
		return s == "minecraft:entity.cat.purr"
	}) {
		t.Fatalf("a tame cat in love never purred in 590 ticks; heard %v", sounds)
	}
	for _, s := range sounds {
		if s == "minecraft:entity.cat.ambient" || s == "minecraft:entity.cat.purreow" {
			t.Errorf("a tame cat in love played %s; heard %v", s, sounds)
		}
	}
}

// TestATemptedStrayCatBegs: a wild cat lured by a player's cod begs for it.
// vanilla: Cat.tick
func TestATemptedStrayCatBegs(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Lure", "minecraft:cod")
	summonClassicCat(t, srv, c, 3.5, 0.5, "")
	var sounds []string
	if !c.ReadTicks(srv, 220, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		s := servertest.SoundOf(data)
		sounds = append(sounds, s)
		return s == "minecraft:entity.cat.beg_for_food"
	}) {
		t.Errorf("a tempted stray never begged in 220 ticks; heard %v", sounds)
	}
}

// TestACatHissesAtASwoopingPhantom: the phantom swooping a survival player
// finds the cat beside them and the cat hisses. The bound is the bite's
// in TestPhantomBitesItsTarget: the phantom circles for a random while first.
// vanilla: Phantom.PhantomSweepAttackGoal.canContinueToUse, Cat.hiss
func TestACatHissesAtASwoopingPhantom(t *testing.T) {
	t.Parallel()
	srv, c := phantomTests(t)
	srv.RunLine("summon minecraft:cat 4.5 64 0.5 {sound_variant:\"minecraft:classic\",NoAI:1b}")
	srv.SpawnMob("minecraft:phantom", 0.5, 84, 0.5)
	for range 120 {
		srv.TickMobs(10)
		hissed := false
		c.SettleAndCollect(func(id int32, data []byte) {
			hissed = hissed || id == protocol.IDPlaySoundEffect && servertest.SoundOf(data) == "minecraft:entity.cat.hiss"
		})
		if hissed {
			return
		}
	}
	t.Error("the cat never hissed at the phantom in 1200 ticks")
}
