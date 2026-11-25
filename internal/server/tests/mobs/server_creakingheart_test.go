package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The creaking heart's link to its creaking: it stands one up at night beside
// a player, keeps it while the night, the distance and the player's eyes allow,
// and kills it when it breaks.
// vanilla: CreakingHeartBlockEntity

// creakingWorldIn is creakingWorld with the player in the game mode named.
func creakingWorldIn(t *testing.T, mode string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = mode
	srv, c, _ := servertest.JoinAs(t, cfg, creakingPlayer)
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	return srv, c
}

func creakingsOf(srv *server.Server) []server.TestMob {
	var out []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:creaking" {
			out = append(out, m)
		}
	}
	return out
}

// plantHeart builds the log column with an awake heart in its middle three
// blocks east of the player and gives the heart its block entity.
func plantHeart(t *testing.T, srv *server.Server, natural bool) (x, y, z int32) {
	t.Helper()
	pos := srv.PlayerPosition(creakingPlayer)
	x, y, z = int32(pos.X)+3, int32(pos.Y), int32(pos.Z)
	log, _ := registry.FindState("minecraft:pale_oak_log", map[string]string{"axis": "y"})
	heart, _ := registry.FindState("minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "awake", "natural": map[bool]string{true: "true", false: "false"}[natural]})
	srv.SetBlock(x, y, z, log)
	srv.SetBlock(x, y+2, z, log)
	srv.SetBlock(x, y+1, z, heart)
	if !srv.EnsureBlockEntityInDimension(world.DimensionName, x, y+1, z, world.KindCreakingHeart) {
		t.Fatal("the heart keeps no block entity")
	}
	return x, y + 1, z
}

// tickHeart runs n ticks of the block entities and the mobs.
func tickHeart(srv *server.Server, n int) {
	for range n {
		srv.TickBlockEntities()
		srv.TickMobs(1)
	}
}

// awaitCreaking ticks until the heart has stood a creaking up, and returns it.
func awaitCreaking(t *testing.T, srv *server.Server) server.TestMob {
	t.Helper()
	for range 200 {
		tickHeart(srv, 1)
		if got := creakingsOf(srv); len(got) > 0 {
			return got[0]
		}
	}
	t.Fatal("the heart never stood a creaking up")
	return server.TestMob{}
}

// TestAHeartStandsOneCreakingUpAtNightBesideAPlayer: an awake heart with a
// player within 32 blocks stands one bound creaking up through the night, and
// the link keeps it from standing a second; none by day, none while peaceful
// and none for a player 40 blocks off.
// vanilla: CreakingHeartBlockEntity.serverTick
func TestAHeartStandsOneCreakingUpAtNightBesideAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, _, hz := plantHeart(t, srv, false)
	srv.SetDayTime(6000)
	tickHeart(srv, 100)
	if got := creakingsOf(srv); len(got) != 0 {
		t.Fatalf("a heart stood %d creakings up by day", len(got))
	}
	srv.SetDayTime(18000)
	srv.SetDifficulty(0)
	// Mobs are not ticked: a peaceful world would take a stood-up creaking away.
	for range 100 {
		srv.TickBlockEntities()
	}
	if got := creakingsOf(srv); len(got) != 0 {
		t.Fatalf("a heart stood %d creakings up on peaceful", len(got))
	}
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition(creakingPlayer)
	c.MoveTo(float64(hx)+40, pos.Y, float64(hz)+0.5, 0, 0)
	c.Settle()
	tickHeart(srv, 100)
	if got := creakingsOf(srv); len(got) != 0 {
		t.Fatalf("a heart stood %d creakings up for a player 40 blocks away", len(got))
	}
	c.MoveTo(float64(hx)+30, pos.Y, float64(hz)+0.5, 0, 0)
	c.Settle()
	m := awaitCreaking(t, srv)
	if !m.CreakingBound {
		t.Errorf("the heart's creaking is not bound to it")
	}
	tickHeart(srv, 100)
	if got := creakingsOf(srv); len(got) != 1 {
		t.Errorf("%d creakings after 100 more ticks, want the one", len(got))
	}
}

