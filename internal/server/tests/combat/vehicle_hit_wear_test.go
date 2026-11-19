package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABladeWearsOnlyOnALivingTarget: a blow on a boat or a minecart costs
// the blade no wear; one on an armour stand, a living body, costs it one.
// vanilla: Player.itemAttackInteraction
func TestABladeWearsOnlyOnALivingTarget(t *testing.T) {
	t.Parallel()
	srv, attacker, _ := feedbackFixture(t)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})

	strike := func(name string, target int32, want int32) {
		t.Helper()
		servertest.AwaitFullSwing(t, srv, "Fencer")
		attacker.Settle()
		attacker.Attack(target)
		attacker.Settle()
		if got := srv.InventorySlot("Fencer", player.HotbarSlot).Damage; got != want {
			t.Errorf("the blade's wear after a blow on the %s = %d, want %d", name, got, want)
		}
	}
	strike("boat", srv.SpawnBoat(4.5, 64, 5.5, "minecraft:oak_boat"), 0)
	strike("minecart", srv.SpawnMinecart(4.5, 64, 5.5), 0)
	strike("armour stand", srv.SpawnArmorStand(4.5, 64, 5.5), 1)
}
