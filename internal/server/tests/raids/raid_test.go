package raids

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestBadOmenInAVillageBecomesARaidOmen: standing by a bell, a bad omen turns
// into a raid omen of the same level; elsewhere it stays.
// vanilla: BadOmenMobEffect.applyEffectTick
func TestBadOmenInAVillageBecomesARaidOmen(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	srv.GiveEffect("Hero", "minecraft:bad_omen", 100000, 2, true)
	run(c, srv, 2)
	if _, amp, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); !ok || amp != 2 {
		t.Errorf("raid omen = (%d, %v), want amplifier 2", amp, ok)
	}
	if _, _, ok := srv.PlayerEffect("Hero", "minecraft:bad_omen"); ok {
		t.Errorf("the bad omen stayed")
	}
	_ = bell
}

// TestRaidIsLostWhenTheVillagersDie: with every villager dead their tickets
// are released, the village is gone and the raid ends in loss.
// vanilla: Raid.tick
func TestRaidIsLostWhenTheVillagersDie(t *testing.T) {
	t.Parallel()
	srv, c, bell := bareBell(t, rules.DifficultyNormal)
	id := spawnVillager(t, srv, bell)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	if !srv.KillMob(id) {
		t.Fatalf("the villager was not killed")
	}
	servertest.UntilTicks(t, srv, 400, "the raid is lost", func() bool {
		r := srv.Raids()
		return len(r) == 1 && r[0].Status == "loss"
	})
}

// TestARaidOmenStartsARaidAtTheVillage: when the raid omen runs out the raid
// is made, centred on the village point, its level the omen's amplifier plus
// one.
// vanilla: RaidOmenMobEffect.applyEffectTick, Raids.createOrExtendRaid
func TestARaidOmenStartsARaidAtTheVillage(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	// A bell no villager claimed, placed after the claim, must not pull the
	// centre off the claimed one (vanilla averages only occupied POIs).
	if !srv.SetBlock(bell[0], bell[1], bell[2]+4, registry.MustState("minecraft:bell", nil)) {
		t.Fatalf("the unclaimed bell was not placed")
	}
	startRaid(t, c, srv, 1)
	r := srv.Raids()[0]
	if r.X != int(bell[0]) || r.Y != int(bell[1]) || r.Z != int(bell[2]) {
		t.Errorf("raid centre = (%d, %d, %d), want the bell %v", r.X, r.Y, r.Z, bell)
	}
	if r.OmenLevel != 2 || r.Started || r.Groups != 0 {
		t.Errorf("raid = %+v, want omen level 2 not yet started", r)
	}
	if _, _, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); ok {
		t.Errorf("the raid omen outlived the raid it started")
	}
}

// TestTheFirstWaveComesAfterTheCountdown: after 300 ticks the first wave
// stands up (pillagers, four and a bonus of up to two on normal), one of them
// the banner-wearing captain, and the bar tells the player.
// vanilla: Raid.tick, Raid.spawnGroup, Raid.setLeader
func TestTheFirstWaveComesAfterTheCountdown(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	events := startRaid(t, c, srv, 0)
	events = append(events, run(c, srv, 330)...)
	r := srv.Raids()[0]
	if !r.Started || r.Groups != 1 {
		t.Fatalf("raid = %+v, want the first wave spawned", r)
	}
	pillagers, vindicators := countKind(srv, "minecraft:pillager"), countKind(srv, "minecraft:vindicator")
	if pillagers < 4 || pillagers > 5 || vindicators > 1 || r.Alive != pillagers+vindicators {
		t.Errorf("pillagers = %d, vindicators = %d, raiders counted = %d, want 4-5 and 0-1, all counted", pillagers, vindicators, r.Alive)
	}
	banners := 0
	for _, m := range srv.Mobs() {
		if srv.MobHeadItem(m.ID) != 0 {
			banners++
		}
	}
	if banners != 1 || !r.LeaderWaveOne {
		t.Errorf("captains = %d (leader known: %v), want exactly one", banners, r.LeaderWaveOne)
	}
	var added bool
	for _, ev := range events {
		if ev.bar == r.BarWire && ev.action == protocol.BossEventAdd {
			added = true
		}
	}
	if !added {
		t.Errorf("the player was not shown the raid bar")
	}
}

