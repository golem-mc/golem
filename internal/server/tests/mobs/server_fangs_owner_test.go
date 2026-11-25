package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// fangsBiteTicks is the ticks a bite needs after its fangs spawn with no warmup.
const fangsBiteTicks = 8

// fangsDamageEvent is the Damage Event of one bite.
type fangsDamageEvent struct {
	victim, damageType, cause, direct int32
}

// biteWatched summons fangs owned by the player named owner at (x, 0.5) with
// /summon and reads the fang's ID and the damage event on victim the bite
// sends to c, if any.
func biteWatched(t *testing.T, srv *server.Server, c *servertest.Client, owner string, x float64, victim int32) (fang int32, event *fangsDamageEvent) {
	t.Helper()
	tag := nbt.Stringify(nbt.Compound{"Owner": nbt.UUIDToIntArray(protocol.OfflineUUID(owner))})
	srv.RunLine("summon minecraft:evoker_fangs " + strconv.FormatFloat(x, 'f', 1, 64) + " 64 0.5 " + tag)
	c.ReadTicks(srv, fangsBiteTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if kind, e, _, _ := servertest.ReadSpawnEntity(t, data); kind == int32(registry.EntityTypeEvokerFangs) {
				fang = e.EntityID
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
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
	spared := srv.SpawnMob("minecraft:husk", 14.5, 64, 0.5)
	thorny := srv.SpawnMob("minecraft:husk", 16.5, 64, 0.5)
	for _, id := range []int32{cow, husk, spared, thorny} {
		srv.SetMobNoAI(id)
	}
	srv.SetMobInvulnerable(spared)
	cowUUID, _ := srv.MobUUID(cow)
	srv.RunLine("team join t " + cowUUID)
	thornyUUID, _ := srv.MobUUID(thorny)
	srv.RunLine(`data merge entity ` + thornyUUID + ` {equipment:{chest:{id:"minecraft:diamond_chestplate",count:1,components:{"minecraft:enchantments":{"minecraft:thorns":7}}}}}`)
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
		fang, ev := biteWatched(t, srv, tc.client, "Owner", tc.x, tc.id)
		if tc.hurt {
			check(tc.name, ev, tc.id, fang)
			if got := srv.PlayerHealth(tc.name); got != before-6 {
				t.Errorf("%s health = %v, want %v: indirect magic is unscaled for a player owner", tc.name, got, before-6)
			}
		} else if ev != nil || srv.PlayerHealth(tc.name) != before {
			t.Errorf("%s was bitten: event %+v, health %v -> %v", tc.name, ev, before, srv.PlayerHealth(tc.name))
		}
	}

	if _, ev := biteWatched(t, srv, oc, "Owner", 9.5, cow); ev != nil {
		t.Errorf("allied cow bitten: %+v", ev)
	}
	if hp, _ := srv.MobHealth(cow); hp != 10 {
		t.Errorf("allied cow health = %v, want 10", hp)
	}

	fang, ev := biteWatched(t, srv, oc, "Owner", 12.5, husk)
	check("husk", ev, husk, fang)
	if hp, _ := srv.MobHealth(husk); hp != 14 {
		t.Errorf("husk health = %v, want 14", hp)
	}

	// A creative owner still cannot hurt an invulnerable mob.
	srv.SetPlayerGameMode("Owner", protocol.GameModeCreative)
	if _, ev := biteWatched(t, srv, oc, "Owner", 14.5, spared); ev != nil {
		t.Errorf("invulnerable husk bitten: %+v", ev)
	}
	if hp, _ := srv.MobHealth(spared); hp != 20 {
		t.Errorf("invulnerable husk health = %v, want 20", hp)
	}
	srv.SetPlayerGameMode("Owner", protocol.GameModeSurvival)

	// Thorns on the victim's armor hurt the owner who caused the bite.
	before := srv.PlayerHealth("Owner")
	biteWatched(t, srv, oc, "Owner", 16.5, thorny)
	if got := srv.PlayerHealth("Owner"); got >= before {
		t.Errorf("owner health = %v, want below %v after Thorns", got, before)
	}

	// The kill is the owner's: the husk drops experience.
	weak := srv.SpawnMob("minecraft:husk", 18.5, 64, 0.5)
	srv.SetMobNoAI(weak)
	srv.SetMobHealth(weak, 4)
	biteWatched(t, srv, oc, "Owner", 18.5, weak)
	if hp, found := srv.MobHealth(weak); found && hp > 0 {
		t.Error("weak husk survived a lethal bite")
	}
	servertest.RunTicks(t, srv, 1) // the death effects
	if srv.OrbCount() == 0 {
		t.Error("no experience dropped: the kill was not credited to the owner")
	}

	// The owner in another dimension is still the owner: the teammate is spared.
	srv.RunLine("execute in minecraft:the_nether run tp Owner 8.5 70 8.5")
	before = srv.PlayerHealth("Mate")
	if _, ev := biteWatched(t, srv, mc, "Owner", 3.5, mate); ev != nil || srv.PlayerHealth("Mate") != before {
		t.Errorf("teammate bitten with the owner in the Nether: event %+v", ev)
	}
}
