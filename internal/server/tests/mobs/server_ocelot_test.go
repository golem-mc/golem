package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
