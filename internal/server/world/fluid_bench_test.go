package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

var fluidWallSink bool
var fluidCollisionSink *fluidCollision
var fluidSlopeSink int

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

func BenchmarkFluidSpreadLip(b *testing.B) {
	for _, tc := range []struct {
		name   string
		dx, dz int32
	}{
		{name: "flat"},
		{name: "north", dz: -1},
		{name: "west", dx: -1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { w.Close() })
			for x := int32(-1); x <= 0; x++ {
				for z := int32(-1); z <= 0; z++ {
					w.Acquire(ChunkPos{X: x, Z: z})
				}
			}
			const y = 64
			source := waterByLevel[0]
			w.SetBlock(0, y, 0, source)
			// Flowing water is eligible for slope selection but cannot be replaced sideways.
			for _, d := range horizontal4 {
				w.SetBlock(d[0], y, d[1], waterByLevel[1])
			}
			if tc.dx != 0 || tc.dz != 0 {
				w.SetBlock(tc.dx, y-1, tc.dz, airState)
			}
			p := NewPhysics(w)
			fs := fluidHolding(source)
			var changes []BlockChange
			b.ReportAllocs()
			for b.Loop() {
				p.spreadToSides(0, y, 0, source, fs, &changes)
			}
			if len(changes) != 0 {
				b.Fatalf("spread changed %d blocks in the fixture", len(changes))
			}
		})
	}
}

// BenchmarkFluidSlopeLayouts measures the four first-step slope searches of a
// water source at (0,64,0) on a 13x13 stone floor with no hole, a far hole at
// (3,63,2), and a near hole at (0,63,2).
func BenchmarkFluidSlopeLayouts(b *testing.B) {
	water := registry.MustState("minecraft:water", nil)
	for _, tc := range []struct {
		name string
		hole *[3]int32
		want int
	}{
		{"flat", nil, 1000},
		{"far_hole", &[3]int32{3, 63, 2}, 4},
		{"near_hole", &[3]int32{0, 63, 2}, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { w.Close() })
			for cx := int32(-1); cx <= 1; cx++ {
				for cz := int32(-1); cz <= 1; cz++ {
					w.Acquire(ChunkPos{X: cx, Z: cz})
				}
			}
			for x := int32(-spreadReach); x <= spreadReach; x++ {
				for z := int32(-spreadReach); z <= spreadReach; z++ {
					w.SetBlock(x, 63, z, stoneState)
					w.SetBlock(x, 64, z, airState)
				}
			}
			w.SetBlock(0, 64, 0, water)
			if tc.hole != nil {
				w.SetBlock(tc.hole[0], tc.hole[1], tc.hole[2], airState)
			}
			p := NewPhysics(w)
			reach := p.behaviourOf(fluidWater).slopeFindDistance
			search := func() int {
				ctx := p.newSpreadContext(0, 64, 0, fluidWater)
				lowest := 1000
				for i, d := range horizontal4 {
					tx, tz := d[0], d[1]
					ctx.state(tx, tz)
					distance := 0
					if !ctx.isHole(tx, tz) {
						ti, _ := ctx.index(tx, tz)
						distance = ctx.slopeDistance(ti, 1, i^2, min(reach, lowest))
					}
					if distance < lowest {
						lowest = distance
					}
				}
				return lowest
			}
			if got := search(); got != tc.want {
				b.Fatalf("%s nearest distance = %d, want %d", tc.name, got, tc.want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				fluidSlopeSink = search()
			}
		})
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
