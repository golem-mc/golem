package blocks

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LightBlock.useWithoutItem
func TestLightBlockClickCyclesOnlyForGameMaster(t *testing.T) {
	for _, op := range []bool{true, false} {
		name := "non-operator"
		if op {
			name = "operator"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var setup []func(*server.Server)
			if op {
				setup = append(setup, servertest.OpsListing(t, "Actor"))
			}
			srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", setup...)
			actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemLight), Count: 1})
			servertest.PrepareGameMasterPlacement(actor)
			state, _ := registry.WithBool(registry.BlockLight.DefaultState(), registry.PropWaterlogged, true)
			srv.SetBlock(4, 64, 5, state)
			actor.Settle()

			for _, next := range []int{0, 1} {
				actor.UseBlock(4, 64, 5)
				actor.Settle()
				want := 15
				if op {
					want = next
				}
				servertest.ExpectBlockNamed(t, srv, 4, 64, 5, "minecraft:light", map[string]string{"level": strconv.Itoa(want), "waterlogged": "true"})
				servertest.ExpectBlockNamed(t, srv, 4, 65, 5, "minecraft:air", nil)
			}
		})
	}
}
