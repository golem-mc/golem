package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

var fluidWallSink bool
var fluidCollisionSink *fluidCollision

func BenchmarkFluidWall(b *testing.B) {
	for _, tc := range []struct {
		name     string
		block    registry.Block
		axis     int
		positive bool
	}{
		{"slab_side", registry.BlockOakSlab, 0, true},
		{"stairs_side", registry.BlockOakStairs, 2, false},
		{"slab_down", registry.BlockOakSlab, 1, false},
		{"trapdoor_down", registry.BlockOakTrapdoor, 1, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dst := tc.block.DefaultState()
			fluidWallSink = passWall(airState, tc.axis, tc.positive, dst)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				fluidWallSink = passWall(airState, tc.axis, tc.positive, dst)
			}
		})
	}
}

// BenchmarkFluidWallFallback measures a shape-cache miss and warmed wall checks
// for waterloggable blocks that reach collisionGeometry's unmodelled fallback.
func BenchmarkFluidWallFallback(b *testing.B) {
	for _, tc := range []struct {
		name  string
		block registry.Block
	}{
		{"copper_grate", registry.BlockCopperGrate},
		{"oak_leaves", registry.BlockOakLeaves},
		{"barrier", registry.BlockBarrier},
		{"conduit", registry.BlockConduit},
		{"decorated_pot", registry.BlockDecoratedPot},
		{"azalea", registry.BlockAzalea},
		{"flowering_azalea", registry.BlockFloweringAzalea},
	} {
		b.Run(tc.name, func(b *testing.B) {
			state := tc.block.DefaultState()
			if !openBush[tc.block] {
				var ok bool
				if state, ok = registry.WithBool(state, registry.PropWaterlogged, true); !ok {
					b.Fatal("block has no waterlogged property")
				}
			}
			_, props, _ := registry.BlockStateName(state)
			if _, modelled := modelledGeometry(state, tc.block, registry.ParseStateProps(props)); modelled || tc.block.NoCollision() || thinPlate(state) {
				b.Fatal("block does not reach the collision fallback")
			}
			// The entity table is global; each fluid shape is still built below.
			_ = CollisionBoxes(state)
			b.Run("construct", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					// Include one invalidating atomic store so every lookup builds
					// the shape, including property parsing and cache publication.
					fluidCollisions[state].Store(nil)
					fluidCollisionSink = collisionOf(state)
				}
			})
			for _, wall := range []struct {
				name     string
				src, dst uint16
			}{
				{"cached_source", state, airState},
				{"cached_destination", airState, state},
			} {
				b.Run(wall.name, func(b *testing.B) {
					fluidWallSink = passWall(wall.src, 0, true, wall.dst)
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						fluidWallSink = passWall(wall.src, 0, true, wall.dst)
					}
				})
			}
		})
	}
}

// BenchmarkFluidClassify runs the fluid paths' block-kind checks over a mix
// of states a spreading flow meets: air, stone, water at two levels, lava, a
// waterlogged and a dry stair, kelp, a bubble column, a door and a ladder.
func BenchmarkFluidClassify(b *testing.B) {
	states := []uint16{
		airState,
		registry.MustState("minecraft:stone", nil),
		registry.MustState("minecraft:water", nil),
		registry.MustState("minecraft:water", map[string]string{"level": "3"}),
		registry.MustState("minecraft:lava", nil),
		registry.MustState("minecraft:oak_stairs", map[string]string{"waterlogged": "true"}),
		registry.MustState("minecraft:oak_stairs", nil),
		registry.MustState("minecraft:kelp", nil),
		registry.MustState("minecraft:bubble_column", nil),
		registry.MustState("minecraft:oak_door", nil),
		registry.MustState("minecraft:ladder", nil),
	}
	source := fluidState{kind: fluidWater, amount: 8}
	b.ReportAllocs()
	n := 0
	for range b.N {
		for _, s := range states {
			if fluidHolding(s).kind != fluidNone {
				n++
			}
			if liquidBlock(s) {
				n++
			}
			if canHoldAny(s) {
				n++
			}
			if canHoldSpecific(s, source) {
				n++
			}
		}
	}
	if n == 0 {
		b.Fatal("no state classified as fluid")
	}
}

// BenchmarkFluidSpreadToSides runs the sideways flood of a water source on a
// floor with a hole three cells off and a few obstacles, which makes the
// slope search walk its whole reach.
func BenchmarkFluidSpreadToSides(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	stone := registry.MustState("minecraft:stone", nil)
	slab := registry.MustState("minecraft:oak_slab", map[string]string{"type": "top"})
	w.SetBlock(3, 63, 0, airState)
	w.SetBlock(-4, 63, 2, airState)
	w.SetBlock(1, 64, 1, stone)
	w.SetBlock(-2, 64, 0, slab)
	src := registry.MustState("minecraft:water", nil)
	fs := fluidState{kind: fluidWater, amount: 8}
	var changes []BlockChange
	b.ReportAllocs()
	for range b.N {
		changes = changes[:0]
		p.spreadToSides(0, 64, 0, src, fs, &changes)
	}
}

// BenchmarkFluidFlood measures 200 ticks of 36 sources flooding a flat world.
func BenchmarkFluidFlood(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { w.Close() })
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	w.FlushLighting()
	source := registry.MustState("minecraft:water", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		p := NewPhysics(w)
		for x := int32(-40); x <= 40; x += 16 {
			for z := int32(-40); z <= 40; z += 16 {
				w.SetBlock(x, 80, z, source)
				p.NotifyPlaced(x, 80, z, airState)
			}
		}
		b.StartTimer()
		for tick := int64(1); tick <= 200; tick++ {
			p.Tick(tick)
		}
		b.StopTimer()
		for x := int32(-56); x <= 56; x++ {
			for z := int32(-56); z <= 56; z++ {
				for y := int32(-64); y <= 90; y++ {
					if fluidHolding(w.BlockAt(x, y, z)).kind == fluidWater {
						w.SetBlock(x, y, z, airState)
					}
				}
			}
		}
		b.StartTimer()
	}
}
