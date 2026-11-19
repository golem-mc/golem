package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func spawnOcelot(t *testing.T, srv *server.Server, c *servertest.Client, x, z float64) servertest.SpawnedMob {
	t.Helper()
	srv.SpawnMob("minecraft:ocelot", x, 64, z)
	return c.ExpectMobSpawn(protocol.EntityTypeOcelot)
}

// TestFishTrustsALuredOcelotOneTimeInThree: an ocelot following a player's
// fish takes it from a hand within three blocks and answers with smoke
// (status 40) or hearts (status 41); a trusted one stays trusting.
// vanilla: Ocelot.mobInteract
func TestFishTrustsALuredOcelotOneTimeInThree(t *testing.T) {
	t.Parallel()
	const name = "Tempter"
	srv, c := catYard(t, name, "minecraft:cod")
	const ocelots = 12
	var mobs []*servertest.SpawnedMob
	for i := range ocelots {
		angle := float64(i) * 2 * math.Pi / ocelots
		m := spawnOcelot(t, srv, c, 0.5+2.5*math.Cos(angle), 0.5+2.5*math.Sin(angle))
		mobs = append(mobs, &m)
	}
	// The lure starts on the ocelots' next selection.
	advanceCat(t, srv, c, 4, mobs...)
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
		if event == protocol.EntityEventTrusted || event == protocol.EntityEventTrustFailed {
			status[entity] = event
		}
	})
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 64-ocelots {
		t.Errorf("%d cod left after feeding %d ocelots once each, want %d", got, ocelots, 64-ocelots)
	}
	advanceCat(t, srv, c, 2, mobs...)
	trusting := 0
	for i, m := range mobs {
		ev, ok := status[m.EntityID]
		if !ok {
			t.Fatalf("ocelot %d answered its fish with neither smoke nor hearts", i)
		}
		if m.Trusting != (ev == protocol.EntityEventTrusted) {
			t.Errorf("ocelot %d: event %d but trusting %v", i, ev, m.Trusting)
		}
		if m.Trusting {
			trusting++
		}
	}
	if trusting == 0 || trusting == ocelots {
		t.Errorf("%d of %d ocelots trust, want some but not all at one in three", trusting, ocelots)
	}
}

// TestAnOcelotThatIsNotLuredTakesTheFishAsLove: with no lure running, the
// fish is the animals' food instead and no trust is rolled.
// vanilla: Ocelot.mobInteract, Animal.mobInteract
func TestAnOcelotThatIsNotLuredTakesTheFishAsLove(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Tempter", "minecraft:cod")
	ocelot := spawnOcelot(t, srv, c, 2.5, 0.5)
	c.UseOn(ocelot.EntityID)
	c.AwaitEntityEvent(ocelot.EntityID, loveHearts)
	if m, _ := mobWithID(srv, ocelot.EntityID); m.InLove != 600 {
		t.Errorf("love %d, want 600", m.InLove)
	}
}

// TestAWildOcelotRunsFromAPlayerUntilItTrusts: an ocelot flees a player in
// sight until it trusts; a trusting one stays.
// vanilla: Ocelot.OcelotAvoidEntityGoal
func TestAWildOcelotRunsFromAPlayerUntilItTrusts(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Walker", "")
	wild := spawnOcelot(t, srv, c, 4.5, 0.5)
	trusting := spawnOcelot(t, srv, c, -4.5, 0.5)
	srv.SetMobTrusting(trusting.EntityID)
	servertest.AwaitTicks(t, srv, 40)
	if d := catDistance(t, srv, wild.EntityID, 0.5, 0.5); d < 8 {
		t.Errorf("the wild ocelot is %.1f blocks from the player after 40 ticks, want it fled to 8", d)
	}
	if d := catDistance(t, srv, trusting.EntityID, -4.5, 0.5); d > 3 {
		t.Errorf("the trusting ocelot wandered %.1f blocks, want it to stay by the player", d)
	}
}

// TestACreeperBacksAwayFromACatAndAnOcelot: a creeper within six blocks of a
// cat or ocelot in sight runs from it, though a player is its prey.
// vanilla: Creeper.registerGoals' AvoidEntityGoal
func TestACreeperBacksAwayFromACatAndAnOcelot(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:cat", "minecraft:ocelot", ""} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := catYard(t, "Bait", "")
			creeper := srv.SpawnMob("minecraft:creeper", 12.5, 64, 0.5)
			if kind != "" {
				cat := srv.SpawnMob(kind, 8.5, 64, 0.5)
				srv.SetMobNoAI(cat)
			}
			servertest.AwaitTicks(t, srv, 30)
			x, _, _, _ := srv.MobPos(creeper)
			if kind == "" && x >= 12.5 {
				t.Fatalf("with no cat the creeper stands at x=%.1f, want it walking toward the player", x)
			}
			if kind != "" && x <= 12.5 {
				t.Errorf("with a %s between, the creeper stands at x=%.1f, want it backed away past 12.5", kind, x)
			}
		})
	}
}

// TestAnOcelotBeyondThreeBlocksIsFedAsLove: the trust roll needs the player
// within three blocks of the ocelot, feet to feet, even while it is lured.
// vanilla: Ocelot.mobInteract
func TestAnOcelotBeyondThreeBlocksIsFedAsLove(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Tempter", "minecraft:cod")
	ocelot := spawnOcelot(t, srv, c, 3.7, 0.5)
	advanceCat(t, srv, c, 1, &ocelot)
	c.UseOn(ocelot.EntityID)
	c.AwaitEntityEvent(ocelot.EntityID, loveHearts)
}

