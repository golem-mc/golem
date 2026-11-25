package mobs

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const (
	evokerKind = "minecraft:evoker"
)

// evokerWorld joins a player at the origin on Hard difficulty at night.
func evokerWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	return srv, c
}

func countOfKind(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

// TestAnEvokerSummonsThreeVexesBoundToIt: the first spell against a target
// is the summon; three vexes arrive with an owner, a bound block and a
// limited life of 30 to 119 seconds, and the spell byte shows on the wire.
// vanilla: Evoker.EvokerSummonSpellGoal
func TestAnEvokerSummonsThreeVexesBoundToIt(t *testing.T) {
	t.Parallel()
	srv, c := evokerWorld(t)
	evoker := srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
	var spellSeen bool
	pumpTicks(c, srv, 240, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == evoker {
				if entries, ok := servertest.ReadMetadata(r); ok {
					if e := servertest.MetaOf(entries, protocol.MetadataIndexSpellcasting); e != nil && e.ByteVal == entity.SpellSummonVex {
						spellSeen = true
					}
				}
			}
		}
		return countOfKind(srv, vexKind) >= 3
	})
	if !spellSeen {
		t.Error("no metadata set the spellcasting byte to the summon spell")
	}
	if got := countOfKind(srv, vexKind); got != 3 {
		t.Fatalf("vexes = %d, want 3", got)
	}
	for _, m := range srv.Mobs() {
		if m.Kind != vexKind {
			continue
		}
		v, ok := srv.MobVexState(m.ID)
		if !ok {
			t.Fatalf("vex %d has no state", m.ID)
		}
		if v.OwnerID != evoker || !v.BoundSet || !v.Limited {
			t.Errorf("vex state = %+v, want owner %d, a bound block and a limited life", v, evoker)
		}
		if v.LifeTicks < 20*29 || v.LifeTicks > 20*120 {
			t.Errorf("vex life = %d ticks, want 30 to 119 seconds", v.LifeTicks)
		}
	}
}

// TestAnEvokerRaisesFangsThatBiteThePlayer: with eight vexes about, the summon
// is refused and the evoker casts fangs: they spawn, the animation event is
// sent and the player loses health.
// vanilla: Evoker.EvokerAttackSpellGoal, EvokerFangs.tick
func TestAnEvokerRaisesFangsThatBiteThePlayer(t *testing.T) {
	t.Parallel()
	srv, c := evokerWorld(t)
	c.MoveToGround(20.5, 64, 0.5, true)
	for i := range 8 {
		srv.SpawnMob(vexKind, -4.5, 64, float64(i)-3.5)
	}
	srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
	fangsType, _ := registry.EntityTypeID("minecraft:evoker_fangs")
	var spawned, bite, hurt bool
	pumpTicks(c, srv, 600, servertest.HostileSkip, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == fangsType {
				spawned = true
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			r.ReadI32()
			if r.ReadU8() == protocol.EntityStatusEvokerFangsBite {
				bite = true
			}
		case protocol.IDPlaySetHealth:
			if servertest.ExpectHealthReport(c, data).Health < 20 {
				hurt = true
			}
		}
		return hurt
	})
	if !spawned {
		t.Error("no evoker fangs were spawned")
	}
	if !bite {
		t.Error("no entity event 4 started the fangs' animation")
	}
	if !hurt {
		t.Error("the fangs never hurt the player")
	}
}

// TestFangsSpareAnIllager: the bite skips another illager standing in it.
// vanilla: AbstractIllager.considersEntityAsAlly
func TestFangsSpareAnIllager(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Target"))
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(20.5, 64, 0.5, true)
	// A dead player's chunks are dropped, taking the vindicator with them;
	// resistance 5 keeps the player alive under the fangs and the vexes.
	c.ChatCommand("effect give Target minecraft:resistance infinite 4 true")
	c.Settle()
	for i := range 8 {
		srv.SpawnMob(vexKind, -4.5, 64, float64(i)-3.5)
	}
	srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
	// The vindicator stands on the line between the evoker and the player.
	vindicator := srv.SpawnMob("minecraft:vindicator", 15.5, 64, 0.5)
	srv.SetMobNoAI(vindicator)
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(int32, []byte) bool { return false })
	if hp, _ := srv.MobHealth(vindicator); hp != 24 {
		t.Errorf("vindicator health = %v after the evoker's fangs, want 24", hp)
	}
}

