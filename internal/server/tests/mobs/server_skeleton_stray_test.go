package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// A skeleton kept in powder snow shakes after 140 ticks and, 300 ticks on,
// turns into a stray.
// vanilla: Skeleton.tick, Skeleton.doFreezeConversion

const (
	strayPitX, strayPitZ = 10, 10
	// Skeleton.tick reads the snow the previous tick found, so the counter
	// starts on the second tick: it shakes on the 142nd and converts 300
	// ticks and one more on.
	shakeAfter   = 142
	convertAfter = 443
)

// startStrayPit starts a creative-mode world and holds the pit's chunk, then
// fills a three-by-three pit two deep with powder snow over a stone floor,
// at midnight so the sun burns nobody. The player is far from it, so the
// skeleton has no target.
func startStrayPit(t *testing.T) (*server.Server, *servertest.Client, string) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Watcher"))
	srv.SetSpawning(false)
	c := connect().Join("Watcher")
	c.Settle()
	srv.StopTick()
	t.Cleanup(srv.HoldChunk(strayPitX, strayPitZ))
	srv.SetPaused(true)
	srv.SetDayTime(18000)
	snow := servertest.MustState(t, "minecraft:powder_snow", nil)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(strayPitX - 2); x <= strayPitX+2; x++ {
		for z := int32(strayPitZ - 2); z <= strayPitZ+2; z++ {
			srv.SetBlock(x, 63, z, stone)
			if x == strayPitX-2 || x == strayPitX+2 || z == strayPitZ-2 || z == strayPitZ+2 {
				// The walls keep a body that wades through the snow in it.
				for y := int32(64); y <= 67; y++ {
					srv.SetBlock(x, y, z, stone)
				}
				continue
			}
			srv.SetBlock(x, 64, z, snow)
			srv.SetBlock(x, 65, z, snow)
		}
	}
	if srv.BlockAt(strayPitX, 64, strayPitZ) != snow {
		t.Fatal("the pit's chunk was not loaded")
	}
	return srv, c, cfg.LevelName
}

func kindsOf(srv *server.Server) map[string]int32 {
	kinds := map[string]int32{}
	for _, m := range srv.Mobs() {
		kinds[m.Kind] = m.ID
	}
	return kinds
}

// shakingOf drains the packets sent so far and reports the last shaking
// boolean told for the entity, and whether the 1048 sound played.
func shakingOf(c *servertest.Client, id int32) (shaking, told, sound bool) {
	c.T.Helper()
	c.SettleAndCollect(func(pid int32, data []byte) {
		switch pid {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			entries, _ := servertest.ReadMetadata(r)
			for _, e := range entries {
				if e.Index == protocol.MetadataIndexSkeletonStraying && e.Typ == protocol.MetadataBoolean {
					shaking, told = e.FlagVal, true
				}
			}
		case protocol.IDPlayWorldEvent:
			if protocol.NewReader(data).ReadI32() == protocol.WorldEventSkeletonToStray {
				sound = true
			}
		}
	})
	return shaking, told, sound
}

// TestASkeletonInPowderSnowTurnsIntoAStray checks the whole conversion: not
// shaking before 140 ticks, shaking after, a stray holding the skeleton's
// bow 300 ticks later with the conversion sound.
func TestASkeletonInPowderSnowTurnsIntoAStray(t *testing.T) {
	t.Parallel()
	srv, c, _ := startStrayPit(t)
	id := srv.SpawnMob("minecraft:skeleton", strayPitX+0.5, 64, strayPitZ+0.5)
	bow := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1}
	srv.SetMobHand(id, bow)

	srv.TickMobs(shakeAfter - 2)
	if shaking, told, _ := shakingOf(c, id); told && shaking {
		t.Fatal("the skeleton shook before 140 ticks in the snow")
	}
	srv.TickMobs(2)
	if shaking, told, _ := shakingOf(c, id); !told || !shaking {
		t.Fatalf("after %d ticks in the snow shaking=%v told=%v, want a shaking skeleton", shakeAfter, shaking, told)
	}
	srv.TickMobs(convertAfter - shakeAfter - 2)
	if kinds := kindsOf(srv); kinds["minecraft:stray"] != 0 {
		t.Fatal("the skeleton became a stray before its 300 ticks were up")
	}
	srv.TickMobs(2)
	kinds := kindsOf(srv)
	if kinds["minecraft:skeleton"] != 0 || kinds["minecraft:stray"] == 0 {
		t.Fatalf("after the conversion the mobs are %v, want one stray and no skeleton", kinds)
	}
	if got := srv.MobHand(kinds["minecraft:stray"]); got.ItemID != bow.ItemID {
		t.Errorf("the stray holds %v, want the skeleton's bow", got)
	}
	if _, _, sound := shakingOf(c, id); !sound {
		t.Error("the conversion played no 1048 sound")
	}
}