func countKind(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

// raiderIDs are the ids of the overworld's raider mobs.
func raiderIDs(srv *server.Server) []int32 {
	var ids []int32
	for _, m := range srv.Mobs() {
		switch m.Kind {
		case "minecraft:pillager", "minecraft:vindicator", "minecraft:evoker", "minecraft:ravager", "minecraft:witch":
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// TestTheBarFollowsTheRaidersHealth: the bar starts full with the wave and
// drops as raiders die.
// vanilla: Raid.updateBossbar
func TestTheBarFollowsTheRaidersHealth(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	if got := srv.Raids()[0].Progress; got != 1 {
		t.Fatalf("progress with the wave whole = %v, want 1", got)
	}
	ids := raiderIDs(srv)
	srv.KillMob(ids[0])
	events := run(c, srv, 3)
	r := srv.Raids()[0]
	want := float32(len(ids)-1) / float32(len(ids))
	if r.Alive != len(ids)-1 || r.Progress != want {
		t.Errorf("after one death: alive %d progress %v, want %d and %v", r.Alive, r.Progress, len(ids)-1, want)
	}
	var sent bool
	for _, ev := range events {
		sent = sent || ev.action == protocol.BossEventProgress && ev.fill == want
	}
	if !sent {
		t.Errorf("no Boss Event moved the fill to %v: %+v", want, events)
	}
}

// TestTheNextWaveFollowsTheLastRaider: with the wave dead the countdown runs
// again and the second wave stands up.
// vanilla: Raid.tick
func TestTheNextWaveFollowsTheLastRaider(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	for _, id := range raiderIDs(srv) {
		srv.KillMob(id)
	}
	run(c, srv, 100)
	if r := srv.Raids()[0]; r.Groups != 1 || r.Alive != 0 {
		t.Fatalf("100 ticks after the wave: %+v, want the countdown still running", r)
	}
	run(c, srv, 260)
	if r := srv.Raids()[0]; r.Groups != 2 || r.Alive == 0 {
		t.Errorf("after the second countdown: %+v, want wave 2 alive", r)
	}
}

// TestACompletedRaidIsWon: on easy a raid of three waves ends in victory when
// the last raider is dead, and the bar says so.
// vanilla: Raid.tick
func TestACompletedRaidIsWon(t *testing.T) {
	t.Parallel()
	srv, c, bell := bareBell(t, rules.DifficultyEasy)
	// The villager claiming the bell can celebrate alongside the platform villagers.
	villagers := []int32{spawnVillager(t, srv, bell)}
	startRaid(t, c, srv, 0)
	for wave := 1; wave <= 3; wave++ {
		run(c, srv, 330)
		if r := srv.Raids()[0]; r.Groups != wave {
			t.Fatalf("wave %d: raid = %+v", wave, r)
		}
		for _, id := range raiderIDs(srv) {
			srv.KillMob(id)
		}
	}
	run(c, srv, 60)
	r := srv.Raids()[0]
	if r.Status != "victory" {
		t.Fatalf("status = %q, want victory", r.Status)
	}
	run(c, srv, 20)
	if got := srv.Raids()[0]; got.BarTitleKind != 1 {
		t.Errorf("bar title kind = %d, want 1 (victory)", got.BarTitleKind)
	}
	// Keep the celebrants above the terrain and trees, with open sky.
	y := bell[1] + 16
	for x := bell[0] - 2; x <= bell[0]+2; x++ {
		for z := bell[2] - 2; z <= bell[2]+2; z++ {
			srv.SetBlock(x, y-1, z, registry.MustState("minecraft:stone", nil))
		}
	}
	srv.RelightAt(bell[0], bell[2])
	for i := range 8 {
		villagers = append(villagers, srv.SpawnMobSeeded("minecraft:villager", float64(bell[0])+0.5,
			float64(y), float64(bell[2])+0.5, int64(2162+i)))
	}
	launched := c.ReadTicks(srv, 300, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() != protocol.EntityTypeFireworkRocket {
			return false
		}
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		for _, id := range villagers {
			vx, vy, vz, ok := srv.MobPos(id)
			if ok && (vx-x)*(vx-x)+(vy-y)*(vy-y)+(vz-z)*(vz-z) < 9 {
				return true
			}
		}
		t.Fatalf("celebration rocket spawned away from villagers at (%g, %g, %g)", x, y, z)
		return false
	})
	if !launched {
		t.Fatal("no villager celebration rocket reached the client within 300 ticks")
	}
}

// TestAPeacefulWorldStopsTheRaid: switching to peaceful ends the raid.
// vanilla: Raid.tick
func TestAPeacefulWorldStopsTheRaid(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	srv.SetDifficulty(rules.DifficultyPeaceful)
	run(c, srv, 3)
	if r := srv.Raids(); len(r) != 0 {
		t.Errorf("raids = %+v, want none on peaceful", r)
	}
}

// TestTheRaidsRuleStopsRaids: with the raids rule off the raid is dropped and
// no omen starts another.
// vanilla: Raids.tick, Raids.createOrExtendRaid
func TestTheRaidsRuleStopsRaids(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	srv.SetGameRule("raids", false)
	run(c, srv, 3)
	if r := srv.Raids(); len(r) != 0 {
		t.Fatalf("raids = %+v, want none with the rule off", r)
	}
	srv.GiveEffect("Hero", "minecraft:bad_omen", 100000, 0, true)
	run(c, srv, 700)
	if r := srv.Raids(); len(r) != 0 {
		t.Errorf("raids = %+v, an omen started one with the rule off", r)
	}
}

// TestRaidsAreSaved: a raid written to raids.dat comes back with its state.
// vanilla: Raids.CODEC
func TestRaidsAreSaved(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 1)
	run(c, srv, 50)
	before := srv.Raids()[0]
	srv.SaveRaids()
	srv.ReloadRaids()
	after := srv.Raids()
	if len(after) != 1 || after[0].X != before.X || after[0].OmenLevel != 2 || after[0].Groups != 0 || after[0].Status != "ongoing" {
		t.Errorf("reloaded raids = %+v, want the one saved: %+v", after, before)
	}
}

// TestNoRaidStartsInTheNether: a raid omen that runs out in the nether
// starts no raid.
// vanilla: Raids.createOrExtendRaid
func TestNoRaidStartsInTheNether(t *testing.T) {
	t.Parallel()
	srv, c, _ := bareBell(t, rules.DifficultyNormal)
	srv.MovePlayerToDimension("Hero", "minecraft:the_nether", 8, 64, 8)
	servertest.AwaitViewSent(t, c)
	pos := srv.PlayerPosition("Hero")
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			srv.SetBlockInDimension("minecraft:the_nether", int32(pos.X)+dx, int32(pos.Y)-1, int32(pos.Z)+dz, registry.MustState("minecraft:stone", nil))
			for dy := range int32(3) {
				srv.SetBlockInDimension("minecraft:the_nether", int32(pos.X)+dx, int32(pos.Y)+dy, int32(pos.Z)+dz, registry.MustState("minecraft:air", nil))
			}
		}
	}
	if !srv.SetBlockInDimension("minecraft:the_nether", int32(pos.X)+1, int32(pos.Y), int32(pos.Z), registry.MustState("minecraft:bell", nil)) {
		t.Fatalf("the bell was not placed in the nether")
	}
	spawnVillagerIn(t, srv, "minecraft:the_nether", [3]int32{int32(pos.X) + 1, int32(pos.Y), int32(pos.Z)})
	srv.GiveEffect("Hero", "minecraft:bad_omen", 100000, 0, true)
	run(c, srv, 2)
	if _, _, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); !ok {
		t.Fatalf("the bad omen did not become a raid omen in the nether village")
	}
	for range 10 {
		run(c, srv, 100)
		if _, _, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); !ok {
			break
		}
	}
	if _, _, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); ok {
		t.Fatalf("the raid omen never ran out; health %v pos %+v", srv.PlayerHealth("Hero"), srv.PlayerPosition("Hero"))
	}
	if n := srv.RaidsIn("minecraft:the_nether"); n != 0 {
		t.Errorf("nether raids = %d, want 0", n)
	}
}

