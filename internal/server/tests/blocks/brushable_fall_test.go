package blocks

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BrushableBlock.tick, BrushableBlock.onBrokenAfterFall
func TestBrushableBlocksBreakAfterFalling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	const floorY = 100
	cases := []struct {
		x     int32
		block registry.Block
	}{
		{5, registry.BlockSuspiciousSand},
		{9, registry.BlockSuspiciousGravel},
	}
	for _, tc := range cases {
		servertest.Setblock(t, c, tc.x, floorY, 0, "stone")
		servertest.Setblock(t, c, tc.x, floorY+2, 0, "stone")
		servertest.Setblock(t, c, tc.x, floorY+3, 0, tc.block.Name())
	}
	c.Settle()
	var destroys [2]atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		for i, tc := range cases {
			// The break is posted at the body's centre, not the cell's centre.
			if event == "minecraft:block_destroy" && x == float64(tc.x)+0.5 && z == 0.5 &&
				math.Abs(y-(floorY+1.49)) < 1e-6 {
				destroys[i].Add(1)
			}
		}
	})
	for _, tc := range cases {
		servertest.Setblock(t, c, tc.x, floorY+2, 0, "air")
	}
	var breaks [2]int
	var falls int
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if _, kind := servertest.SpawnEntityHead(data); kind == protocol.EntityTypeFallingBlock {
				falls++
			}
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() != protocol.WorldEventBlockBreak {
				return false
			}
			x, y, z := r.ReadPosition()
			state := r.ReadI32()
			for i, tc := range cases {
				if x == tc.x && y == floorY+1 && z == 0 {
					breaks[i]++
					if state != int32(tc.block.DefaultState()) {
						t.Errorf("%s break state = %d, want %d", tc.block.Name(), state, tc.block.DefaultState())
					}
				}
			}
		}
		return false
	})
	if falls != len(cases) {
		t.Errorf("falling block spawns = %d, want %d", falls, len(cases))
	}
	for i, tc := range cases {
		servertest.ExpectAir(t, srv, [3]int32{tc.x, floorY + 1, 0})
		if breaks[i] != 1 {
			t.Errorf("%s landing break events = %d, want 1", tc.block.Name(), breaks[i])
		}
		if got := destroys[i].Load(); got != 1 {
			t.Errorf("%s block_destroy events at body centre = %d, want 1", tc.block.Name(), got)
		}
		for _, item := range srv.ItemsInDimension(world.DimensionName) {
			dx, dy, dz := item.X-(float64(tc.x)+0.5), item.Y-(floorY+1), item.Z-0.5
			if dx*dx+dy*dy+dz*dz <= 4 {
				t.Errorf("%s left item within 2 blocks: %+v", tc.block.Name(), item)
			}
		}
	}
}
