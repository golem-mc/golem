package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// The dying effects: vanilla's LivingEntity.remove runs each of a dead
// body's effects' onMobRemoved with the KILLED reason once — oozing stands
// two size-two slimes up at the body, capped by max_entity_cramming, and
// weaving places two or three cobwebs on replaceable cells with a sturdy
// face below them. The trigger waits for the corpse's end: a creeper's
// comes on the blast itself, the body having no corpse, and a player's runs
// when the respawn removes the old body.
// vanilla: LivingEntity.triggerOnDeathMobEffects, OozingMobEffect.onMobRemoved,
// WeavingMobEffect.onMobRemoved

// worldEventCobwebWeave is the level event a placed cobweb plays.
// vanilla: WeavingMobEffect's levelEvent(3018, pos, 0)
const worldEventCobwebWeave = 3018

// effectKillMob stands a mob up on a stopped loop at the flat ground's
// (5.5, 64, 5.5), gives it the named effect, kills it with /kill and steps
// the mob pass until the corpse leaves, counting the slimes standing at the
// body each tick. The returned tick is the one the corpse left on — the
// tick the dying effects fire on — and the counts index by the tick since
// the kill.
func effectKillMob(t *testing.T, kind, effect string, typ int32, pre func(*server.Server)) (*server.Server, *servertest.Client, servertest.SpawnedMob, int, []int) {
	t.Helper()
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// Midnight, so the body is not lit and burned by the sun before its
	// death lands.
	srv.SetDayTime(18000)
	srv.StopTick()

	c.ChatCommand("summon " + kind + " 5.5 64 5.5 {NoAI:1b}")
	spawn := c.ExpectMobSpawn(typ)
	if w := c.Give("effect give @e[type="+kind+"] "+effect+" 60", spawn.EntityID); !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
		t.Fatalf("the %s got no %s effect", kind, effect)
	}
	if pre != nil {
		pre(srv)
	}
	c.ChatCommand("kill @e[type=" + kind + "]")
	c.Settle()

	var counts []int
	removed := awaitCorpseEnd(t, srv, c, spawn.EntityID, func() {
		counts = append(counts, slimesStandingAt(srv, 5.5, 64.5, 5.5))
	})
	return srv, c, spawn, removed, counts
}

// awaitCorpseEnd steps the mob pass until a body's corpse leaves — the
// twenty-tick countdown's end, whose broadcast poof is also the tick the
// body's dying effects fire on — and answers that tick's number, running
// counter after each tick when one is given.
func awaitCorpseEnd(t *testing.T, srv *server.Server, c *servertest.Client, entityID int32, counter func()) int {
	t.Helper()
	for tick := range 100 {
		srv.TickMobs(1)
		if counter != nil {
			counter()
		}
		// A settle reads all the tick queued, so the poof is found on the
		// tick that sent it: a window could close first and let the next
		// tick move the bodies the death stood up.
		var back []servertest.PushedPacket
		poof := false
		c.SettleAndCollect(func(id int32, data []byte) {
			back = append(back, servertest.PushedPacket{Id: id, Data: data})
			if id != protocol.IDPlayEntityEvent {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadI32() == entityID && r.ReadU8() == protocol.EntityStatusDeathPoof {
				poof = true
			}
		})
		c.Unread(back...)
		if poof {
			return tick
		}
	}
	t.Fatalf("the corpse of entity %d never left", entityID)
	return -1
}

// slimesStandingAt counts the slimes whose feet sit exactly at a spot, the
// place a dying body's oozing stands its slimes up at.
func slimesStandingAt(srv *server.Server, x, y, z float64) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:slime" && m.X == x && m.Y == y && m.Z == z {
			n++
		}
	}
	return n
}

// cobwebsIn counts the cobwebs standing in the block box the bounds name.
func cobwebsIn(t *testing.T, srv *server.Server, x0, y0, z0, x1, y1, z1 int32) int {
	t.Helper()
	cobweb := servertest.BlockState(t, "minecraft:cobweb")
	n := 0
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				if srv.BlockAt(x, y, z) == cobweb {
					n++
				}
			}
		}
	}
	return n
}

