package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestASpearJabWithoutAttackRangeReachesTheEntityInteractionRange jabs with
// a wooden spear given without its attack_range at a zombie six blocks
// ahead, its near face 5.7 from the eyes. The reach falls back to the entity
// interaction range: the default 3 falls short, a base of 10 lands.
// vanilla: AttackRange.defaultFor
func TestASpearJabWithoutAttackRangeReachesTheEntityInteractionRange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.CombatTestConfig(), "Reacher", servertest.OpsListing(t, "Reacher"))
	c.FacePlayer(5.5, 64, 5.5, 0, 0) // yaw 0 looks along +Z
	// Standing still: the step into place would lengthen the jab by its
	// five blocks of known movement.
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	spear := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1}
	spear.Remove(protocol.DataComponentAttackRange)
	srv.StopTick()
	srv.GiveSlot("Reacher", player.HotbarSlot, spear)
	srv.TickSurvival() // sees the new item and spends the charge
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 11.5)
	full := mobHealthNow(t, srv, zombie)

	chargeByHand(t, srv, "Reacher", 13) // 20 / (4 - 2.4615) ticks
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != full {
		t.Fatalf("a jab at the default entity range left the zombie at %v, want it untouched at %v", got, full)
	}

	c.ChatCommand("attribute @s minecraft:entity_interaction_range base set 10")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	chargeByHand(t, srv, "Reacher", 13)
	c.Stab()
	c.Settle()
	want := full - spearJab(t, "minecraft:wooden_spear")
	if got := mobHealthNow(t, srv, zombie); math.Abs(got-want) > 0.01 {
		t.Errorf("a jab at entity range 10 left the zombie at %v, want %v", got, want)
	}
}
