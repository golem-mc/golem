package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: TamableAnimal.readAdditionalSaveData, getTeam
func TestASummonedWolfReadsItsOwnerUUIDString(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	srv.RunLine("team add owners")
	srv.RunLine("team modify owners friendlyFire false")
	srv.RunLine("team join owners Tester")
	c.ChatCommand(`summon minecraft:wolf 2.5 64 4.5 {NoAI:1b,Owner:"f3d28cb0-7225-3cb1-baeb-2dadd2be89ae",attributes:[{id:"minecraft:max_health",base:40d}],Health:40f}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	var pet int32
	for _, mob := range srv.Mobs() {
		if mob.Kind == "minecraft:wolf" {
			pet = mob.ID
		}
	}
	state, ok := srv.MobTamable(pet)
	if !ok || !state.Tame || state.Owner != protocol.OfflineUUID("Tester").String() {
		t.Fatalf("summoned wolf tame state = %+v, found=%v; want tamed and owned by Tester", state, ok)
	}
	if hp, _ := srv.MobHealth(pet); hp != 40 || state.MaxHealth != 40 {
		t.Fatalf("summoned wolf health = %v of %v, want 40 of 40", hp, state.MaxHealth)
	}
	// A friendly team's TNT cart proves the pet uses its online owner's team,
	// without explicitly adding the pet to the scoreboard.
	c.ChatCommand(`summon minecraft:tnt_minecart 2.5 64 6.5 {fuse:2,Team:"owners"}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	var exploded bool
	c.ReadTicks(srv, 3, func(id int32, _ []byte) bool {
		exploded = exploded || id == protocol.IDPlayExplosion
		return false
	})
	if !exploded {
		t.Fatal("the TNT cart never exploded")
	}
	if hp, ok := srv.MobHealth(pet); !ok || hp != 40 {
		t.Fatalf("owner's team blast left wolf health = %v, found=%v; want 40", hp, ok)
	}
}
