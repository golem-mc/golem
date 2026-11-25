package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestComparatorModeTogglesKeepItsInputAndFacing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	// Equal front and side signals distinguish compare from subtract mode.
	srv.SetBlock(5, 64, 1, servertest.MustState(t, "minecraft:redstone_block", nil))
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:respawn_anchor[charges=4]")
	for i, tc := range []struct{ mode, power, powered string }{
		{"compare", "15", "true"}, {"subtract", "0", "false"}, {"compare", "15", "true"},
	} {
		c.ReadTicks(srv, 4, func(int32, []byte) bool { return false })
		servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:comparator", map[string]string{
			"mode": tc.mode, "facing": "west", "powered": tc.powered,
		})
		servertest.ExpectWirePower(t, srv, 6, 64, 0, tc.power)
		if i < 2 {
			c.UseBlock(5, 64, 0)
		}
	}
}

func TestComparatorClickDropsRepeaterAboveIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	// strict skips the neighbour update that would drop it at placement.
	servertest.Setblock(t, c, 5, 65, 0, "minecraft:repeater[facing=west] strict")
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:repeater", nil)
	c.UseBlock(5, 64, 0)
	c.Settle()
	servertest.ExpectAir(t, srv, [3]int32{5, 65, 0})
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:repeater")
}