// vanilla: EvokerFangs.dealDamageTo, Entity.isAlliedTo, OwnableEntity.getRootOwner
func TestFangsRespectMobAlliances(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	c := connect().Join("Observer")
	servertest.HoldTicks(t, srv)
	srv.RunLine("team add red")
	srv.RunLine("team add blue")
	srv.RunLine("team join red Observer")
	spawn := func(kind, team string, x, z float64) int32 {
		id := srv.SpawnMob(kind, x, 64, z)
		srv.SetMobNoAI(id)
		if team != "" {
			identity, _ := srv.MobUUID(id)
			srv.RunLine("team join " + team + " " + identity)
		}
		return id
	}
	evoker := spawn(evokerKind, "", 1.5, 14.5)
	redEvoker := spawn(evokerKind, "red", 4.5, 14.5)
	blueIllager := spawn("minecraft:pillager", "blue", 7.5, 14.5)
	redCow := spawn("minecraft:cow", "red", 10.5, 14.5)
	middleVex := spawn(vexKind, "", 13.5, 14.5)
	srv.SetMobVexOwner(middleVex, evoker)

	for i, tc := range []struct {
		name, kind, team string
		caster, summoner int32
		reverse, cycle   bool
		playerRoot       bool
		damage           float64
	}{
		{name: "same team", kind: "minecraft:cow", team: "red", caster: redEvoker},
		{name: "unteamed illagers", kind: "minecraft:vindicator", caster: evoker},
		{name: "different teams", kind: "minecraft:vindicator", team: "blue", caster: redEvoker, damage: 6},
		{name: "only one on a team", kind: "minecraft:vindicator", caster: redEvoker, damage: 6},
		{name: "root is the caster", kind: vexKind, caster: evoker, summoner: middleVex},
		{name: "root is a teammate", kind: vexKind, caster: redEvoker, summoner: redCow},
		{name: "root is an online teammate", kind: vexKind, caster: redEvoker, playerRoot: true},
		{name: "root is an enemy illager", kind: vexKind, caster: redEvoker, summoner: blueIllager, damage: 6},
		{name: "victim considers the owner an ally", kind: evokerKind, caster: middleVex, reverse: true},
		{name: "cycle has no root", kind: vexKind, caster: evoker, summoner: middleVex, cycle: true, damage: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, z := 2.5+3*float64(i%4), 2.5+3*float64(i/4)
			target := spawn(tc.kind, tc.team, x, z)
			if tc.summoner != 0 {
				srv.SetMobVexOwner(target, tc.summoner)
			}
			if tc.playerRoot {
				identity, _ := srv.MobUUID(target)
				owner := nbt.UUIDToIntArray(protocol.OfflineUUID("Observer"))
				srv.RunLine(fmt.Sprintf("data merge entity %s {owner:[I;%d,%d,%d,%d]}", identity, owner[0], owner[1], owner[2], owner[3]))
			}
			if tc.reverse || tc.cycle {
				srv.SetMobVexOwner(middleVex, target)
			}
			before, ok := srv.MobHealth(target)
			if !ok {
				t.Fatal("target missing before the bite")
			}
			srv.SpawnFangs(x, 64, z, tc.caster)
			servertest.RunTicks(t, srv, 8)
			if got, ok := srv.MobHealth(target); !ok || got != before-tc.damage {
				t.Errorf("health after bite = %v (found %v), want %v", got, ok, before-tc.damage)
			}
		})
	}

	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	before := srv.PlayerHealth("Observer")
	srv.SpawnFangs(0.5, 64, 0.5, redEvoker)
	servertest.RunTicks(t, srv, 8)
	if got := srv.PlayerHealth("Observer"); got != before {
		t.Errorf("same-team player health after bite = %v, want %v", got, before)
	}
}

// TestAnEvokerTurnsBlueSheepRed: with nothing to hunt, the wololo spell dyes
// a blue sheep in reach red, and mob griefing off forbids it.
// vanilla: Evoker.EvokerWololoSpellGoal
func TestAnEvokerTurnsBlueSheepRed(t *testing.T) {
	t.Parallel()
	for _, griefing := range []bool{true, false} {
		srv, c := evokerWorld(t)
		c.MoveToGround(40.5, 64, 0.5, true)
		srv.SetGameRule(world.RuleMobGriefing.String(), griefing)
		srv.SpawnMob(evokerKind, 10.5, 64, 0.5)
		sheep := srv.SpawnMob("minecraft:sheep", 12.5, 64, 0.5)
		srv.SetMobColour(sheep, "blue")
		srv.TickMobs(400)
		want := "blue"
		if griefing {
			want = "red"
		}
		if got := srv.MobColour(sheep); got != want {
			t.Errorf("mob_griefing %v: sheep colour = %q, want %q", griefing, got, want)
		}
	}
}

