package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A powered floor lever on a hopper locks it; the hopper's enabled write then
// shape-updates the lever, whose support (the hollow hopper top) is gone.
// vanilla: HopperBlock.checkPoweredState, FaceAttachedHorizontalDirectionalBlock.updateShape
func TestHopperEnabledChangeDropsFloorLever(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 64, 0, "hopper[facing=down,enabled=true]")
	servertest.Setblock(t, c, 5, 65, 0, "lever[face=floor,facing=north,powered=true]")
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:hopper", map[string]string{"enabled": "true"})
}
