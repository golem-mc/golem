package world

import (
	"github.com/golem-mc/golem/registry"
)

// vanilla: TemplateFeature.place, TemplateFeatureConfiguration
func templateFeature(cfg jobj) featureFn {
	jonly(cfg, "templates", "processors")
	var procs []structureProcessor
	var protected bool
	if cfg.has("processors") {
		procs, protected = loadFeatureProcessors(cfg["processors"])
	}
	type entry struct {
		template  *Template
		rotations []Rotation
	}
	var entries []entry
	var weights []int32
	for _, x := range cfg.arr("templates") {
		e := jobjOf(x)
		jonly(e, "data", "weight")
		data := e.obj("data")
		jonly(data, "id", "rotations")
		t := loadFeatureTemplate(data.str("id"))
		// Entity-bearing templates need placement and loot handling as well.
		if len(t.Entities) != 0 {
			modelFail("feature template %s has entities", data.str("id"))
		}
		rotations := []Rotation{RotateNone, RotateClockwise90, Rotate180, RotateCounterclockwise90}
		if data.has("rotations") {
			rotations = nil
			for _, v := range data.arr("rotations") {
				rot, ok := ParseRotation(v.(string))
				if !ok {
					modelFail("feature template rotation %v", v)
				}
				rotations = append(rotations, rot)
			}
		}
		if len(rotations) == 0 {
			modelFail("feature template has no rotations")
		}
		entries = append(entries, entry{t, rotations})
		weights = append(weights, e.integer("weight"))
	}
	return func(r *decorRegion, p [3]int32) bool {
		e := entries[pickWeighted(r.rng, weights)]
		rotation := e.rotations[r.rng.NextIntN(int32(len(e.rotations)))]
		dx, _, dz := TransformTemplatePos(-e.template.Size[0]/2, 0, -e.template.Size[2]/2, MirrorNone, rotation)
		placed, ok := e.template.Place(TemplatePlacement{
			Origin: [3]int32{p[0] + dx, p[1], p[2] + dz}, Rotation: rotation,
			Integrity: 1, random: r.rng, process: r.featureProcess(procs, protected, p),
		})
		if !ok {
			return false
		}
		r.placeFeatureTemplate(placed)
		for _, b := range placed.Changes {
			r.templateShape(b.X, b.Y, b.Z)
			for _, o := range fenceOffsets {
				r.templateShape(b.X+o[0], b.Y, b.Z+o[1])
			}
		}
		return true
	}
}

// placeFeatureTemplate retains existing water, without spreading sources that
// came from the template into its dry waterloggable blocks.
// vanilla: StructureTemplate.placeInWorld
func (r *decorRegion) placeFeatureTemplate(placed *PlacedTemplate) {
	locked := make(map[[3]int32]bool)
	var toFill [][3]int32
	for i, b := range placed.Changes {
		if b.Y < r.minY || b.Y >= r.minY+r.height || absInt32(b.X>>4-r.nx) > 1 || absInt32(b.Z>>4-r.nz) > 1 {
			continue
		}
		pos := [3]int32{b.X, b.Y, b.Z}
		previousState := r.get(b.X, b.Y, b.Z)
		previous := fluidHolding(previousState)
		state := b.State
		if fluidHolding(state).isSource() {
			locked[pos] = true
		} else if waterloggable[registry.BlockOf(state)] {
			if previous.kind == fluidWater && previous.isSource() && templateTakesWater(state) {
				state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
			}
			if !previous.isSource() {
				toFill = append(toFill, pos)
			}
		}
		// A different block drops its old entity; template NBT replaces even an
		// entity of the same kind via vanilla's temporary barrier block.
		// vanilla: StructureTemplate.placeInWorld
		if registry.BlockOf(previousState) != registry.BlockOf(state) || i < len(placed.Tags) && placed.Tags[i] != nil {
			r.protoAt(b.X, b.Z).dropBlockEntity(b.X&15, b.Y-r.minY, b.Z&15)
		}
		r.set(b.X, b.Y, b.Z, state)
		if i < len(placed.Tags) && placed.Tags[i] != nil {
			r.placeBlockEntity(b.X, b.Y, b.Z, state, placed.Tags[i])
		}
	}
	for filled := true; filled && len(toFill) > 0; {
		filled = false
		remaining := toFill[:0]
		for _, p := range toFill {
			fluid := fluidHolding(r.get(p[0], p[1], p[2]))
			for _, d := range []int{dirUp, dirNorth, dirEast, dirSouth, dirWest} {
				if fluid.isSource() {
					break
				}
				o := dirDelta[d]
				q := [3]int32{p[0] + o[0], p[1] + o[1], p[2] + o[2]}
				if f := fluidHolding(r.get(q[0], q[1], q[2])); f.isSource() && !locked[q] {
					fluid = f
				}
			}
			if fluid.isSource() && waterloggable[registry.BlockOf(r.get(p[0], p[1], p[2]))] {
				if fluid.kind == fluidWater && templateTakesWater(r.get(p[0], p[1], p[2])) {
					state, _ := registry.WithBool(r.get(p[0], p[1], p[2]), registry.PropWaterlogged, true)
					r.set(p[0], p[1], p[2], state)
				}
				filled = true
			} else {
				remaining = append(remaining, p)
			}
		}
		toFill = remaining
	}
}
