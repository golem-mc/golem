package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAComparatorReadsACopperGolemStatuesPose: standing 1, sitting 2,
// running 3, star 4, following /setblock and a pose click alike.
// vanilla: CopperGolemStatueBlock.getAnalogOutputSignal
func TestAComparatorReadsACopperGolemStatuesPose(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:copper_golem_statue[copper_golem_pose=standing]")
	c.ReadTicks(srv, 4, func(int32, []byte) bool { return false })
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:copper_golem_statue[copper_golem_pose=star]")
	c.ReadTicks(srv, 4, func(int32, []byte) bool { return false })
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "4")

	servertest.Setblock(t, c, 4, 64, 0, "minecraft:copper_golem_statue[copper_golem_pose=standing]")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stick"), Count: 1})
	c.UseBlock(4, 64, 0)
	c.ReadTicks(srv, 4, func(int32, []byte) bool { return false })
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "2")
}
