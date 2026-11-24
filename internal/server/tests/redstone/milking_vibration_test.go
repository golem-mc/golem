package redstone

// Milking or stewing an animal posts ENTITY_INTERACT (frequency 6) at it.
// vanilla: Mob.interact, AbstractCow.mobInteract, Goat.mobInteract, MushroomCow.mobInteract

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestMilkingVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mob, tool, filled string
		protoType               int32
		calf                    bool
	}{
		{"cow", "minecraft:cow", "minecraft:bucket", "minecraft:milk_bucket", protocol.EntityTypeCow, false},
		{"goat", "minecraft:goat", "minecraft:bucket", "minecraft:milk_bucket", protocol.EntityTypeGoat, false},
		{"mooshroom bucket", "minecraft:mooshroom", "minecraft:bucket", "minecraft:milk_bucket", protocol.EntityTypeMooshroom, false},
		{"mooshroom bowl", "minecraft:mooshroom", "minecraft:bowl", "minecraft:mushroom_stew", protocol.EntityTypeMooshroom, false},
		{"calf", "minecraft:cow", "minecraft:bucket", "minecraft:bucket", protocol.EntityTypeCow, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalSensorScene(t)
			tool := servertest.MustItemID(t, tc.tool)
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: tool, Count: 1})
			c.Settle()
			tag := nbt.Compound{"NoAI": nbt.Byte(1)}
			if tc.calf {
				tag["Age"] = nbt.Int(-24000)
			}
			if srv.SpawnMobTagged(tc.mob, 1.5, 64, 4.5, tag) == 0 {
				t.Fatal("spawn refused")
			}
			m := c.ExpectMobSpawn(tc.protoType)
			servertest.HoldTicks(t, srv)
			c.UseOn(m.EntityID)
			c.Settle()
			servertest.RunTicks(t, srv, 10)
			if got := srv.InventorySlot("Actor", player.HotbarSlot).ItemID; got != servertest.MustItemID(t, tc.filled) {
				t.Errorf("hand holds item %d, want %s", got, tc.filled)
			}
			// The animal is three blocks from the sensor, the player four:
			// power 15 - floor(15/8 * 3) = 10 only if posted at the animal.
			want, phase, power := "6", "active", "10"
			if tc.calf {
				want, phase, power = "0", "inactive", "0"
			}
			expectComparator(t, srv, want)
			expectSensor(t, srv, 4, 64, 4, phase, power)
		})
	}
}
