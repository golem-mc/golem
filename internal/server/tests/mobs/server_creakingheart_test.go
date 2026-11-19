package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

// hasCreaking reports whether the creaking with the ID is still in the world.
func hasCreaking(srv *server.Server, id int32) bool {
	for _, m := range creakingsOf(srv) {
		if m.ID == id {
			return true
		}
	}
	return false
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

// TestAHeartLetsItsCreakingGoByDayAndPastThirtyFourBlocks: the link is dropped
// when the night ends or the creaking strays beyond 34 blocks, the body taken
// away; one within 34 stays.
// vanilla: CreakingHeartBlockEntity.serverTick
func TestAHeartLetsItsCreakingGoByDayAndPastThirtyFourBlocks(t *testing.T) {
	t.Parallel()
	srv, _ := creakingWorldIn(t, "survival")
	hx, _, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	srv.MoveMob(m.ID, float64(hx)+0.5+33, float64(hz)+0.5)
	tickHeart(srv, 60)
	if !hasCreaking(srv, m.ID) {
		t.Fatalf("the creaking 33 blocks from its heart was let go, want it kept")
	}
	srv.MoveMob(m.ID, float64(hx)+0.5+36, float64(hz)+0.5)
	tickHeart(srv, 60)
	if hasCreaking(srv, m.ID) {
		t.Fatalf("the creaking 36 blocks from its heart was kept, want it let go")
	}

	// The heart stands a new one up at night; by day it lets that one go.
	next := awaitCreaking(t, srv)
	srv.SetDayTime(6000)
	tickHeart(srv, 60)
	if hasCreaking(srv, next.ID) {
		t.Errorf("the creaking was kept by day, want it let go")
	}
}

// TestACreakingStopsAtTheEdgeOfItsHeartsReach: a hunting creaking's path
// does not run past 32 blocks from its heart, so it stays alive within the
// heart's 34-block check.
// vanilla: Creaking.HomeNodeEvaluator.getPathType
func TestACreakingStopsAtTheEdgeOfItsHeartsReach(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, _, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	srv.MoveMob(m.ID, float64(hx)+10.5, float64(hz)+0.5)
	srv.TickMobs(1)
	m = creakingByID(t, srv, m.ID)
	c.MoveTo(m.X-1.2, m.Y, m.Z, -90, 0)
	c.Settle()
	c.Attack(m.ID)
	tickHeart(srv, 2)
	// 30 blocks from the creaking: inside its follow range.
	c.MoveTo(float64(hx)+40.5, m.Y, float64(hz)+0.5, -90, 0)
	c.Settle()
	for range 300 {
		tickHeart(srv, 1)
		c.Settle()
	}
	if !hasCreaking(srv, m.ID) {
		t.Fatalf("the creaking was torn down chasing a player past its heart's reach")
	}
	got := creakingByID(t, srv, m.ID)
	if dx, dz := got.X-(float64(hx)+0.5), got.Z-(float64(hz)+0.5); dx*dx+dz*dz > 33*33 {
		t.Errorf("the creaking is %.1f blocks from its heart, want within 33", math.Hypot(dx, dz))
	}
}

// TestAHeartLetsGoOfACreakingAPlayerStandsInside: a player's eyes inside the
// body for more than four of the heart's checks take it away, and a player
// beside it do not.
// vanilla: Creaking.playerIsStuckInYou
func TestAHeartLetsGoOfACreakingAPlayerStandsInside(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	// The creaking wanders, so the player follows it each tick: beside it
	// first, then with the eyes inside.
	follow := func(ticks int, dx float64) {
		for range ticks {
			if !hasCreaking(srv, m.ID) {
				return
			}
			now := creakingByID(t, srv, m.ID)
			c.MoveTo(now.X+dx, now.Y, now.Z, 0, 0)
			c.Settle()
			tickHeart(srv, 1)
		}
	}
	follow(150, 2)
	if !hasCreaking(srv, m.ID) {
		t.Fatalf("the creaking was let go with the player beside it, want it kept")
	}
	// Four checks 20 to 24 ticks apart leave it; the fifth takes it.
	follow(4*20-5, 0)
	if !hasCreaking(srv, m.ID) {
		t.Fatalf("the creaking was let go before five checks with the player inside, want it kept")
	}
	follow(2*24+5, 0)
	if hasCreaking(srv, m.ID) {
		t.Errorf("the creaking was kept through five checks with the player inside, want it let go")
	}
}

// TestABoundCreakingTakesNoDamageAndSendsParticlesToItsHeart: a player's blow
// on the heart's creaking leaves its health, starts the particle trail and
// grows resin on the logs.
// vanilla: Creaking.hurtServer, CreakingHeartBlockEntity.creakingHurt
func TestABoundCreakingTakesNoDamageAndSendsParticlesToItsHeart(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	srv.MoveMob(m.ID, float64(hx)+4.5, float64(hz)+0.5)
	srv.TickMobs(1)
	m = creakingByID(t, srv, m.ID)
	c.MoveTo(m.X-1.2, m.Y, m.Z, -90, 0)
	c.Settle()
	trail, hidden := 0, 0
	swayed := false
	c.Attack(m.ID)
	for range 110 {
		tickHeart(srv, 1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayEntityEvent {
				if r := protocol.NewReader(data); r.ReadI32() == m.ID && r.ReadU8() == 66 {
					swayed = true
				}
				return
			}
			if id != protocol.IDPlayLevelParticles {
				return
			}
			r := protocol.NewReader(data)
			pkind := r.ReadVarInt()
			if pkind == protocol.ParticleTrail {
				// Target, colour and duration come before the flags.
				r.ReadF64()
				r.ReadF64()
				r.ReadF64()
				r.ReadI32()
				r.ReadVarInt()
				trail++
				if !servertest.ReadParticleTail(t, r).AlwaysShow {
					hidden++
				}
			}
		})
	}
	// vanilla: CreakingHeartBlockEntity.emitParticles sends them always-show.
	if hidden != 0 {
		t.Errorf("%d of %d trail particles skip the client's reduced-particles setting, want none", hidden, trail)
	}
	if health, ok := srv.MobHealth(m.ID); !ok || health != 1 {
		t.Errorf("health after the blow = %v (alive %v), want the untouched 1", health, ok)
	}
	if !swayed {
		t.Errorf("no sway event followed the blow")
	}
	// 20 at the blow, then two a tick while more than 50 of the 100 ticks are left.
	if trail < 100 || trail > 130 {
		t.Errorf("%d trail particles reached the player, want the 20 of the blow and the stream after it (about 120)", trail)
	}
	resin := 0
	for dx := int32(-2); dx <= 2; dx++ {
		for dy := int32(-3); dy <= 3; dy++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if name, _, _ := registry.BlockStateName(srv.BlockAt(hx+dx, hy+dy, hz+dz)); name == "minecraft:resin_clump" {
					resin++
				}
			}
		}
	}
	if resin == 0 {
		t.Errorf("no resin grew on the logs after the blow")
	}
}

