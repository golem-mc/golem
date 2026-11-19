package rules

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TemplateCell is a cell a structure template wrote, with whether it held a
// water source before the write.
type TemplateCell struct {
	X, Y, Z   int32
	WasSource bool
}

// WaterSourceAt reports whether the fluid in the cell is a water source: a
// level-0 water block, a waterlogged block or a water plant.
func WaterSourceAt(w BlockReader, x, y, z int32) bool { return waterAt(w, x, y, z) }

// templateFillOrder is the order a flooded template cell looks for source
// fluid in.
var templateFillOrder = [5][3]int32{{0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

// TemplateWaterlogging waterlogs the blocks a template wrote over water: a
// waterloggable block takes the water of the source it replaced, and one that
// replaced flowing water or air takes it from a neighbouring source that the
// template did not write. It returns the writes it made.
// vanilla: StructureTemplate.placeInWorld, SimpleWaterloggedBlock.placeLiquid
func TemplateWaterlogging(w ShapeWorld, cells []TemplateCell) []world.BlockChange {
	var out []world.BlockChange
	locked := map[[3]int32]bool{}
	var toFill [][3]int32
	waterlog := func(x, y, z int32) {
		state := w.BlockAt(x, y, z)
		if next, ok := WithWaterlogged(state, true); ok && next != state && w.SetBlock(x, y, z, next) {
			out = append(out, world.BlockChange{X: x, Y: y, Z: z, State: next})
		}
	}
	for _, c := range cells {
		pos := [3]int32{c.X, c.Y, c.Z}
		if waterAt(w, c.X, c.Y, c.Z) {
			locked[pos] = true
			continue
		}
		name, _, ok := registry.BlockStateName(w.BlockAt(c.X, c.Y, c.Z))
		if !ok || !registry.HasProperty(name, "waterlogged") {
			continue
		}
		if c.WasSource {
			waterlog(c.X, c.Y, c.Z)
			continue
		}
		toFill = append(toFill, pos)
	}
	for filled := true; filled && len(toFill) > 0; {
		filled = false
		kept := toFill[:0]
		for _, pos := range toFill {
			source, water := waterAt(w, pos[0], pos[1], pos[2]), true
			for i := 0; i < len(templateFillOrder) && !source; i++ {
				d := templateFillOrder[i]
				n := [3]int32{pos[0] + d[0], pos[1] + d[1], pos[2] + d[2]}
				if locked[n] {
					continue
				}
				switch {
				case waterAt(w, n[0], n[1], n[2]):
					source, water = true, true
				case IsLavaSource(w.BlockAt(n[0], n[1], n[2])):
					source, water = true, false
				}
			}
			if !source {
				kept = append(kept, pos)
				continue
			}
			if water {
				waterlog(pos[0], pos[1], pos[2])
			}
			filled = true
		}
		toFill = kept
	}
	return out
}

// IsLavaSource reports whether a block state is a lava source (level 0).
func IsLavaSource(state uint16) bool {
	f := blocklogic.FluidOf(state)
	return f.Kind == blocklogic.FluidLava && f.IsSource()
}