// TestBreakingTheHeartKillsItsCreaking: a player's break of the heart kills
// the creaking, which twitches before it goes.
// vanilla: CreakingHeartBlock.playerWillDestroy, CreakingHeartBlockEntity.removeProtector
func TestBreakingTheHeartKillsItsCreaking(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "creative")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	awaitCreaking(t, srv)
	creakingID := creakingsOf(srv)[0].ID
	pos := srv.PlayerPosition(creakingPlayer)
	srv.MoveMob(creakingID, pos.X+1.5, pos.Z)
	died, twitching := false, false
	sounds, heard := countCreakingSounds(t, "minecraft:entity.creaking.death")
	deathAt := 0
	c.BreakBlock(hx, hy, hz)
	servertest.AwaitBlockUpdate(t, c, hx, hy, hz, 0)
	for tick := range 60 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			sounds(id, data)
			switch id {
			case protocol.IDPlayEntityEvent:
				if r := protocol.NewReader(data); r.ReadI32() == creakingID && r.ReadU8() == protocol.EntityStatusDeath {
					died = true
				}
			case protocol.IDPlayEntityMetadata:
				r := protocol.NewReader(data)
				if r.ReadVarInt() != creakingID {
					return
				}
				for r.ReadU8() != 0xFF {
					if r.ReadVarInt() != protocol.MetadataBoolean {
						return
					}
					if r.ReadBool() {
						twitching = true
					}
				}
			}
		})
		if deathAt == 0 && heard["minecraft:entity.creaking.death"] > 0 {
			deathAt = tick + 1
		}
	}
	if !died {
		t.Errorf("the creaking was taken away without dying")
	}
	if !twitching {
		t.Errorf("the creaking died without the tearing-down flag its twitch plays on")
	}
	// vanilla: Creaking.tearDown plays the death sound once the 45-tick twitch is over.
	if heard["minecraft:entity.creaking.death"] != 1 || deathAt < 45 {
		t.Errorf("the death sound came %d times, first at tick %d, want once after the 45-tick twitch",
			heard["minecraft:entity.creaking.death"], deathAt)
	}
}

// vanilla: CreakingHeartBlock.onExplosionHit
func TestBlastingTheHeartKillsItsCreaking(t *testing.T) {
	t.Parallel()
	srv, _ := creakingWorldIn(t, "creative")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	awaitCreaking(t, srv)
	servertest.HoldTicks(t, srv)

	if srv.SpawnTNT(float64(hx)+1.5, float64(hy), float64(hz)+0.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}
	servertest.UntilTicks(t, srv, 20, "the heart to break", func() bool {
		return registry.BlockOf(srv.BlockAt(hx, hy, hz)) == registry.BlockAir
	})
	servertest.UntilTicks(t, srv, 60, "the creaking to finish tearing down", func() bool {
		return len(creakingsOf(srv)) == 0
	})
}

// TestAHeartsComparatorReadsItsCreakingsDistance: a comparator beside the heart
// reads 15 less a fifteenth of the creaking's distance in 32 blocks, rounded
// down, and nothing without a creaking.
// vanilla: CreakingHeartBlockEntity.computeAnalogOutputSignal
func TestAHeartsComparatorReadsItsCreakingsDistance(t *testing.T) {
	t.Parallel()
	srv, _ := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, false)
	comparator, _ := registry.FindState("minecraft:comparator", map[string]string{"facing": "east"})
	srv.SetBlock(hx-1, hy, hz, comparator)
	srv.EnsureBlockEntityInDimension(world.DimensionName, hx-1, hy, hz, world.KindComparator)
	reading := func() int {
		srv.TickPhysics(6)
		tag, ok := srv.BlockEntityTagAt(hx-1, hy, hz)
		if !ok {
			t.Fatal("the comparator keeps no block entity")
		}
		out, _ := nbt.Get[nbt.Int](tag, "OutputSignal")
		return int(out)
	}
	srv.SetDayTime(18000)
	if got := reading(); got != 0 {
		t.Fatalf("the comparator reads %d with no creaking, want 0", got)
	}
	m := awaitCreaking(t, srv)
	for _, dist := range []float64{0, 14, 30} {
		srv.MoveMob(m.ID, float64(hx)+0.5, float64(hz)+0.5+dist)
		srv.TickMobs(1)
		srv.TickBlockEntities()
		now := creakingByID(t, srv, m.ID)
		d := math.Sqrt(math.Pow(now.X-(float64(hx)+0.5), 2) + math.Pow(now.Y-float64(hy), 2) + math.Pow(now.Z-(float64(hz)+0.5), 2))
		want := 15 - int(math.Floor(d/32*15))
		if got := reading(); got != want {
			t.Errorf("the comparator reads %d with the creaking %.1f blocks off, want %d", got, d, want)
		}
	}
}

// countCreakingSounds returns a collector that counts the named sound events in the
// packets it is handed, and the counts.
func countCreakingSounds(t *testing.T, names ...string) (collect func(int32, []byte), heard map[string]int) {
	t.Helper()
	byID := map[int32]string{}
	for _, name := range names {
		id, ok := registry.SoundEventID(name)
		if !ok {
			t.Fatalf("the sound registry has no %s", name)
		}
		byID[id+1] = name
	}
	heard = map[string]int{}
	return func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		if name, ok := byID[protocol.NewReader(data).ReadVarInt()]; ok {
			heard[name]++
		}
	}, heard
}
