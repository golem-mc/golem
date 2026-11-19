package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestOpeningAContainerSendsNoBlockEntityData opens a chest and a furnace
// and checks that no Block Entity Data crosses the wire with the window: the
// items reach the client in Set Container Content alone. vanilla:
// ServerPlayer.openMenu
func TestOpeningAContainerSendsNoBlockEntityData(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)

	for i, item := range []string{"minecraft:chest", "minecraft:furnace"} {
		x := int32(4 + 2*i)
		w.SetCreativeSlot(36, servertest.ItemStack(t, item, 1))
		w.PlaceBlock(x, 63, 0, protocol.FaceTop)
		w.SettleKeeping()
		srv.TickSurvival()
		w.ExpectBlockAck()
	}
	for i, menu := range []int32{protocol.MenuGeneric9x3, protocol.MenuFurnace} {
		x := int32(4 + 2*i)
		w.UseBlock(x, 64, 0)
		w.ExpectContainerOpen(menu, false, x, 64, 0)
		w.SettleAndCollect(func(id int32, _ []byte) {
			if id == protocol.IDPlayBlockEntityData {
				t.Errorf("window %d: block entity data sent with the open", menu)
			}
		})
		w.CloseWindow()
		w.ExpectWindowZero()
	}
}
