package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWitchThrowsSplashPotionsAtAPlayer stands a witch eight blocks off a
// survival player and watches the wire: the witch throws a splash potion
// it owns, born at its eyes less a tenth, and keeps throwing.
func TestAWitchThrowsSplashPotionsAtAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Prey")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	var potions []servertest.ThrownSpawn
	c.ReadTicks(srv, 400, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity {
			if sp := servertest.ReadThrownSpawn(c, data); sp.EntityType == protocol.EntityTypeSplashPotion {
				potions = append(potions, sp)
			}
		}
		return len(potions) >= 2
	})
	if len(potions) < 2 {
		t.Fatalf("a witch eight blocks from a player threw %d splash potions in 400 ticks, want at least 2", len(potions))
	}
	for _, p := range potions {
		if p.OwnerID != witchID {
			t.Errorf("a potion's owner = %d, want the witch %d", p.OwnerID, witchID)
		}
	}
}

// TestAHarmingSplashHurtsAWitchItsThrowerIsNot is the control: the same
// splash owned by another body does hurt the witch.
func TestAHarmingSplashHurtsAWitchItsThrowerIsNot(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Bystander")

	witchID := srv.SpawnMob("minecraft:witch", 8.5, 64, 0.5)
	srv.SetMobNoAI(witchID)
	before, _ := srv.MobHealth(witchID)
	srv.ThrowMobPotion("minecraft:harming", 8.5, 65, 0.5, 0)
	srv.TickEntities(10)
	srv.TickMobs(3)
	if after, _ := srv.MobHealth(witchID); after >= before {
		t.Errorf("a foreign harming splash left the witch at %v from %v, want it hurt", after, before)
	}
}
