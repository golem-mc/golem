package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnAttackOnAMobIsRefusedPastReachOrWithASpear: an empty-handed attack on
// a zombie whose box is 6.5 from the eyes, past the entity range of 3 plus
// the buffer of 3, is refused; a spear's attack is refused whatever the
// distance, within its attack_range of 4.5 plus the margin and buffer
// included, as a spear's blow is its jab; an empty hand in reach lands.
// vanilla: ServerGamePacketListenerImpl.handleAttack, Player.isWithinAttackRange
func TestAnAttackOnAMobIsRefusedPastReachOrWithASpear(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Striker")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	srv.StopTick()
	// The zombie's box is 0.3 either side of its feet and spans the eyes'
	// height: the far one's near face is 6.5 from the eyes, the near one's 2.7.
	far := srv.SpawnMob("minecraft:zombie", 5.5, 64, 12.3)
	near := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	farFull, nearFull := mobHealthNow(t, srv, far), mobHealthNow(t, srv, near)
	chargeByHand(t, srv, "Striker", 5)

	c.Attack(far)
	c.Settle()
	if got := mobHealthNow(t, srv, far); got != farFull {
		t.Errorf("an empty hand's attack 6.5 away left the zombie at %v, want it untouched at %v", got, farFull)
	}

	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1})
	chargeByHand(t, srv, "Striker", 13) // the spear's full charge: its minimum_attack_charge is 1
	for _, id := range []int32{far, near} {
		c.Attack(id)
		c.Settle()
	}
	if got := mobHealthNow(t, srv, far); got != farFull {
		t.Errorf("a spear's attack 6.5 away left the zombie at %v, want it untouched at %v", got, farFull)
	}
	if got := mobHealthNow(t, srv, near); got != nearFull {
		t.Errorf("a spear's attack 2.7 away left the zombie at %v, want it untouched at %v", got, nearFull)
	}

	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{})
	chargeByHand(t, srv, "Striker", 5)
	c.Attack(near)
	c.Settle()
	if got := mobHealthNow(t, srv, near); got >= nearFull {
		t.Errorf("an empty hand's attack 2.7 away left the zombie at %v, want it hurt below %v", got, nearFull)
	}
}