// TestAnOcelotStopsFollowingALurePlayerWhoMoves: a lured wild ocelot within
// six blocks drops the tempt goal when the player moves; a player standing
// still keeps it.
// vanilla: TemptGoal.canContinueToUse
func TestAnOcelotStopsFollowingALurePlayerWhoMoves(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Tempter", "minecraft:cod")
	ocelot := spawnOcelot(t, srv, c, 4.5, 0.5)
	servertest.AwaitTicks(t, srv, 4)
	if !srv.MobTemptRunning(ocelot.EntityID) {
		t.Fatal("the ocelot does not follow the fish")
	}
	servertest.AwaitTicks(t, srv, 6)
	if !srv.MobTemptRunning(ocelot.EntityID) {
		t.Fatal("the ocelot stopped following a player who stood still")
	}
	c.MoveTo(1.5, 64, 0.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 4)
	if srv.MobTemptRunning(ocelot.EntityID) {
		t.Error("the ocelot still follows a player who moved a block")
	}
}

// TestATrustingOcelotKeepsFollowingAPlayerWhoMoves: a trusting ocelot is
// exempt from the scare, so a moving player does not end its tempt goal.
// vanilla: Ocelot.OcelotTemptGoal.canScare
func TestATrustingOcelotKeepsFollowingAPlayerWhoMoves(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Tempter", "minecraft:cod")
	ocelot := spawnOcelot(t, srv, c, 4.5, 0.5)
	if !srv.SetMobTrusting(ocelot.EntityID) {
		t.Fatal("the ocelot is not in the herd")
	}
	servertest.AwaitTicks(t, srv, 4)
	if !srv.MobTemptRunning(ocelot.EntityID) {
		t.Fatal("the ocelot does not follow the fish")
	}
	c.MoveTo(1.5, 64, 0.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 4)
	if !srv.MobTemptRunning(ocelot.EntityID) {
		t.Error("a trusting ocelot stopped following a player who moved")
	}
}

// TestAWildCatStopsFollowingALurePlayerWhoMoves: a wild cat within six
// blocks drops the tempt goal when the player moves.
// vanilla: Cat.CatTemptGoal.canScare
func TestAWildCatStopsFollowingALurePlayerWhoMoves(t *testing.T) {
	t.Parallel()
	srv, c := catYard(t, "Tempter", "minecraft:cod")
	cat := spawnCat(t, srv, c, 4.5, 0.5)
	servertest.AwaitTicks(t, srv, 4)
	if !srv.MobTemptRunning(cat.EntityID) {
		t.Fatal("the cat does not follow the fish")
	}
	c.MoveTo(1.5, 64, 0.5, 0, 0)
	c.Settle()
	servertest.AwaitTicks(t, srv, 4)
	if srv.MobTemptRunning(cat.EntityID) {
		t.Error("the cat still follows a player who moved a block")
	}
}

// ocelotOnFloor lays a two-wide strip of the floor block from x=4 (the ocelot stops tempting 2.5 blocks from the player, on bare ground) to x=8,
// stands an ocelot at its far end and returns the server and the ocelot.
func ocelotOnFloor(t *testing.T, floor, food string) (*server.Server, *servertest.Client, servertest.SpawnedMob) {
	t.Helper()
	srv, c := catYard(t, "Tempter", food)
	ocelot := spawnOcelot(t, srv, c, 6.5, 0.5)
	// The stance is set after the tick's contacts, so let it settle first.
	advanceCat(t, srv, c, 3, &ocelot)
	state := servertest.BlockState(t, floor)
	for x := int32(4); x <= 8; x++ {
		for z := int32(0); z <= 1; z++ {
			srv.SetBlock(x, 63, z, state)
		}
	}
	return srv, c, ocelot
}

// TestATemptedOcelotCrossesMagmaUnhurt: an ocelot walking to a player's fish
// crouches, so magma does not burn it and redstone ore stays unlit; one with
// no player tempting it is burnt.
// vanilla: Ocelot.isSteppingCarefully, MagmaBlock.stepOn, RedStoneOreBlock.stepOn
func TestATemptedOcelotCrossesMagmaUnhurt(t *testing.T) {
	t.Parallel()
	t.Run("tempted", func(t *testing.T) {
		t.Parallel()
		srv, c, ocelot := ocelotOnFloor(t, "minecraft:magma_block", "minecraft:cod")
		max, _ := srv.MobHealth(ocelot.EntityID)
		sawCrouch := false
		for range 10 {
			advanceCat(t, srv, c, 10, &ocelot)
			sawCrouch = sawCrouch || ocelot.Pose == protocol.PoseSneaking
			if health, _ := srv.MobHealth(ocelot.EntityID); health != max {
				t.Fatalf("health %v, want %v: a crouching ocelot was burnt", health, max)
			}
		}
		if !sawCrouch {
			t.Error("the tempted ocelot never crouched")
		}
	})
	t.Run("untempted", func(t *testing.T) {
		t.Parallel()
		srv, c, ocelot := ocelotOnFloor(t, "minecraft:magma_block", "")
		max, _ := srv.MobHealth(ocelot.EntityID)
		servertest.RunTicks(t, srv, 40)
		if health, _ := srv.MobHealth(ocelot.EntityID); health >= max {
			t.Errorf("health %v, want it burnt below %v", health, max)
		}
		_ = c
	})
	t.Run("ore", func(t *testing.T) {
		t.Parallel()
		srv, c, ocelot := ocelotOnFloor(t, "minecraft:redstone_ore", "minecraft:cod")
		for range 10 {
			advanceCat(t, srv, c, 10, &ocelot)
		}
		for x := int32(4); x <= 8; x++ {
			for z := int32(0); z <= 1; z++ {
				if registry.StateLit(srv.BlockAt(x, 63, z)) {
					t.Fatalf("ore at %d,%d lit", x, z)
				}
			}
		}
	})
}
