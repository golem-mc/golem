package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockInput.place, FireBlock.updateShape, BaseFireBlock.getState
func TestSetblockFireResolvesFromItsBase(t *testing.T) {
	t.Parallel()
	srv, c := setblockActor(t)
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)

	for i, tc := range []struct {
		base, requested string
		want            uint16
	}{
		{"soul_soil", "fire", registry.BlockSoulFire.DefaultState()},
		{"soul_sand", "fire", registry.BlockSoulFire.DefaultState()},
		{"netherrack", "fire", registry.BlockFire.DefaultState()},
		{"soul_soil", "fire[age=7,north=true]", registry.BlockSoulFire.DefaultState()},
		{"soul_sand", "fire[age=7]", registry.BlockSoulFire.DefaultState()},
		{"netherrack", "fire[age=7]", registry.MustState("minecraft:fire", map[string]string{"age": "7"})},
		{"netherrack", "fire[north=true]", registry.MustState("minecraft:fire", map[string]string{"north": "true"})},
		{"soul_soil", "fire strict", registry.BlockFire.DefaultState()},
		{"soul_sand", "fire strict", registry.BlockFire.DefaultState()},
	} {
		t.Run(tc.base+"/"+tc.requested, func(t *testing.T) {
			x, z := int32(3+2*(i%5)), int32(3+2*(i/5))
			servertest.Setblock(t, c, x, 63, z, tc.base)
			c.ChatCommand(fmt.Sprintf("setblock %d 64 %d %s", x, z, tc.requested))
			var got uint16
			if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
				for _, ch := range servertest.BlockChangesIn(t, id, data) {
					if ch.X == x && ch.Y == 64 && ch.Z == z {
						got = ch.State
						return true
					}
				}
				return false
			}) {
				t.Fatal("no fire block update arrived")
			}
			if got != tc.want {
				t.Errorf("fire sent to client = %d (%s), want %d (%s)", got, registry.BlockOf(got).Name(), tc.want, registry.BlockOf(tc.want).Name())
			}
			if state := srv.BlockAt(x, 64, z); state != tc.want {
				t.Errorf("placed fire = %d, want %d", state, tc.want)
			}
		})
	}
}
