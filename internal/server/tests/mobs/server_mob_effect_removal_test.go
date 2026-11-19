package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"

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

// slimesWithin counts the slimes within r blocks of the point on every axis.
func slimesWithin(srv *server.Server, x, y, z, r float64) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:slime" && math.Abs(m.X-x) <= r && math.Abs(m.Y-y) <= r && math.Abs(m.Z-z) <= r {
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

// TestOozingSlimesRespectMaxEntityCramming holds the count vanilla clamps
// to: a body dying inside a full crowd stands none up, a partly full one
// only the room left, a rule of zero is no cap at all, and a body dying
// clear of the crowd counts nothing.
// vanilla: OozingMobEffect.numberOfSlimesToSpawn
func TestOozingSlimesRespectMaxEntityCramming(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		cramming   int32
		nearby     float64 // the x a pre-placed slime stands at, 0 for none
		wantSlimes int
	}{
		// One room left in a cap of one: one slime stands up.
		{"roomForOne", 1, 0, 1},
		// The cap of one is already taken by a slime inside the body's
		// inflated box: none stand up.
		{"capTaken", 1, 7.0, 0},
		// A rule of zero uncaps the count: both stand up past the crowd.
		{"uncapped", 0, 7.0, 2},
		// The default rule with a slime outside the body's inflated box,
		// whose edge sits at 7.8: both stand up.
		{"clearOfTheCrowd", 24, 8.1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, _, _, counts := effectKillMob(t, "minecraft:zombie", "minecraft:oozing",
				protocol.EntityTypeZombie, func(srv *server.Server) {
					srv.SetGameRuleValue("max_entity_cramming", tc.cramming)
					if tc.nearby != 0 {
						srv.SpawnMob("minecraft:slime", tc.nearby, 64, 5.5)
					}
				})
			if got := counts[len(counts)-1]; got != tc.wantSlimes {
				t.Fatalf("the dying body stood %d slimes up, want %d", got, tc.wantSlimes)
			}
		})
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

// TestWeavingObeyesMobGriefing holds the gate vanilla reads for a mob's
// death: the rule off leaves the ground unweaved and no weave event played,
// while a player's own death weaves regardless of the rule.
// vanilla: WeavingMobEffect.onMobRemoved's rule check
func TestWeavingObeyesMobGriefing(t *testing.T) {
	t.Parallel()
	const name = "Slayer"
	srv, c, playerID := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.SetGameRule("mob_griefing", false)
	srv.StopTick()

	// A mob's death under the rule off weaves nothing.
	c.ChatCommand("summon minecraft:cow 5.5 64 5.5 {NoAI:1b}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:weaving 60", spawn.EntityID); !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
		t.Fatal("the cow got no weaving effect")
	}
	c.ChatCommand("kill @e[type=minecraft:cow]")
	c.Settle()
	watch := &weaveEventWatch{}
	for range 40 {
		srv.TickMobs(1)
		c.SettleAndCollect(watch.record)
	}
	if got := cobwebsIn(t, srv, 0, 61, 0, 10, 67, 10); got != 0 {
		t.Errorf("a mob's death under mob_griefing false wove %d cobwebs, want none", got)
	}
	for _, hit := range watch.events {
		if hit.event == worldEventCobwebWeave {
			t.Errorf("a weave event played at (%d, %d, %d) under mob_griefing false, want none", hit.x, hit.y, hit.z)
		}
	}

	// A player's own death weaves whatever the rule says.
	c.MoveToGround(9.5, 64, 5.5, true)
	c.Settle()
	if w := c.Give("effect give "+name+" minecraft:weaving 60", playerID); len(w.Effects) == 0 {
		t.Fatal("the player got no weaving effect")
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, playerID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	c.PerformRespawn()
	c.ExpectRespawnThroughTeleport()
	c.Settle()
	if got := cobwebsIn(t, srv, 7, 63, 3, 11, 65, 7); got < 2 || got > 3 {
		t.Fatalf("a player's death under mob_griefing false wove %d cobwebs, want two or three", got)
	}
}