// TestFangsHurtAMobWithTheJawsAsDirectSource: with the evoker and the jaws on
// opposite sides of the victim, the damage is the evoker's, the event names
// the spawned jaws as the direct entity, and the shove points away from them.
// vanilla: EvokerFangs.dealDamageTo, DamageSource.getSourcePosition
func TestFangsHurtAMobWithTheJawsAsDirectSource(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Target")
	servertest.HoldTicks(t, srv)
	evoker := srv.SpawnMob(evokerKind, 14.5, 64, 0.5)
	srv.SetMobNoAI(evoker)
	pig := srv.SpawnMob("minecraft:pig", 10.5, 64, 0.5)
	srv.SetMobNoAI(pig)
	srv.SpawnFangs(9.7, 64, 0.5, evoker)
	var fangs, cause, direct int32
	var vx float64
	var hurt, shoved bool
	// A zero-warmup fang bites on its eighth tick.
	c.ReadTicks(srv, 8, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlaySpawnEntity:
			if kind, e, _, _ := servertest.ReadSpawnEntity(t, data); kind == int32(registry.EntityTypeEvokerFangs) {
				fangs = e.EntityID
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == pig {
				r.ReadVarInt()
				cause, direct = r.ReadVarInt(), r.ReadVarInt()
				hurt = true
			}
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == pig {
				vx, _, _ = r.ReadLpVec3()
				shoved = shoved || hurt
			}
		}
		return false
	})
	if fangs == 0 || !hurt {
		t.Fatalf("fangs spawned = %d, pig hurt = %v", fangs, hurt)
	}
	if cause != evoker+1 {
		t.Errorf("damage cause = %d, want the evoker %d", cause, evoker+1)
	}
	if direct != fangs+1 {
		t.Errorf("direct entity = %d, want the jaws %d", direct, fangs+1)
	}
	if !shoved || vx <= 0 {
		t.Errorf("pig motion after the bite = %v (sent %v), want a shove away from the jaws (+x)", vx, shoved)
	}
}

// TestFangsFindOversizedMobsOnlyThroughTheirSections: a ghast scaled to 64
// blocks wide, filed two sections away, keeps its health although its box
// overlaps the bite, while one filed in the searched sections is hurt.
// vanilla: EvokerFangs.tick, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestFangsFindOversizedMobsOnlyThroughTheirSections(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Observer")
	servertest.HoldTicks(t, srv)
	var ghasts [2]int32
	for i, x := range []float64{46.5, 30.5} {
		ghasts[i] = srv.SpawnMob("minecraft:ghast", x, 80, 0.5)
		srv.SetMobNoAI(ghasts[i])
		identity, _ := srv.MobUUID(ghasts[i])
		srv.RunLine("attribute " + identity + " minecraft:scale base set 16")
	}
	srv.SpawnFangs(16, 80, 0.5, 0)
	servertest.RunTicks(t, srv, 8)
	for i, want := range []float64{10, 4} {
		got, ok := srv.MobHealth(ghasts[i])
		if !ok || got != want {
			t.Errorf("ghast %d health = %v (present %v), want %v", i, got, ok, want)
		}
	}
}

// damageEventOf reads the damage type and cause of the first damage event on victim.
func damageEventOf(c *servertest.Client, srv *server.Server, ticks int, victim int32) (damageType, cause int32) {
	c.ReadTicks(srv, ticks, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayDamageEvent {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == victim {
				damageType = r.ReadVarInt()
				cause = r.ReadVarInt()
				return true
			}
		}
		return false
	})
	return damageType, cause
}

