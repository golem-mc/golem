package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// fangsBiteTicks is the ticks a bite needs after its fangs spawn with no warmup.
const fangsBiteTicks = 8

// fangsDamageEvent is the Damage Event of one bite.
type fangsDamageEvent struct {
	victim, damageType, cause, direct int32
}

// biteWatched summons fangs owned by owner at (x, z) and reads the fang's ID
// and the damage event on victim the bite sends to c, if any.
func biteWatched(t *testing.T, srv *server.Server, c *servertest.Client, owner int32, x, z float64, victim int32) (fang int32, event *fangsDamageEvent) {
	t.Helper()
	fangsType := int32(registry.EntityTypeEvokerFangs)
	srv.SpawnFangs(x, 64, z, owner)
	c.ReadTicks(srv, fangsBiteTicks, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			fangID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == fangsType {
				fang = fangID
			}
		case protocol.IDPlayDamageEvent:
			if ev := (fangsDamageEvent{r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()}); ev.victim == victim {
				event = &ev
			}
		}
		return false
	})
	return fang, event
}

// TestPlayerOwnedFangsBite: fangs a player owns spare that player, their
// teammates and an allied mob, and hurt anyone else with unscaled indirect
// magic damage credited to the player.
// vanilla: EvokerFangs.dealDamageTo, DamageSource.scalesWithDifficulty
func TestPlayerOwnedFangsBite(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetSpawning(false)
	srv.SetDifficulty(rules.DifficultyEasy)
	oc, mc, fc := connect(), connect(), connect()
	owner, mate, foe := oc.JoinAs("Owner"), mc.JoinAs("Mate"), fc.JoinAs("Foe")
	servertest.HoldTicks(t, srv)
	srv.RunLine("team add t")
	srv.RunLine("team join t Owner")
	srv.RunLine("team join t Mate")
	oc.MoveToGround(0.5, 64, 0.5, true)
	mc.MoveToGround(3.5, 64, 0.5, true)
	fc.MoveToGround(6.5, 64, 0.5, true)
	cow := srv.SpawnMob("minecraft:cow", 9.5, 64, 0.5)
	husk := srv.SpawnMob("minecraft:husk", 12.5, 64, 0.5)
	for _, id := range []int32{cow, husk} {
		srv.SetMobNoAI(id)
	}
	cowUUID, _ := srv.MobUUID(cow)
	srv.RunLine("team join t " + cowUUID)
	oc.Settle()

	indirectMagic := registry.MustID("minecraft:damage_type", "minecraft:indirect_magic")
	check := func(what string, ev *fangsDamageEvent, victim, fang int32) {
		t.Helper()
		want := fangsDamageEvent{victim, indirectMagic, owner + 1, fang + 1}
		if ev == nil || *ev != want {
			t.Errorf("%s damage event = %+v, want %+v", what, ev, want)
		}
	}

	// A player is told by their own connection, a mob by the owner's.
	for _, tc := range []struct {
		name   string
		client *servertest.Client
		id     int32
		x      float64
		hurt   bool
	}{{"Owner", oc, owner, 0.5, false}, {"Mate", mc, mate, 3.5, false}, {"Foe", fc, foe, 6.5, true}} {
		before := srv.PlayerHealth(tc.name)
		fang, ev := biteWatched(t, srv, tc.client, owner, tc.x, 0.5, tc.id)
		if tc.hurt {
			check(tc.name, ev, tc.id, fang)
			if got := srv.PlayerHealth(tc.name); got != before-6 {
				t.Errorf("%s health = %v, want %v: indirect magic is unscaled for a player owner", tc.name, got, before-6)
			}
		} else if ev != nil || srv.PlayerHealth(tc.name) != before {
			t.Errorf("%s was bitten: event %+v, health %v -> %v", tc.name, ev, before, srv.PlayerHealth(tc.name))
		}
	}

	if _, ev := biteWatched(t, srv, oc, owner, 9.5, 0.5, cow); ev != nil {
		t.Errorf("allied cow bitten: %+v", ev)
	}
	if hp, _ := srv.MobHealth(cow); hp != 10 {
		t.Errorf("allied cow health = %v, want 10", hp)
	}

	fang, ev := biteWatched(t, srv, oc, owner, 12.5, 0.5, husk)
	check("husk", ev, husk, fang)
	if hp, _ := srv.MobHealth(husk); hp != 14 {
		t.Errorf("husk health = %v, want 14", hp)
	}

	// The kill is the owner's: the husk drops experience.
	weak := srv.SpawnMob("minecraft:husk", 14.5, 64, 0.5)
	srv.SetMobNoAI(weak)
	srv.SetMobHealth(weak, 4)
	biteWatched(t, srv, oc, owner, 14.5, 0.5, weak)
	if hp, found := srv.MobHealth(weak); found && hp > 0 {
		t.Error("weak husk survived a lethal bite")
	}
	servertest.RunTicks(t, srv, 30) // the death animation
	if srv.OrbCount() == 0 {
		t.Error("no experience dropped: the kill was not credited to the owner")
	}
}