// TestASilentSkeletonConvertsWithoutTheSound: Skeleton.doFreezeConversion
// plays the sound only for a body that is not silent.
func TestASilentSkeletonConvertsWithoutTheSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := startStrayPit(t)
	id := srv.SpawnMob("minecraft:skeleton", strayPitX+0.5, 64, strayPitZ+0.5)
	srv.SetMobSilent(id, true)
	srv.TickMobs(convertAfter + 2)
	if kindsOf(srv)["minecraft:stray"] == 0 {
		t.Fatal("the silent skeleton never converted")
	}
	if _, _, sound := shakingOf(c, id); sound {
		t.Error("the silent skeleton's conversion played the 1048 sound")
	}
}

// TestLeavingThePowderSnowCancelsTheConversion: once the snow is gone the
// shaking stops on the next tick and the skeleton never converts.
func TestLeavingThePowderSnowCancelsTheConversion(t *testing.T) {
	t.Parallel()
	srv, c, _ := startStrayPit(t)
	id := srv.SpawnMob("minecraft:skeleton", strayPitX+0.5, 64, strayPitZ+0.5)
	srv.TickMobs(shakeAfter + 5)
	if shaking, _, _ := shakingOf(c, id); !shaking {
		t.Fatal("the skeleton was not shaking in the snow")
	}
	for x := int32(strayPitX - 1); x <= strayPitX+1; x++ {
		for z := int32(strayPitZ - 1); z <= strayPitZ+1; z++ {
			srv.SetBlock(x, 64, z, 0)
			srv.SetBlock(x, 65, z, 0)
		}
	}
	srv.TickMobs(5)
	if shaking, told, _ := shakingOf(c, id); !told || shaking {
		t.Errorf("out of the snow shaking=%v told=%v, want the shaking told off", shaking, told)
	}
	srv.TickMobs(convertAfter)
	if kinds := kindsOf(srv); kinds["minecraft:stray"] != 0 || kinds["minecraft:skeleton"] == 0 {
		t.Errorf("the skeleton that left the snow is now %v, want it still a skeleton", kinds)
	}
}

// TestANoAISkeletonDoesNotConvert: Skeleton.tick converts only a body with
// its AI.
func TestANoAISkeletonDoesNotConvert(t *testing.T) {
	t.Parallel()
	srv, _, _ := startStrayPit(t)
	id := srv.SpawnMob("minecraft:skeleton", strayPitX+0.5, 64, strayPitZ+0.5)
	srv.SetMobNoAI(id)
	srv.TickMobs(convertAfter + 20)
	if kinds := kindsOf(srv); kinds["minecraft:stray"] != 0 {
		t.Errorf("a NoAI skeleton turned into a stray: %v", kinds)
	}
}