// TestFangsKeepTheirOwnerAcrossDimensions: fangs owned by an evoker loaded in
// the nether spare an allied vindicator and hurt a pig with the evoker as the
// cause of the indirect magic damage, which the pig then names as its attacker.
// vanilla: EvokerFangs.dealDamageTo, EntityReference.getEntity
func TestFangsKeepTheirOwnerAcrossDimensions(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Target", servertest.OpsListing(t, "Target"))
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	evoker := srv.SpawnMobInDimension(evokerKind, world.NetherName, 0.5, 64, 0.5)
	srv.SetMobNoAI(evoker)
	pig := srv.SpawnMob("minecraft:pig", 10.5, 64, 0.5)
	ally := srv.SpawnMob("minecraft:vindicator", 10.5, 64, 0.5)
	for _, id := range []int32{pig, ally} {
		srv.SetMobNoAI(id)
		srv.SetMobHealth(id, 100)
	}
	srv.SpawnFangs(10.5, 64, 0.5, evoker)
	damageType, cause := damageEventOf(c, srv, 8, pig)
	if want := registry.MustID("minecraft:damage_type", "minecraft:indirect_magic"); damageType != want {
		t.Errorf("damage type = %d, want indirect magic %d", damageType, want)
	}
	if cause != evoker+1 {
		t.Errorf("damage cause = %d, want the nether evoker %d", cause, evoker+1)
	}
	if hp, _ := srv.MobHealth(ally); hp < 100 {
		t.Errorf("vindicator health = %v, want it spared as the evoker's ally", hp)
	}
	c.ChatCommand("execute as @e[type=minecraft:pig,limit=1] on attacker run say hi")
	said := false
	c.ReadTicks(srv, 2, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayDisguisedChat {
			said = bytes.Contains(bytes.ToLower(data), []byte("evoker"))
		}
		return said
	})
	if !said {
		t.Error("the pig's attacker relation did not find the nether evoker as the speaker")
	}
}

// TestFangsFollowTheirOwnerThroughATransferDuringWarmup: the evoker crosses to
// the nether after raising its fangs, which then spare its ally and hurt a pig
// as the evoker's new body.
// vanilla: EvokerFangs.dealDamageTo, EntityReference.getEntity
func TestFangsFollowTheirOwnerThroughATransferDuringWarmup(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Target")
	servertest.HoldTicks(t, srv)
	pig := srv.SpawnMob("minecraft:pig", 10.5, 64, 0.5)
	ally := srv.SpawnMob("minecraft:vindicator", 10.5, 64, 0.5)
	for _, id := range []int32{pig, ally} {
		srv.SetMobNoAI(id)
		srv.SetMobHealth(id, 100)
	}
	owner := srv.SpawnMob(evokerKind, 40.5, 64, 40.5)
	srv.SpawnFangsWarmup(10.5, 64, 0.5, owner, 4)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	srv.RunLine("execute in minecraft:the_nether run tp @e[type=minecraft:evoker,limit=1] 0.5 64 0.5")
	mobs := srv.MobsInDimension(world.NetherName)
	if len(mobs) != 1 || mobs[0].ID == owner {
		t.Fatalf("the nether holds %+v, want the evoker under a new ID (was %d)", mobs, owner)
	}
	srv.SetMobNoAI(mobs[0].ID)
	_, cause := damageEventOf(c, srv, 14, pig)
	if cause != mobs[0].ID+1 {
		t.Errorf("damage cause = %d, want the evoker's new body %d", cause, mobs[0].ID+1)
	}
	if hp, _ := srv.MobHealth(ally); hp < 100 {
		t.Errorf("vindicator health = %v, want it spared as the evoker's ally", hp)
	}
}

// TestFangsThornsReachAnOwnerInAnotherDimension: a Thorns VII wearer bitten by
// fangs hurts the evoker that owns them from the nether.
// vanilla: EvokerFangs.dealDamageTo, EnchantmentHelper.doPostAttackEffects
func TestFangsThornsReachAnOwnerInAnotherDimension(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Target", servertest.OpsListing(t, "Target"))
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Target", player.ArmorSlot+1, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", 7))
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	evoker := srv.SpawnMobInDimension(evokerKind, world.NetherName, 0.5, 64, 0.5)
	srv.SetMobNoAI(evoker)
	at := srv.PlayerPosition("Target")
	srv.SpawnFangs(at.X, at.Y, at.Z, evoker)
	c.ReadTicks(srv, 8, func(int32, []byte) bool { return false })
	hp, ok := srv.MobHealthInDimension(world.NetherName, evoker)
	if !ok {
		t.Fatal("the nether evoker is gone")
	}
	if hp >= 24 {
		t.Errorf("nether evoker health = %v, want it hurt by the thorns", hp)
	}
	c.ChatCommand("execute on attacker run say hi")
	said := false
	c.ReadTicks(srv, 2, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayDisguisedChat {
			said = bytes.Contains(bytes.ToLower(data), []byte("evoker"))
		}
		return said
	})
	if !said {
		t.Error("the player's attacker relation did not find the nether evoker as the speaker")
	}
}