// weaveEventWatch collects the world events a tick carried.
type weaveEventWatch struct {
	events []worldEventHit
}

type worldEventHit struct {
	event   int32
	x, y, z int32
	data    int32
}

func (w *weaveEventWatch) record(id int32, data []byte) {
	if id != protocol.IDPlayWorldEvent {
		return
	}
	r := protocol.NewReader(data)
	hit := worldEventHit{event: r.ReadI32()}
	hit.x, hit.y, hit.z = r.ReadPosition()
	hit.data = r.ReadI32()
	w.events = append(w.events, hit)
}

// TestOozingSpawnsSlimesWhenItsMobDies kills an oozing zombie and holds the
// two slimes vanilla stands up at the body: both on the tick the corpse
// leaves, at the body's cell half a block up, and none on any earlier tick
// of the corpse's wait.
// vanilla: OozingMobEffect.onMobRemoved, OozingMobEffect.spawnSlimeOffspring
func TestOozingSpawnsSlimesWhenItsMobDies(t *testing.T) {
	t.Parallel()
	srv, _, _, removed, counts := effectKillMob(t, "minecraft:zombie", "minecraft:oozing", protocol.EntityTypeZombie, nil)

	for tick, n := range counts[:removed] {
		if n != 0 {
			t.Fatalf("tick %d of the corpse's wait held %d slimes, want none before the corpse leaves", tick, n)
		}
	}
	if got := counts[removed]; got != 2 {
		t.Fatalf("the corpse's last tick stood %d slimes up, want two", got)
	}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:slime" {
			continue
		}
		if m.X != 5.5 || m.Y != 64.5 || m.Z != 5.5 {
			t.Errorf("a slime stood at (%v, %v, %v), want the body's (5.5, 64.5, 5.5)", m.X, m.Y, m.Z)
		}
	}
}

// TestWeavingPlacesCobwebsWhenItsMobDies kills a weaving cow and holds the
// cobwebs vanilla weaves around the body: two or three of them on the tick
// the corpse leaves, in the body's own block's three-by-three cube, each on
// a replaceable cell with a sturdy face below — the feet's layer and the
// ground under it, never the air above — each announced by the weave's own
// world event.
// vanilla: WeavingMobEffect.onMobRemoved, WeavingMobEffect.spawnCobwebsRandomlyAround
func TestWeavingPlacesCobwebsWhenItsMobDies(t *testing.T) {
	t.Parallel()
	srv, c, _, _, _ := effectKillMob(t, "minecraft:cow", "minecraft:weaving", protocol.EntityTypeCow, nil)

	watch := &weaveEventWatch{}
	c.SettleAndCollect(watch.record)
	if got := cobwebsIn(t, srv, 4, 63, 4, 6, 65, 6); got < 2 || got > 3 {
		t.Fatalf("the body's three-by-three holds %d cobwebs, want two or three", got)
	}
	if got := cobwebsIn(t, srv, 0, 61, 0, 10, 67, 10); got < 2 || got > 3 {
		t.Fatalf("the ground around the body holds %d cobwebs, want the cube's two or three", got)
	}
	if got := cobwebsIn(t, srv, 4, 65, 4, 6, 65, 6); got != 0 {
		t.Errorf("the layer above the feet holds %d cobwebs, want none, its face below being air", got)
	}
	weaves := 0
	for _, hit := range watch.events {
		if hit.event != worldEventCobwebWeave {
			continue
		}
		weaves++
		if hit.x < 4 || hit.x > 6 || hit.y < 63 || hit.y > 65 || hit.z < 4 || hit.z > 6 {
			t.Errorf("the weave event played at (%d, %d, %d), outside the body's three-by-three", hit.x, hit.y, hit.z)
		}
	}
	if weaves == 0 {
		t.Error("no weave world event played")
	}
}