// TestTheStrayConversionTimerIsSaved: a shaking skeleton saves its time left
// under StrayConversionTime, and a skeleton loaded with one finishes it.
func TestTheStrayConversionTimerIsSaved(t *testing.T) {
	t.Parallel()
	srv, c, level := startStrayPit(t)
	id := srv.SpawnMob("minecraft:skeleton", strayPitX+0.5, 64, strayPitZ+0.5)
	srv.TickMobs(shakeAfter + 10)
	if shaking, _, _ := shakingOf(c, id); !shaking {
		t.Fatal("the skeleton was not shaking")
	}
	srv.FlushMobs()
	srv.Shutdown()
	pos := world.ChunkPos{X: world.ChunkOf(strayPitX), Z: world.ChunkOf(strayPitZ)}
	servertest.EntityFileIs(t, level, pos, "minecraft:skeleton", func(compound nbt.Compound) {
		got, ok := nbt.Get[nbt.Int](compound, "StrayConversionTime")
		if !ok || got <= 0 || got >= 300 {
			t.Errorf("StrayConversionTime = %v (present %v), want the ticks left under 300", got, ok)
		}
		if got, ok := nbt.Get[nbt.Int](compound, "FreezingTime"); !ok || got != 140 {
			t.Errorf("FreezingTime = %v (present %v), want 140", got, ok)
		}
	})
}

// TestASkeletonLoadsItsStrayConversionTime: a skeleton saved with a
// StrayConversionTime is shown shaking when it comes back.
// vanilla: Skeleton.readAdditionalSaveData
func TestASkeletonLoadsItsStrayConversionTime(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(7, 7, 7, 7)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{}, nbt.Compound{
		"id":                  nbt.String("minecraft:skeleton"),
		"UUID":                servertest.UuidWords(id),
		"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5)}},
		"Health":              nbt.Float(20),
		"PersistenceRequired": nbt.Byte(1),
		"StrayConversionTime": nbt.Int(120),
	})
	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	shown := c.JoinSeeingMobs("Keeper", id)
	m := servertest.MetaOf(shown[id].Meta, protocol.MetadataIndexSkeletonStraying)
	if m == nil || !m.FlagVal {
		t.Errorf("the loaded skeleton's shaking field = %+v, want true", m)
	}
}

// TestOnlyASkeletonConvertsInPowderSnow: a stray kept in powder snow past the
// conversion time stays a stray and never shakes. The server has no wither
// skeleton to test.
// vanilla: Skeleton.tick
func TestOnlyASkeletonConvertsInPowderSnow(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:stray"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := startStrayPit(t)
			id := srv.SpawnMob(kind, strayPitX+0.5, 64, strayPitZ+0.5)
			srv.TickMobs(convertAfter + 2)
			kinds := kindsOf(srv)
			if kinds[kind] == 0 || len(kinds) != 1 {
				t.Fatalf("after the conversion time the mobs are %v, want only the %s", kinds, kind)
			}
			if shaking, _, sound := shakingOf(c, id); shaking || sound {
				t.Errorf("the %s shook=%v sound=%v in powder snow", kind, shaking, sound)
			}
		})
	}
}

// TestASkeletonInPowderSnowDoesNotFreeze: Skeleton.canFreeze is false, so the
// skeleton gathers no frozen ticks and takes no freeze damage while it
// converts; a bogged in the same pit does freeze.
// vanilla: Skeleton.canFreeze, LivingEntity.aiStep
func TestASkeletonInPowderSnowDoesNotFreeze(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind   string
		frozen bool
	}{{"minecraft:skeleton", false}, {"minecraft:bogged", true}} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := startStrayPit(t)
			id := srv.SpawnMob(tc.kind, strayPitX+0.5, 64, strayPitZ+0.5)
			srv.TickMobs(shakeAfter + 200)
			var frozen int
			for _, m := range srv.Mobs() {
				if m.ID == id {
					frozen = m.Frozen
				}
			}
			if (frozen > 0) != tc.frozen {
				t.Errorf("%s Frozen=%d, want frozen=%v", tc.kind, frozen, tc.frozen)
			}
			if hp, _ := srv.MobHealth(id); !tc.frozen && hp != 20 {
				t.Errorf("%s health %v, want 20", tc.kind, hp)
			}
		})
	}
}