// TestADormantHeartsCreakingGrowsNoResin: a blow on the creaking of a heart
// that is not awake grows no resin.
// vanilla: CreakingHeartBlockEntity.creakingHurt
func TestADormantHeartsCreakingGrowsNoResin(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	// The creaking is kept past dusk by a persistent flag the test cannot set,
	// so the heart goes dormant while the clock still reads night: the day
	// state is written by hand.
	dormant, _ := registry.FindState("minecraft:creaking_heart",
		map[string]string{"axis": "y", "creaking_heart_state": "dormant", "natural": "false"})
	srv.SetBlock(hx, hy, hz, dormant)
	srv.MoveMob(m.ID, float64(hx)+4.5, float64(hz)+0.5)
	srv.TickMobs(1)
	m = creakingByID(t, srv, m.ID)
	c.MoveTo(m.X-1.2, m.Y, m.Z, -90, 0)
	c.Settle()
	c.Attack(m.ID)
	for range 10 {
		srv.TickMobs(1)
	}
	for dy := int32(-3); dy <= 3; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			if name, _, _ := registry.BlockStateName(srv.BlockAt(hx+dx, hy+dy, hz)); name == "minecraft:resin_clump" {
				t.Fatalf("resin grew at %d, %d beside a dormant heart", dx, dy)
			}
		}
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

