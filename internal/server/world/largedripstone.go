package world

import (
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// largeDripstoneFeature is LargeDripstoneFeature.place.
func largeDripstoneFeature(o jobj) featureFn {
	jonly(o, "replaceable_blocks", "floor_to_ceiling_search_range", "column_radius", "height_scale", "max_column_radius_to_cave_height_ratio", "stalactite_bluntness", "stalagmite_bluntness", "wind_speed", "min_radius_for_wind", "min_bluntness_for_wind")
	blocks := speleothemBlocks{base: registry.BlockDripstoneBlock, replaceable: blockSetOf(o["replaceable_blocks"])}
	search := int32(30)
	if o.has("floor_to_ceiling_search_range") {
		search = o.integer("floor_to_ceiling_search_range")
	}
	lo, hi := largeDripstoneRadiusBounds(o["column_radius"])
	scale, downBlunt, upBlunt := compileFloat(o["height_scale"]), compileFloat(o["stalactite_bluntness"]), compileFloat(o["stalagmite_bluntness"])
	speed := compileFloat(o["wind_speed"])
	ratio := float32(o.num("max_column_radius_to_cave_height_ratio"))
	minRadius, minBlunt := o.integer("min_radius_for_wind"), float64(float32(o.num("min_bluntness_for_wind")))
	return func(r *decorRegion, p treePos) bool {
		if !speleothemEmpty(r.get(p[0], p[1], p[2])) {
			return false
		}
		ceiling, hasCeiling := scanSpeleothemEdge(r, p, search, 1)
		floor, hasFloor := scanSpeleothemEdge(r, p, search, -1)
		edge := func(y int32) bool {
			s := r.get(p[0], y, p[2])
			return blocks.isBase(s) || registry.BlockOf(s) == registry.BlockLava
		}
		if !hasCeiling || !hasFloor || !edge(ceiling) || !edge(floor) || ceiling-floor-1 < 4 {
			return false
		}
		maxRadius := min(max(int32(float32(ceiling-floor-1)*ratio), lo), hi)
		radius := lo + r.rng.NextIntN(maxRadius-lo+1)
		down := largeDripstone{treePos{p[0], ceiling - 1, p[2]}, -1, radius, float64(downBlunt(r.rng)), float64(scale(r.rng))}
		up := largeDripstone{treePos{p[0], floor + 1, p[2]}, 1, radius, float64(upBlunt(r.rng)), float64(scale(r.rng))}
		wind := dripstoneWind{}
		if radius >= minRadius && down.bluntness >= minBlunt && up.bluntness >= minBlunt {
			velocity := speed(r.rng)
			angle := float32(r.rng.NextFloat() * float32(math.Pi))
			wind = dripstoneWind{p[1], 16 - radius, float64(levelgen.MthCos(float64(angle)) * velocity), float64(levelgen.MthSin(float64(angle)) * velocity)}
		}
		downOK, upOK := down.embed(r, wind), up.embed(r, wind)
		if downOK {
			down.place(r, wind)
		}
		if upOK {
			up.place(r, wind)
		}
		return true
	}
}

// largeDripstoneRadiusBounds reads bounds without sampling the provider.
func largeDripstoneRadiusBounds(v any) (int32, int32) {
	if _, ok := v.(float64); ok {
		n := jint(v)
		return n, n
	}
	o := jobjOf(v)
	switch jtype(o) {
	case "constant":
		jonly(o, "value")
		n := o.integer("value")
		return n, n
	case "clamped":
		jonly(o, "source", "min_inclusive", "max_inclusive")
		lo, hi := largeDripstoneRadiusBounds(o["source"])
		return max(lo, o.integer("min_inclusive")), min(hi, o.integer("max_inclusive"))
	case "uniform":
		jonly(o, "min_inclusive", "max_inclusive")
		return o.integer("min_inclusive"), o.integer("max_inclusive")
	}
	modelFail("unhandled large dripstone radius provider %q", jtype(o))
	return 0, 0
}

type dripstoneWind struct {
	originY, maxOffset int32
	x, z               float64
}

func (w dripstoneWind) offset(p treePos) treePos {
	dy := float64(w.originY - p[1])
	p[0] += min(max(int32(math.Floor(w.x*dy)), -w.maxOffset), w.maxOffset)
	p[2] += min(max(int32(math.Floor(w.z*dy)), -w.maxOffset), w.maxOffset)
	return p
}

type largeDripstone struct {
	root             treePos
	dy, radius       int32
	bluntness, scale float64
}

func (d largeDripstone) height(distance float32) int32 {
	return int32(speleothemHeight(float64(distance), float64(d.radius), d.scale, d.bluntness))
}

// embed is LargeDripstone.moveBackUntilBaseIsInsideStoneAndShrinkRadiusIfNecessary.
func (d *largeDripstone) embed(r *decorRegion, wind dripstoneWind) bool {
	for d.radius > 1 {
		p := d.root
		tries := min(int32(10), d.height(0))
		for range tries {
			if registry.BlockOf(r.get(p[0], p[1], p[2])) == registry.BlockLava {
				return false
			}
			if speleothemEmbedded(r, wind.offset(p), d.radius) {
				d.root = p
				return true
			}
			p[1] -= d.dy
		}
		d.radius /= 2
	}
	return false
}

// place is LargeDripstone.placeBlocks.
func (d largeDripstone) place(r *decorRegion, wind dripstoneWind) {
	for dx := -d.radius; dx <= d.radius; dx++ {
		for dz := -d.radius; dz <= d.radius; dz++ {
			distance := float32(math.Sqrt(float64(float32(dx*dx + dz*dz))))
			if distance > float32(d.radius) {
				continue
			}
			height := d.height(distance)
			if height <= 0 {
				continue
			}
			if float64(r.rng.NextFloat()) < .2 {
				factor := float32(r.rng.NextFloat()*(float32(1)-float32(.8))) + float32(.8)
				height = int32(float32(height) * factor)
			}
			p := at(d.root, treePos{dx, 0, dz})
			maxY := int32(math.MaxInt32)
			if d.dy > 0 {
				maxY = r.heightOf(hmWorldSurfaceWG, p[0], p[2])
			}
			outOfStone := false
			for i := int32(0); i < height && p[1] < maxY; i++ {
				q := wind.offset(p)
				s := r.get(q[0], q[1], q[2])
				if speleothemEmptyOrLava(s) {
					outOfStone = true
					r.set(q[0], q[1], q[2], registry.BlockDripstoneBlock.DefaultState())
				} else if outOfStone && registry.BlockOf(s).InTag(registry.TagIDBaseStoneOverworld) {
					break
				}
				p[1] += d.dy
			}
		}
	}
}