// TestADyingCreeperFiresOozingAtTheBlast lights a NoAI oozing creeper's
// fuse by NBT and holds the slimes standing up on the blast itself: the
// creeper leaves no corpse, so the slimes come the tick it goes, none on
// any tick of the fuse before it.
// vanilla: Creeper.explodeCreeper's triggerOnDeathMobEffects
func TestADyingCreeperFiresOozingAtTheBlast(t *testing.T) {
	t.Parallel()
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(9.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()

	c.ChatCommand("summon minecraft:creeper 5.5 64 5.5 {NoAI:1b,ignited:1b}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if w := c.Give("effect give @e[type=minecraft:creeper] minecraft:oozing 60", spawn.EntityID); !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
		t.Fatal("the creeper got no oozing effect")
	}
	c.Settle()

	creeperGone := -1
	var counts []int
	for tick := range 400 {
		srv.TickMobs(1)
		remaining := false
		for _, m := range srv.Mobs() {
			if m.ID == spawn.EntityID {
				remaining = true
			}
		}
		counts = append(counts, slimesStandingAt(srv, 5.5, 64.5, 5.5))
		if !remaining {
			creeperGone = tick
			break
		}
	}
	if creeperGone < 0 {
		t.Fatal("the creeper never exploded")
	}
	if creeperGone == 0 {
		t.Fatal("the creeper exploded on its own first tick, want the fuse wound first")
	}
	if got := counts[creeperGone]; got != 2 {
		t.Fatalf("the blast stood %d slimes up on its own tick, want two", got)
	}
	for tick, n := range counts[:creeperGone] {
		if n != 0 {
			t.Fatalf("tick %d of the fuse held %d slimes, want none before the blast", tick, n)
		}
	}
}

// TestOozingWaitsForAPlayersRespawn kills an oozing player who respawns
// before the corpse's twenty ticks are up: no slimes while the death screen
// waits, two at the death spot the moment the respawn removes the body.
// vanilla: PlayerList.respawn's removePlayerImmediately
func TestOozingWaitsForAPlayersRespawn(t *testing.T) {
	t.Parallel()
	const name = "Slinger"
	srv, c, entityID := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(5.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	if w := c.Give("effect give "+name+" minecraft:oozing 60", entityID); len(w.Effects) == 0 {
		t.Fatal("the player got no oozing effect")
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	c.Settle()
	srv.TickMobs(5)
	if got := slimesStandingAt(srv, 5.5, 64.5, 5.5); got != 0 {
		t.Fatalf("a dead body waiting on its death screen stood %d slimes up, want none", got)
	}

	c.PerformRespawn()
	c.ExpectRespawnThroughTeleport()
	c.Settle()
	if got := slimesStandingAt(srv, 5.5, 64.5, 5.5); got != 2 {
		t.Fatalf("the respawn stood %d slimes up at the death spot, want two", got)
	}
}

// TestADeadPlayersOozingFiresTwentyTicksAfterTheDeath leaves an oozing
// player on the death screen: the corpse's removal at its twentieth tick
// stands the slimes up with no respawn, and the respawn after it adds none.
// vanilla: LivingEntity.tickDeath, LivingEntity.remove
func TestADeadPlayersOozingFiresTwentyTicksAfterTheDeath(t *testing.T) {
	t.Parallel()
	const name = "Seeper"
	srv, c, entityID := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(5.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	if w := c.Give("effect give "+name+" minecraft:oozing 60", entityID); len(w.Effects) == 0 {
		t.Fatal("the player got no oozing effect")
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	c.Settle()
	// The tick's mob pass steps the slimes after the corpse's removal stands
	// them up, so the count takes the block around the spot.
	for range 19 {
		srv.RunTick()
	}
	if got := slimesWithin(srv, 5.5, 64.5, 5.5, 1); got != 0 {
		t.Fatalf("a body dead for 19 ticks stood %d slimes up, want none", got)
	}
	srv.RunTick()
	if got := slimesWithin(srv, 5.5, 64.5, 5.5, 1); got != 2 {
		t.Fatalf("the corpse's removal at its 20th tick stood %d slimes up, want two", got)
	}

	c.PerformRespawn()
	c.ExpectRespawnThroughTeleport()
	c.Settle()
	if got := slimesWithin(srv, 5.5, 64.5, 5.5, 1); got != 2 {
		t.Fatalf("the respawn after the removal left %d slimes at the death spot, want still two", got)
	}
}

// TestAPlayersRespawnWeavesWhateverMobGriefingSays kills a weaving player
// and holds the cobwebs the respawn weaves at the death spot with the rule
// off — a player's own death is exempt from it.
// vanilla: WeavingMobEffect.onMobRemoved's rule check
func TestAPlayersRespawnWeavesWhateverMobGriefingSays(t *testing.T) {
	t.Parallel()
	const name = "Weaver"
	srv, c, entityID := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(5.5, 64, 5.5, true)
	c.Settle()
	srv.SetGameRule("mob_griefing", false)
	srv.StopTick()

	if w := c.Give("effect give "+name+" minecraft:weaving 60", entityID); len(w.Effects) == 0 {
		t.Fatal("the player got no weaving effect")
	}
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	c.PerformRespawn()
	c.ExpectRespawnThroughTeleport()
	c.Settle()
	if got := cobwebsIn(t, srv, 4, 64, 4, 6, 64, 6); got < 2 || got > 3 {
		t.Fatalf("the death spot's feet layer holds %d cobwebs, want two or three", got)
	}
}

// TestAMobWithBothDyingEffectsFiresBoth kills a zombie carrying oozing and
// weaving at once and holds both triggers landing on the corpse's last
// tick: the slimes and the cobwebs together.
// vanilla: LivingEntity.triggerOnDeathMobEffects runs the whole set
func TestAMobWithBothDyingEffectsFiresBoth(t *testing.T) {
	t.Parallel()
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()

	c.ChatCommand("summon minecraft:zombie 5.5 64 5.5 {NoAI:1b}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if w := c.Give("effect give @e[type=minecraft:zombie] minecraft:oozing 60", spawn.EntityID); !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
		t.Fatal("the zombie got no oozing effect")
	}
	if w := c.Give("effect give @e[type=minecraft:zombie] minecraft:weaving 60", spawn.EntityID); !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
		t.Fatal("the zombie got no weaving effect")
	}
	c.ChatCommand("kill @e[type=minecraft:zombie]")
	c.Settle()

	awaitCorpseEnd(t, srv, c, spawn.EntityID, nil)
	if got := slimesStandingAt(srv, 5.5, 64.5, 5.5); got != 2 {
		t.Fatalf("the corpse's last tick stood %d slimes up, want two", got)
	}
	if got := cobwebsIn(t, srv, 4, 64, 4, 6, 64, 6); got < 2 || got > 3 {
		t.Fatalf("the corpse's last tick wove %d cobwebs, want two or three", got)
	}
}
