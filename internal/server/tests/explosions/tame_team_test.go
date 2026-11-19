package explosions

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: TamableAnimal.getTeam, Entity.doTeamsAllowDamage
func TestATamedAnimalInheritsItsOwnersTeamAgainstBlasts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind                                      string
		ownTeam, friendlyFire, offline, chained, primed bool
	}{
		{name: "wolf", kind: "wolf"},
		{name: "cat", kind: "cat"},
		{name: "primed TNT discards its team", kind: "wolf", primed: true},
		{name: "own team overrides owner", kind: "wolf", ownTeam: true},
		{name: "friendly fire enabled", kind: "wolf", friendlyFire: true},
		{name: "offline owner", kind: "wolf", offline: true},
		{name: "root owner", kind: "wolf", chained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Alice")
			servertest.HoldTicks(t, srv)
			srv.RunLine("team add t")
			srv.RunLine(fmt.Sprint("team modify t friendlyFire ", tc.friendlyFire))
			srv.RunLine("team join t Alice")
			pet := srv.SpawnMob("minecraft:"+tc.kind, 2.5, 64, 4.5)
			full := 40.0
			if tc.kind == "cat" {
				full = 10
			}
			if !srv.SetMobOwner(pet, protocol.OfflineUUID("Alice")) {
				t.Fatal("could not set pet owner")
			}
			if tc.kind == "wolf" && !srv.TameMobFor(pet, "Alice") {
				t.Fatal("could not tame wolf")
			}
			if !srv.SetMobHealth(pet, full) {
				t.Fatal("could not tame and heal pet")
			}
			if tc.offline {
				srv.RunLine("team join t Offline")
				if !srv.SetMobOwner(pet, protocol.OfflineUUID("Offline")) {
					t.Fatal("could not set absent owner")
				}
			}
			if tc.chained {
				parent := srv.SpawnMob("minecraft:cat", 10.5, 64, 10.5)
				if !srv.TameMobFor(parent, "Alice") {
					t.Fatal("could not tame root owner's pet")
				}
				id, _ := srv.MobUUID(parent)
				if !srv.SetMobOwner(pet, uuid.MustParse(id)) {
					t.Fatal("could not set intermediate owner")
				}
			}
			if tc.ownTeam {
				srv.RunLine("team add u")
				id, _ := srv.MobUUID(pet)
				srv.RunLine("team join u " + id)
			}
			kind := "minecraft:tnt_minecart"
			if tc.primed {
				kind = "minecraft:tnt"
			}
			srv.RunLine("summon " + kind + " 2.5 64 6.5 {fuse:2,Team:\"t\"}")
			servertest.RunTicks(t, srv, 3)
			health, ok := srv.MobHealth(pet)
			if !ok {
				t.Fatal("pet disappeared")
			}
			spared := !tc.ownTeam && !tc.friendlyFire && !tc.offline && !tc.primed
			if spared && health != full || !spared && health >= full {
				t.Errorf("pet health = %v, want spared=%v at full health %v", health, spared, full)
			}
		})
	}
}
