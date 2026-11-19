package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestFluidCollisionReadsACarpetAsOpenAndACoralBlockAsFull: a carpet's plate
// covers no side face, so the flow reads it as open, while a coral block fills
// the cell.
func TestFluidCollisionReadsACarpetAsOpenAndACoralBlockAsFull(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block    string
		fullCube bool
	}{
		{"minecraft:white_carpet", false},
		{"minecraft:tube_coral_block", true},
		{"minecraft:stone", true},
	} {
		c := collisionOf(registry.MustState(tc.block, nil))
		if c.fullCube != tc.fullCube || (!tc.fullCube && len(c.boxes) != 0) {
			t.Errorf("collisionOf(%s) = %v, full cube %v; want full cube %v and no boxes when open", tc.block, c.boxes, c.fullCube, tc.fullCube)
		}
	}
}

func TestFluidWallFullCubeAndEmpty(t *testing.T) {
	t.Parallel()
	stone := registry.BlockStone.DefaultState()
	for _, tc := range []struct {
		src, dst uint16
		pass     bool
	}{
		{airState, airState, true},
		{stone, airState, false},
		{airState, stone, false},
		{stone, stone, false},
		{registry.StateCount, airState, true},
		{airState, ^uint16(0), true},
		{registry.StateCount, stone, false},
		{stone, ^uint16(0), false},
	} {
		for axis := range 3 {
			for _, positive := range []bool{false, true} {
				if got := passWall(tc.src, axis, positive, tc.dst); got != tc.pass {
					t.Errorf("passWall(%d, %d, %v, %d) = %v, want %v", tc.src, axis, positive, tc.dst, got, tc.pass)
				}
			}
		}
	}
}

func TestFluidWallWaterloggableCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block registry.Block
		pass  bool
	}{
		{registry.BlockCopperGrate, false},
		{registry.BlockOakLeaves, false},
		{registry.BlockBarrier, false},
		{registry.BlockConduit, true},
		{registry.BlockDecoratedPot, true},
	} {
		for _, wet := range []bool{false, true} {
			state, ok := registry.WithBool(tc.block.DefaultState(), registry.PropWaterlogged, wet)
			if !ok {
				t.Fatalf("block %d has no waterlogged property", tc.block)
			}
			for axis := range 3 {
				for _, positive := range []bool{false, true} {
					if got := passWall(state, axis, positive, airState); got != tc.pass {
						t.Errorf("state %d to air, axis %d positive %v: pass = %v, want %v", state, axis, positive, got, tc.pass)
					}
					if got := passWall(airState, axis, positive, state); got != tc.pass {
						t.Errorf("air to state %d, axis %d positive %v: pass = %v, want %v", state, axis, positive, got, tc.pass)
					}
				}
			}
		}
	}
}

type fluidWallTestCase struct {
	src, dst uint16
	axis     int
	positive bool
	pass     bool
}

func fluidWallTestCases() []fluidWallTestCase {
	states := []uint16{
		airState, registry.BlockStone.DefaultState(),
		registry.BlockOakSlab.DefaultState(),
		registry.MustState("minecraft:oak_slab", map[string]string{"type": "top"}),
		registry.MustState("minecraft:oak_slab", map[string]string{"type": "double"}),
		registry.BlockOakStairs.DefaultState(),
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "inner_left"}),
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "inner_right", "half": "top"}),
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "outer_left"}),
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "outer_right", "half": "top"}),
		registry.BlockOakTrapdoor.DefaultState(),
		registry.MustState("minecraft:oak_trapdoor", map[string]string{"open": "true"}),
		registry.BlockChest.DefaultState(),
		registry.BlockSnow.DefaultState(),
		registry.BlockScaffolding.DefaultState(),
		registry.BlockPistonHead.DefaultState(),
		registry.StateCount, ^uint16(0),
	}
	var cases []fluidWallTestCase
	for _, src := range states {
		for _, dst := range states {
			for axis := range 3 {
				for _, positive := range []bool{false, true} {
					s, d := collisionOf(src), collisionOf(dst)
					pass := false
					if !s.fullCube && !d.fullCube {
						pass = len(s.boxes) == 0 && len(d.boxes) == 0 || !mergedFaceOccludes(s.boxes, d.boxes, axis, positive)
					}
					cases = append(cases, fluidWallTestCase{src, dst, axis, positive, pass})
				}
			}
		}
	}
	return cases
}

func TestFluidWallCacheMatchesGeometry(t *testing.T) {
	for _, tc := range fluidWallTestCases() {
		// Check both a possible miss and the subsequent hit.
		for range 2 {
			if got := passWall(tc.src, tc.axis, tc.positive, tc.dst); got != tc.pass {
				t.Fatalf("passWall(%d, %d, %v, %d) = %v, geometry wants %v", tc.src, tc.axis, tc.positive, tc.dst, got, tc.pass)
			}
		}
	}
}

func TestFluidWallCacheCollisions(t *testing.T) {
	var buckets [256][]fluidWallTestCase
	for _, tc := range fluidWallTestCases() {
		s, d := collisionOf(tc.src), collisionOf(tc.dst)
		if s.fullCube || d.fullCube || len(s.boxes) == 0 && len(d.boxes) == 0 {
			continue
		}
		direction := uint64(tc.axis * 2)
		if tc.positive {
			direction++
		}
		key := uint64(tc.src) | uint64(tc.dst)<<16 | direction<<32
		index := key * 0x9e3779b97f4a7c15 >> 56
		buckets[index] = append(buckets[index], tc)
	}
	var pair []fluidWallTestCase
	for _, bucket := range buckets {
		for _, tc := range bucket {
			if tc.pass != bucket[0].pass {
				pair = []fluidWallTestCase{bucket[0], tc}
				break
			}
		}
		if pair != nil {
			break
		}
	}
	if pair == nil {
		t.Fatal("no colliding nontrivial faces with opposite results")
	}
	for range 20 {
		for _, tc := range pair {
			if got := passWall(tc.src, tc.axis, tc.positive, tc.dst); got != tc.pass {
				t.Fatalf("eviction reused a mismatched key: %+v got %v", tc, got)
			}
		}
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for n := range 1000 {
				tc := pair[(n+worker)%len(pair)]
				if got := passWall(tc.src, tc.axis, tc.positive, tc.dst); got != tc.pass {
					t.Errorf("concurrent eviction reused a mismatched key: %+v got %v", tc, got)
					return
				}
			}
		})
	}
	wg.Wait()
}