// TestRaidersWalkTowardTheVillage: raiders spawned away from the village head
// for the raid's centre. At simulation distance 4 the send view's neighbor
// ring loads a fifth ring of chunks, where a wave would stand still.
// vanilla: PathfindToRaidGoal, Raid.findRandomSpawnPos (isPositionEntityTicking)
func TestRaidersWalkTowardTheVillage(t *testing.T) {
	t.Parallel()
	srv, c, bell := village(t, rules.DifficultyNormal)
	srv.SetSimulationDistance(4)
	srv.SetPlayerGameMode("Hero", protocol.GameModeCreative)
	startRaid(t, c, srv, 0)
	run(c, srv, 330)
	meanDist := func() float64 {
		sum, n := 0.0, 0
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:witch" || m.Vehicle != 0 {
				continue
			}
			for _, id := range raiderIDs(srv) {
				if id == m.ID {
					sum += math.Hypot(m.X-float64(bell[0]), m.Z-float64(bell[2]))
					n++
				}
			}
		}
		if n == 0 {
			t.Fatal("no raiders")
		}
		return sum / float64(n)
	}
	before := meanDist()
	run(c, srv, 200)
	if after := meanDist(); after >= before-2 {
		t.Errorf("mean distance to the village %.1f -> %.1f, want the raiders closer", before, after)
	}
}
