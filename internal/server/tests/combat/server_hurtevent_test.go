package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAHurtMobGetsNoEntityEventTwo checks that a hurt is told by the damage
// event alone: entity event 2 is the kinetic weapon hit sound.
// vanilla: EntityEvent.KINETIC_HIT, LivingEntity.hurtServer
func TestAHurtMobGetsNoEntityEventTwo(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Hitter")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	mobID := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	c.Attack(mobID)
	damaged, kinetic := false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayDamageEvent:
			damaged = damaged || r.ReadVarInt() == mobID
		case protocol.IDPlayEntityEvent:
			kinetic = kinetic || r.ReadI32() == mobID && r.ReadU8() == protocol.EntityStatusKineticHit
		}
	})
	if !damaged {
		t.Error("no damage event for the zombie")
	}
	if kinetic {
		t.Error("entity event 2 sent for a hurt zombie")
	}
}
