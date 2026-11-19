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

// entityOwnerOf reads a saved Owner int array.
func entityOwnerOf(compound nbt.Compound) (uuid.UUID, bool) {
	words, ok := protocol.UUIDWords(compound["Owner"])
	if !ok {
		return uuid.Nil, false
	}
	return nbt.UUIDFromIntArray(words)
}
