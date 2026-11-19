package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The hit triggers: used_totem, entity_hurt_player and target_hit.

// TestATotemSaveObtainsPostmortal hurts a player holding a totem of
// undying: a blow it survives does not obtain totem_of_undying, the lethal
// one the totem turns does. vanilla: LivingEntity.checkTotemDeathProtection
func TestATotemSaveObtainsPostmortal(t *testing.T) {
	t.Parallel()
	const name, totem = "Survivor", "minecraft:adventure/totem_of_undying"
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), name)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:totem_of_undying"), Count: 1})
	srv.StopTick()
	if !srv.Hurt(name, 2) {
		t.Fatal("the first blow did not land")
	}
	if srv.AdvancementObtained(name, totem, "used_totem") {
		t.Fatal("a blow the player survived obtained totem_of_undying")
	}
	srv.OpenHurtWindow(name, 0)
	srv.Hurt(name, 40)
	if !srv.AdvancementObtained(name, totem, "used_totem") {
		t.Error("the totem's save from a lethal blow did not obtain totem_of_undying")
	}
}
