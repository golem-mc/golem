package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NearestItemSensor.doTick, LivingEntity.hasLineOfSight
func TestAnAllaySeesAnItemPastAWaterloggedLadder(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	glass := registry.BlockStates["minecraft:glass"]
	for x := int32(0); x <= 10; x++ {
		for y := int32(100); y <= 102; y++ {
			for z := int32(0); z <= 2; z++ {
				state := glass
				if x > 0 && x < 10 && y == 101 && z == 1 {
					state = 0
				}
				srv.SetBlock(x, y, z, state)
			}
		}
	}
	srv.SetBlock(5, 101, 0, registry.BlockStates["minecraft:stone"])
	srv.SetBlock(5, 101, 1, registry.MustState("minecraft:ladder", map[string]string{"facing": "south", "waterlogged": "true"}))
	for _, x := range []int32{4, 6} {
		srv.SetBlock(x, 101, 1, registry.MustState("minecraft:oak_wall_sign", map[string]string{"facing": "south"}))
	}
	id := srv.SpawnMobSeeded(allay, 2.5, 101, 1.5, 1)
	c.ExpectMobSpawn(allayType())
	srv.SetMobHand(id, protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1})
	// Repeat a losing stroll roll: fetching must start from sight, without a
	// random flight first bringing the item into unobstructed view.
	step := func() {
		srv.SeedMob(id, 1)
		servertest.RunTicks(t, srv, 1)
	}
	for range 40 {
		step()
	}
	if x, _, z, _ := srv.MobPos(id); math.Abs(x-2.5) > 0.01 || math.Abs(z-1.5) > 0.01 {
		t.Fatalf("allay wandered without an item: (%v, %v)", x, z)
	}
	item := srv.SpawnItemEntity(8.5, 101, 1.5, "minecraft:diamond", 1)
	for range 160 {
		step()
		found := false
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			found = found || it.ID == item
		}
		if !found {
			return
		}
	}
	x, y, z, _ := srv.MobPos(id)
	t.Fatalf("allay at (%v, %v, %v) did not collect the visible diamond", x, y, z)
}