// TestBreakingANaturalHeartPaysItsBreakerExperience: a survival break of a
// natural heart drops 20 to 24 experience; a creative break, or a heart a
// player placed, drops none.
// vanilla: CreakingHeartBlock.tryAwardExperience
func TestBreakingANaturalHeartPaysItsBreakerExperience(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mode    string
		natural bool
		wantXP  bool
	}{
		{"survival natural", "survival", true, true},
		{"survival placed", "survival", false, false},
		{"creative natural", "creative", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.HostileTestConfig()
			cfg.GameMode = tc.mode
			srv, c, _ := servertest.JoinAs(t, cfg, creakingPlayer)
			c.Settle()
			servertest.HoldTicks(t, srv)
			pos := srv.PlayerPosition(creakingPlayer)
			c.MoveToGround(pos.X, pos.Y, pos.Z, true)
			c.Settle()
			hx, hy, hz := plantHeart(t, srv, tc.natural)
			srv.SetDayTime(6000)
			srv.GiveSlot(creakingPlayer, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_axe", "minecraft:efficiency", 5))
			c.StartDigging(hx, hy, hz)
			c.StopDigging(hx, hy, hz)
			c.Settle()
			servertest.UntilTicks(t, srv, 40, "the heart breaks", func() bool { return srv.BlockAt(hx, hy, hz) == 0 })
			xp := 0
			for _, o := range srv.OrbsInWorld() {
				xp += o.XP
			}
			if tc.wantXP && (xp < 20 || xp > 24) {
				t.Errorf("the break left %d experience, want 20 to 24", xp)
			}
			if !tc.wantXP && xp != 0 {
				t.Errorf("the break left %d experience, want none", xp)
			}
		})
	}
}

