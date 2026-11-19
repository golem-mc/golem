package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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
