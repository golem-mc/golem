package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSpearJabTriggersThorns jabs a player in four Thorns III pieces: the
// victim's thorns answer the jab, hurting the jabber and wearing the
// chestplate.
// vanilla: Player.stabAttack, EnchantmentHelper.doPostAttackEffectsWithItemSource
func TestSpearJabTriggersThorns(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	jabber(t, srv, c, "Jabber")
	v := jabTarget(t, connect, "Knight")
	v.Settle()
	for slot, item := range map[int]string{
		player.ArmorSlot:     "minecraft:diamond_helmet",
		player.ArmorSlot + 1: "minecraft:diamond_chestplate",
		player.ArmorSlot + 2: "minecraft:diamond_leggings",
		player.ArmorSlot + 3: "minecraft:diamond_boots",
	} {
		srv.GiveSlot("Knight", slot, servertest.EnchantedStack(t, item, "minecraft:thorns", 3))
	}
	srv.SetWearSeed(1) // the first piece's roll fires its thorns

	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if srv.PlayerHealth("Jabber") >= 20 {
		t.Error("the thorns did not hurt the jabber")
	}
	if srv.PlayerHealth("Knight") >= 20 {
		t.Error("the jab did not hurt the thorns wearer")
	}
	if wear := srv.InventorySlot("Knight", player.ArmorSlot+1).Damage; wear == 0 {
		t.Error("the chestplate took no wear from the jab")
	}
}
