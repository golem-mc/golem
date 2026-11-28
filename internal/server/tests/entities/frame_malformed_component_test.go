package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEntityDataModifyDropsMalformedStackComponents checks that writing a
// malformed nested stack component onto an item frame's Item or an armor
// stand's equipment keeps the item without the refused component and sends
// valid metadata/equipment so a watching client stays connected.
// vanilla: ItemStack.MAP_CODEC, DataComponentPatch.CODEC, TagValueInput.read
func TestEntityDataModifyDropsMalformedStackComponents(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)
	r.Ticks = 0

	t.Run("item frame bundle_contents with air", func(t *testing.T) {
		target := "@e[type=minecraft:item_frame,limit=1]"
		r.Run(`summon minecraft:item_frame 4.5 70 4.5 {Fixed:1b,Facing:3b,Item:{id:"minecraft:bundle",count:1}}`)
		r.Run(`data modify entity ` + target + ` Item.components set value {"minecraft:bundle_contents":[{id:"minecraft:air"}]}`)
		if !dataGetIs(r.Run("data get entity "+target+" Item"), `{count: 1, id: "minecraft:bundle"}`) {
			t.Errorf("frame Item did not drop refused bundle_contents while keeping the bundle")
		}
		r.Run("kill " + target)
	})

	t.Run("armor stand charged_projectiles with air", func(t *testing.T) {
		target := "@e[type=minecraft:armor_stand,limit=1]"
		r.Run(`summon minecraft:armor_stand 4.5 70 4.5 {NoGravity:1b,equipment:{mainhand:{id:"minecraft:crossbow",count:1}}}`)
		r.Run(`data modify entity ` + target + ` equipment.mainhand.components set value {"minecraft:charged_projectiles":[{id:"minecraft:air"}]}`)
		if !dataGetIs(r.Run("data get entity "+target+" equipment.mainhand"), `{count: 1, id: "minecraft:crossbow"}`) {
			t.Errorf("stand mainhand did not drop refused charged_projectiles while keeping the crossbow")
		}
		r.Run("kill " + target)
	})
}
