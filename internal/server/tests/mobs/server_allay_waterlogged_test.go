package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestAnAllayCrossesAWaterloggedLadderToCollectAnItem(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	glass := registry.BlockGlass.DefaultState()
	for x := int32(0); x <= 10; x++ {
		for y := int32(63); y <= 65; y++ {
			for z := int32(0); z <= 2; z++ {
				state := glass
				if x > 0 && x < 10 && y == 64 && z == 1 {
					state = 0
				}
				srv.SetBlock(x, y, z, state)
			}
		}
	}
	// Signs contain the ladder's water so the approach stays dry.
	for _, x := range []int32{4, 6} {
		srv.SetBlock(x, 64, 1, registry.MustState("minecraft:oak_wall_sign", map[string]string{"facing": "south", "waterlogged": "false"}))
	}
	srv.SetBlock(5, 64, 0, registry.BlockStone.DefaultState())
	srv.SetBlock(5, 64, 1, registry.MustState("minecraft:ladder", map[string]string{"facing": "south", "waterlogged": "true"}))
	id := srv.SpawnMob(allay, 2.5, 64, 1.5)
	c.ExpectMobSpawn(allayType())
	srv.SetMobHand(id, protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1})
	srv.SpawnItemEntity(8.5, 64, 1.5, "minecraft:diamond", 1)
	servertest.UntilTicks(t, srv, 400, "the allay across the waterlogged ladder with its diamond", func() bool {
		x, _, _, ok := srv.MobPos(id)
		return ok && x > 6 && len(srv.MobInventory(id)) == 1
	})
}