// TestAHeartRemovedByNoPlayerTakesItsCreakingAway: a creaking whose heart is
// gone from the world without a player's break is taken away, not killed.
// vanilla: CreakingHeartBlockEntity.preRemoveSideEffects
func TestAHeartRemovedByNoPlayerTakesItsCreakingAway(t *testing.T) {
	t.Parallel()
	srv, _ := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	srv.SetBlock(hx, hy, hz, 0)
	srv.RemoveBlockEntity(hx, hy, hz)
	srv.TickMobs(40)
	if hasCreaking(srv, m.ID) {
		t.Errorf("the creaking outlived the vanished heart")
	}
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

// TestACreakingTheHeartDoesNotListIsKilled: a creaking whose heart lists no
// creaking, or another one, dies although the heart is there, and the heart
// stands its own up.
// vanilla: Creaking.tick, CreakingHeartBlockEntity.isProtector
func TestACreakingTheHeartDoesNotListIsKilled(t *testing.T) {
	t.Parallel()
	srv, _ := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	old := awaitCreaking(t, srv)
	srv.UnlinkCreakingHeart(hx, hy, hz)
	tickHeart(srv, 80)
	got := creakingsOf(srv)
	if len(got) != 1 || got[0].ID == old.ID {
		t.Errorf("%d creakings after the heart lost its link, want only the new one (old %d, now %v)", len(got), old.ID, got)
	}
}

// TestACreakingKeepsLivingWhileItsHeartChunkIsUnloaded: a bound creaking whose
// heart is in an unloaded chunk is not taken for an orphan.
// vanilla: Level.getBlockEntity loads the chunk
func TestACreakingKeepsLivingWhileItsHeartChunkIsUnloaded(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, _, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	// The creaking stands where the player is going, 80 blocks off, so its
	// chunk is the one that stays.
	far := int32(hx) + 80
	srv.MoveMob(m.ID, float64(far)+0.5, float64(hz)+0.5)
	c.MoveTo(float64(far)+0.5, m.Y, float64(hz)+0.5, 0, 0)
	// The creaking's own chunk must be loaded before mobs tick, or the herd
	// drops it as a mob in an unloaded chunk. The sender loads it on its own
	// beat, so wait on the beats; the bound is a stall guard.
	c.Settle()
	for range 2000 {
		if !srv.ChunkLoadedInDimension(world.DimensionName, hx, hz) && srv.ChunkLoadedInDimension(world.DimensionName, far, hz) {
			break
		}
		<-srv.ChunkBeat()
	}
	if srv.ChunkLoadedInDimension(world.DimensionName, hx, hz) {
		t.Fatal("the heart's chunk is still loaded 80 blocks off")
	}
	if !srv.ChunkLoadedInDimension(world.DimensionName, far, hz) {
		t.Fatal("the creaking's chunk is not loaded 80 blocks off")
	}
	srv.TickMobs(60)
	if !hasCreaking(srv, m.ID) {
		t.Errorf("the creaking died with its heart's chunk unloaded")
	}
}

// TestAHeartBlownUpKillsItsCreakingAndPaysExperience: a blast by a player that
// destroys a natural heart twitches its creaking to death and pays the
// experience a break pays.
// vanilla: CreakingHeartBlock.onExplosionHit
func TestAHeartBlownUpKillsItsCreakingAndPaysExperience(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, hy, hz := plantHeart(t, srv, true)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	died, twitching := false, false
	srv.BlastAtCause(float64(hx)+0.5, float64(hy)+0.5, float64(hz)+0.5, 4, creakingPlayer)
	for range 3 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityEvent:
				if r := protocol.NewReader(data); r.ReadI32() == m.ID && r.ReadU8() == protocol.EntityStatusDeath {
					died = true
				}
			case protocol.IDPlayEntityMetadata:
				r := protocol.NewReader(data)
				if r.ReadVarInt() != m.ID {
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
	}
	if srv.BlockAt(hx, hy, hz) != 0 {
		t.Fatalf("the blast left the heart standing")
	}
	if !died || !twitching {
		t.Errorf("the creaking died %v, twitching %v after its heart blew up, want both", died, twitching)
	}
	xp := 0
	for _, o := range srv.OrbsInWorld() {
		xp += o.XP
	}
	if xp < 20 || xp > 24 {
		t.Errorf("the blast left %d experience, want 20 to 24", xp)
	}
}

// TestAHeartsCreakingSpeaksWhenItSpawns: the creaking the heart stands up
// plays its ambient sound along with the spawn sound.
// vanilla: SpawnUtil.trySpawnMob, CreakingHeartBlockEntity.serverTick
func TestAHeartsCreakingSpeaksWhenItSpawns(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	servertest.HoldTicks(t, srv)
	plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	// The spawn spot is rolled up to 16 blocks from the heart, which is as far
	// as the sound carries; this seed's lands within hearing.
	srv.SetLevelSeed(1)
	sounds, heard := countCreakingSounds(t, "minecraft:entity.creaking.ambient", "minecraft:entity.creaking.spawn")
	for range 200 {
		srv.TickBlockEntities()
		c.SettleAndCollect(func(pid int32, data []byte) {
			sounds(pid, data)
			if pid != protocol.IDPlaySoundEffect {
				return
			}
			name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
			if name != "minecraft:entity.creaking.spawn" {
				return
			}
			m := creakingsOf(srv)[0]
			if x != int32(m.X*8) || y != int32(m.Y*8) || z != int32(m.Z*8) {
				t.Errorf("spawn sound at %d,%d,%d eighths; want feet at %v,%v,%v", x, y, z, m.X, m.Y, m.Z)
			}
			if category != protocol.SoundCategoryHostile || volume != 1 || pitch < 0.79 || pitch > 1.21 {
				t.Errorf("spawn voice: category=%d volume=%v pitch=%v", category, volume, pitch)
			}
		})
		if len(creakingsOf(srv)) > 0 {
			break
		}
	}
	c.SettleAndCollect(sounds)
	if heard["minecraft:entity.creaking.spawn"] != 1 || heard["minecraft:entity.creaking.ambient"] != 1 {
		t.Errorf("heard the spawn sound %d times and the ambient sound %d times, want once each",
			heard["minecraft:entity.creaking.spawn"], heard["minecraft:entity.creaking.ambient"])
	}
}

// TestAHeartBoundCreakingStaysOutOfAPortal stands the heart's creaking and a
// summoned one in a lit portal: the summoned one crosses, the bound one may
// not leave its heart and stays.
// vanilla: Creaking.canUsePortal
func TestAHeartBoundCreakingStaysOutOfAPortal(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CrossingTestConfig(t), creakingPlayer)
	servertest.LightPortal(t, srv, c)
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	srv.SetDayTime(18000)
	plantHeart(t, srv, false)
	bound := awaitCreaking(t, srv)
	if !bound.CreakingBound {
		t.Fatal("the heart's creaking is not bound to it")
	}
	if bound.Y != servertest.PortalFloorY {
		t.Fatalf("the heart's creaking stands at y %v, want the portal's floor %d", bound.Y, servertest.PortalFloorY)
	}
	z := float64(servertest.PortalInnerZ) + 0.5
	srv.MoveMob(bound.ID, float64(servertest.PortalInnerX0)+0.5, z)
	free := srv.SpawnMob("minecraft:creaking", float64(servertest.PortalInnerX1)+0.5, servertest.PortalFloorY, z)
	if free == 0 {
		t.Fatal("spawning the unbound creaking failed")
	}
	srv.SetMobNoAI(bound.ID)
	srv.SetMobNoAI(free)

	for range 5 {
		tickHeart(srv, 1)
		if len(srv.MobsInDimension(world.NetherName)) > 0 {
			break
		}
	}
	tickHeart(srv, 5)
	if got := srv.MobsInDimension(world.NetherName); len(got) != 1 || got[0].ID == bound.ID {
		t.Errorf("nether mobs = %+v, want the summoned creaking alone", got)
	}
	if !hasCreaking(srv, bound.ID) {
		t.Errorf("the heart's creaking left the overworld")
	}
}

