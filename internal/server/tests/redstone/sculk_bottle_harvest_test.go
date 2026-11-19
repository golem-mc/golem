package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: BottleItem.use
func TestASurvivalPlayerFillingAWaterBottleMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.SetBlock(0, 63, 4, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:glass_bottle"), Count: 1})
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	expectComparator(t, srv, "12") // fluid_pickup
}

// vanilla: Consumable.onConsume
func TestASurvivalPlayerFinishingADrinkMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.GiveSlot("Actor", player.HotbarSlot, servertest.WaterBottle(t))
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	// The drink takes 32 ticks, then the vibration flies.
	servertest.RunTicks(t, srv, 45)
	expectComparator(t, srv, "8") // drink
}

// vanilla: PotionItem.useOn, PumpkinBlock.useItemOn, BeehiveBlock.useItemOn
func TestASurvivalPlayersBlockHarvestMakesAVibration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, block, item, frequency string
		props                        map[string]string
	}{
		{name: "water bottle on dirt", block: "dirt", item: "potion", frequency: "13"},
		{name: "carve pumpkin", block: "pumpkin", item: "shears", frequency: "6"},
		{name: "shear hive", block: "beehive", item: "shears", frequency: "6", props: map[string]string{"honey_level": "5"}},
		{name: "bottle honey", block: "beehive", item: "glass_bottle", frequency: "12", props: map[string]string{"honey_level": "5"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:"+tt.block, tt.props))
			stack := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:"+tt.item), Count: 1}
			if tt.item == "potion" {
				stack = servertest.WaterBottle(t)
			}
			srv.GiveSlot("Actor", player.HotbarSlot, stack)
			c.Settle()
			placeSensor(t, srv)
			c.UseBlock(2, 64, 4)
			c.Settle()
			servertest.RunTicks(t, srv, 10)
			expectComparator(t, srv, tt.frequency)
		})
	}
}
