package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
