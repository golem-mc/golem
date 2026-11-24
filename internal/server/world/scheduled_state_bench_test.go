package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkScheduledStateWrites visits blocks whose properties change on a scheduled tick.
func BenchmarkScheduledStateWrites(b *testing.B) {
	for _, kind := range []string{"leaf", "scaffolding", "dried_ghast", "moss_carpet", "rail"} {
		b.Run(kind, func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			var state uint16
			var visit func(*[]BlockChange)
			switch kind {
			case "leaf":
				state = registry.MustState("minecraft:oak_leaves", map[string]string{"persistent": "true", "waterlogged": "true"})
				w.SetBlock(9, GroundLevel, 8, registry.BlockOakLog.DefaultState())
				visit = func(changes *[]BlockChange) { p.leafTick(8, GroundLevel, 8, state, changes) }
			case "scaffolding":
				state = registry.MustState("minecraft:scaffolding", map[string]string{"distance": "5"})
				visit = func(changes *[]BlockChange) {
					var falls []FallingSpawn
					p.scaffoldingTick(8, GroundLevel, 8, state, changes, &falls)
				}
			case "dried_ghast":
				state = registry.MustState("minecraft:dried_ghast", map[string]string{"hydration": "2", "facing": "west"})
				visit = func(changes *[]BlockChange) { p.driedGhastTick(8, GroundLevel, 8, state, changes) }
			case "moss_carpet":
				state = registry.BlockPaleMossCarpet.DefaultState()
				w.SetBlock(9, GroundLevel, 8, registry.BlockStone.DefaultState())
				visit = func(changes *[]BlockChange) { MossyCarpetState(w, state, 8, GroundLevel, 8, true) }
			case "rail":
				state = registry.MustState("minecraft:rail", map[string]string{"waterlogged": "true"})
				w.SetBlock(9, GroundLevel, 8, registry.BlockRail.DefaultState())
				visit = func(changes *[]BlockChange) {
					r, _ := p.railAround(8, GroundLevel, 8, state)
					r.updateDir(true, false, changes)
				}
			}
			b.ReportAllocs()
			var changes []BlockChange
			for b.Loop() {
				w.SetBlock(8, GroundLevel, 8, state)
				changes = changes[:0]
				visit(&changes)
			}
		})
	}
}

// BenchmarkTemplateStateTransforms rotates and mirrors the states placed by structures.
func BenchmarkTemplateStateTransforms(b *testing.B) {
	states := []uint16{
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "inner_left", "waterlogged": "true"}),
		registry.MustState("minecraft:oak_fence", map[string]string{"north": "true"}),
		registry.MustState("minecraft:rail", map[string]string{"shape": "ascending_east"}),
		registry.BlockJigsaw.DefaultState(),
		registry.BlockOakSign.DefaultState(),
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, state := range states {
			RotateState(MirrorState(state, MirrorLeftRight), RotateClockwise90)
		}
	}
}

// BenchmarkWaterloggedBlockTicks books and dispatches ticks at waterlogged blocks.
func BenchmarkWaterloggedBlockTicks(b *testing.B) {
	for _, kind := range []string{"dripleaf", "detector_rail", "scaffolding"} {
		b.Run(kind, func(b *testing.B) {
			w, err := NewWorld(b.TempDir(), NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			var state uint16
			var act func()
			ticks := int64(100)
			switch kind {
			case "dripleaf":
				state = registry.MustState("minecraft:big_dripleaf", map[string]string{"facing": "west", "waterlogged": "true"})
				act = func() { p.DripleafProjectiled(8, GroundLevel, 8) }
			case "detector_rail":
				state = registry.MustState("minecraft:detector_rail", map[string]string{"shape": "north_south", "waterlogged": "true"})
				ticks = 20
				act = func() {
					p.SetRailCartSource(railCartsStub{crossing: true})
					var changes []BlockChange
					var effects []Effect
					p.EntityInside(8, GroundLevel, 8, &changes, &effects)
					p.SetRailCartSource(railCartsStub{})
				}
			case "scaffolding":
				state = registry.MustState("minecraft:scaffolding", map[string]string{"distance": "5", "waterlogged": "true"})
				for _, side := range horizontal4 {
					w.SetBlock(8+side[0], GroundLevel, 8+side[1], registry.BlockStone.DefaultState())
				}
				ticks = 5
				act = func() { p.NotifyPlaced(8, GroundLevel, 8, airState) }
			}
			b.ReportAllocs()
			var tick int64
			for b.Loop() {
				w.SetBlock(8, GroundLevel, 8, state)
				act()
				for end := tick + ticks; tick < end; {
					tick++
					p.Tick(tick)
				}
			}
		})
	}
}