func TestCreakingHeartRemovalSoundsUseFeetAndHonorSilent(t *testing.T) {
	t.Parallel()
	for _, killed := range []bool{false, true} {
		for _, silent := range []bool{false, true} {
			t.Run(fmt.Sprintf("killed=%v/silent=%v", killed, silent), func(t *testing.T) {
				srv, c := creakingWorldIn(t, "creative")
				servertest.HoldTicks(t, srv)
				hx, hy, hz := plantHeart(t, srv, false)
				srv.SetDayTime(18000)
				m := awaitCreaking(t, srv)
				pos := srv.PlayerPosition(creakingPlayer)
				srv.MoveMob(m.ID, pos.X+1.5, pos.Z)
				srv.SetMobNoAI(m.ID)
				srv.SetMobSilent(m.ID, silent)
				m = creakingByID(t, srv, m.ID)
				c.Settle()
				if killed {
					c.BreakBlock(hx, hy, hz)
				} else {
					srv.SetDayTime(6000)
				}
				heard := map[string]int{}
				for range 80 {
					tickHeart(srv, 1)
					c.SettleAndCollect(func(pid int32, data []byte) {
						if pid != protocol.IDPlaySoundEffect {
							return
						}
						name, _, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
						if name != "minecraft:entity.creaking.twitch" && name != "minecraft:entity.creaking.death" {
							return
						}
						heard[name]++
						if x != int32(m.X*8) || y != int32(m.Y*8) || z != int32(m.Z*8) || volume != 1 || pitch < 0.79 || pitch > 1.21 {
							t.Errorf("%s at %d,%d,%d volume=%v pitch=%v; want feet %v,%v,%v and adult voice", name, x, y, z, volume, pitch, m.X, m.Y, m.Z)
						}
					})
				}
				wantDeath, wantTwitch := 0, 0
				if !silent {
					wantDeath = 1
					if killed {
						wantTwitch = 1
					}
				}
				if heard["minecraft:entity.creaking.death"] != wantDeath || heard["minecraft:entity.creaking.twitch"] != wantTwitch {
					t.Errorf("heard %v; want %d death, %d twitch", heard, wantDeath, wantTwitch)
				}
			})
		}
	}
}

// TestCreakingSwaysUnderSkeletonArrow: an ownerless arrow (a skeleton's or a
// dispenser's) that hits a heart-bound creaking sways it and sounds the sway,
// but only a player's blow reaches the heart.
// vanilla: Creaking.hurtServer
func TestCreakingSwaysUnderSkeletonArrow(t *testing.T) {
	t.Parallel()
	srv, c := creakingWorldIn(t, "survival")
	hx, _, hz := plantHeart(t, srv, false)
	srv.SetDayTime(18000)
	m := awaitCreaking(t, srv)
	srv.MoveMob(m.ID, float64(hx)+4.5, float64(hz)+0.5)
	srv.TickMobs(1)
	m = creakingByID(t, srv, m.ID)
	c.Settle()
	swaySound := servertest.ConduitSoundID(t, "minecraft:entity.creaking.sway")
	hurtSound := servertest.ConduitSoundID(t, "minecraft:block.creaking_heart.hurt")
	swayed, sounded, heartHurt, trail := false, false, false, 0
	srv.ShootArrow(m.X-2, m.Y+1, m.Z, 1.5, 0, 0)
	for range 20 {
		srv.TickEntities(1)
		tickHeart(srv, 1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityEvent:
				if r := protocol.NewReader(data); r.ReadI32() == m.ID && r.ReadU8() == 66 {
					swayed = true
				}
			case protocol.IDPlaySoundEffect:
				var s servertest.SoundSee
				s.ReadSoundSee(t, data)
				sounded = sounded || s.Sound == swaySound
				heartHurt = heartHurt || s.Sound == hurtSound
			case protocol.IDPlayLevelParticles:
				if protocol.NewReader(data).ReadVarInt() == protocol.ParticleTrail {
					trail++
				}
			}
		})
	}
	if !swayed || !sounded {
		t.Errorf("sway event %v, sway sound %v after an ownerless arrow hit, want both", swayed, sounded)
	}
	if heartHurt || trail != 0 {
		t.Errorf("the heart was told of a blow no player struck (sound %v, %d trail particles)", heartHurt, trail)
	}
}
