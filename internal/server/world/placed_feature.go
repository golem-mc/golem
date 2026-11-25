package world

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// The overworld's placed features without a hand port are interpreted from
// the JSON the vanilla data generator writes (levelgen.PlacedFeatureJSON), so
// their numbers are vanilla's. The generator keeps only features made of the
// types handled here; a type or key this file does not handle panics at
// start-up, never places differently.

// jobj is a JSON object of the data pack. The accessors panic on a missing or
// mistyped key: the files are the generator's output, so a gap is a bug in
// this port, caught the first time the features are built.
type jobj map[string]any

func parseJSON(s string) jobj {
	var o jobj
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		panic("placed feature: " + err.Error())
	}
	return o
}

func (o jobj) has(k string) bool { _, ok := o[k]; return ok }

func (o jobj) num(k string) float64 {
	v, ok := o[k].(float64)
	if !ok {
		panic(fmt.Sprintf("placed feature: %q is not a number in %v", k, o))
	}
	return v
}

func (o jobj) integer(k string) int32 { return int32(o.num(k)) }

// integerOr is an optionalFieldOf integer.
func (o jobj) integerOr(k string, def int32) int32 {
	if !o.has(k) {
		return def
	}
	return o.integer(k)
}

func (o jobj) str(k string) string {
	v, ok := o[k].(string)
	if !ok {
		panic(fmt.Sprintf("placed feature: %q is not a string in %v", k, o))
	}
	return v
}

func (o jobj) flag(k string) bool { v, _ := o[k].(bool); return v }

func (o jobj) obj(k string) jobj {
	v, ok := asJobj(o[k])
	if !ok {
		panic(fmt.Sprintf("placed feature: %q is not an object in %v", k, o))
	}
	return v
}

func (o jobj) arr(k string) []any {
	v, ok := o[k].([]any)
	if !ok {
		panic(fmt.Sprintf("placed feature: %q is not an array in %v", k, o))
	}
	return v
}

// unported marks a feature that uses something this port does not model. The
// tree features' compile skips such a feature; any other caller lets it panic.
type unported string

func modelFail(format string, args ...any) {
	panic(unported("placed feature: " + fmt.Sprintf(format, args...)))
}

func jtype(o jobj) string {
	s, _ := o["type"].(string)
	return strings.TrimPrefix(s, "minecraft:")
}

// asJobj reads a JSON object: a decoded one is a plain map.
func asJobj(v any) (jobj, bool) {
	switch t := v.(type) {
	case jobj:
		return t, true
	case map[string]any:
		return t, true
	}
	return nil, false
}

func jobjOf(v any) jobj {
	o, ok := asJobj(v)
	if !ok {
		modelFail("expected an object, got %v", v)
	}
	return o
}

func jint(v any) int32 {
	f, ok := v.(float64)
	if !ok {
		modelFail("expected a number, got %v", v)
	}
	return int32(f)
}

// jfloat reads a Java float field: the file holds its shortest decimal, which
// rounds back to the same float32.
func jfloat(v any) float64 {
	f, ok := v.(float64)
	if !ok {
		modelFail("expected a number, got %v", v)
	}
	return float64(float32(f))
}

// jstrings reads a holder set: one name or a list of them.
func jstrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, len(t))
		for i, x := range t {
			out[i], _ = x.(string)
		}
		return out
	}
	modelFail("expected names, got %v", v)
	return nil
}

// jonly stops at a key the interpreter does not handle.
func jonly(o jobj, keys ...string) {
	for k := range o {
		if k != "type" && !slices.Contains(keys, k) {
			modelFail("%s: unhandled key %q", jtype(o), k)
		}
	}
}

func jvec(o jobj) [3]int32 { return jvecAt(o, "offset") }

// jvecAt reads the x, y, z list under key; zero when it is absent.
func jvecAt(o jobj, key string) (v [3]int32) {
	if l, ok := o[key].([]any); ok {
		for i := range v {
			v[i] = jint(l[i])
		}
	}
	return v
}

// --- Int providers ---

type intProvider func(rng *levelgen.WorldgenRandom) int32

// compileInt reads an IntProvider: a bare number is a constant.
// vanilla: IntProvider.CODEC
func compileInt(v any) intProvider {
	if _, ok := v.(float64); ok {
		c := jint(v)
		return func(*levelgen.WorldgenRandom) int32 { return c }
	}
	o := jobjOf(v)
	switch jtype(o) {
	case "constant":
		jonly(o, "value")
		c := jint(o["value"])
		return func(*levelgen.WorldgenRandom) int32 { return c }
	case "uniform":
		jonly(o, "min_inclusive", "max_inclusive")
		lo, hi := jint(o["min_inclusive"]), jint(o["max_inclusive"])
		return func(rng *levelgen.WorldgenRandom) int32 { return rng.NextIntN(hi-lo+1) + lo }
	case "clamped_normal":
		jonly(o, "mean", "deviation", "min_inclusive", "max_inclusive")
		mean, deviation := float32(o.num("mean")), float32(o.num("deviation"))
		lo, hi := float32(o.num("min_inclusive")), float32(o.num("max_inclusive"))
		return func(rng *levelgen.WorldgenRandom) int32 { return int32(clampedNormal(rng, mean, deviation, lo, hi)) }
	case "biased_to_bottom":
		jonly(o, "min_inclusive", "max_inclusive")
		lo, hi := jint(o["min_inclusive"]), jint(o["max_inclusive"])
		return func(rng *levelgen.WorldgenRandom) int32 { return lo + rng.NextIntN(rng.NextIntN(hi-lo+1)+1) }
	case "clamped":
		jonly(o, "min_inclusive", "max_inclusive", "source")
		src := compileInt(o["source"])
		lo, hi := jint(o["min_inclusive"]), jint(o["max_inclusive"])
		return func(rng *levelgen.WorldgenRandom) int32 { return min(max(src(rng), lo), hi) }
	case "trapezoid":
		jonly(o, "min", "max", "plateau")
		lo, hi, plateau := jint(o["min"]), jint(o["max"]), jint(o["plateau"])
		return trapezoid(lo, hi, plateau)
	case "weighted_list":
		jonly(o, "distribution")
		var provs []intProvider
		var weights []int32
		for _, e := range o["distribution"].([]any) {
			e := jobjOf(e)
			provs = append(provs, compileInt(e["data"]))
			weights = append(weights, jint(e["weight"]))
		}
		return func(rng *levelgen.WorldgenRandom) int32 { return provs[pickWeighted(rng, weights)](rng) }
	}
	modelFail("unhandled int provider %q", jtype(o))
	return nil
}

type floatProvider func(rng *levelgen.WorldgenRandom) float32

// compileFloat reads the FloatProviders used by speleothem clusters.
func compileFloat(v any) floatProvider {
	if f, ok := v.(float64); ok {
		return func(*levelgen.WorldgenRandom) float32 { return float32(f) }
	}
	o := jobjOf(v)
	switch jtype(o) {
	case "uniform":
		jonly(o, "min_inclusive", "max_exclusive")
		lo, hi := float32(o.num("min_inclusive")), float32(o.num("max_exclusive"))
		return func(rng *levelgen.WorldgenRandom) float32 { return float32(rng.NextFloat()*(hi-lo)) + lo }
	case "clamped_normal":
		jonly(o, "mean", "deviation", "min", "max")
		mean, deviation := float32(o.num("mean")), float32(o.num("deviation"))
		lo, hi := float32(o.num("min")), float32(o.num("max"))
		return func(rng *levelgen.WorldgenRandom) float32 { return clampedNormal(rng, mean, deviation, lo, hi) }
	default:
		modelFail("unhandled float provider %q", jtype(o))
		return nil
	}
}

// clampedNormal is ClampedNormalFloat.sample; Mth.normal rounds the Gaussian
// and the product to float before adding the mean.
func clampedNormal(rng *levelgen.WorldgenRandom, mean, deviation, lo, hi float32) float32 {
	return min(max(mean+float32(float32(rng.NextGaussian())*deviation), lo), hi)
}

// trapezoid is TrapezoidInt.sample.
func trapezoid(lo, hi, plateau int32) intProvider {
	between := func(rng *levelgen.WorldgenRandom, a, b int32) int32 { return rng.NextIntN(b-a+1) + a }
	return func(rng *levelgen.WorldgenRandom) int32 {
		if plateau == 0 && hi == -lo {
			return diff(rng, hi+1)
		}
		span := hi - lo
		if plateau == span {
			return between(rng, lo, hi)
		}
		start := (span - plateau) / 2
		end := span - start
		return lo + between(rng, 0, end) + between(rng, 0, start)
	}
}

// pickWeighted draws an index by weight with one nextInt of the total.
// vanilla: WeightedList.getRandomOrThrow
func pickWeighted(rng *levelgen.WorldgenRandom, weights []int32) int {
	var total int32
	for _, w := range weights {
		total += w
	}
	sel := rng.NextIntN(total)
	for i, w := range weights {
		sel -= w
		if sel < 0 {
			return i
		}
	}
	return len(weights) - 1
}

// --- Block states and providers ---

func compileBlockState(v any) uint16 {
	if name, ok := v.(string); ok {
		b, found := registry.BlockByName(name)
		if !found {
			modelFail("unknown block %q", name)
		}
		return b.DefaultState()
	}
	o := jobjOf(v)
	name, _ := o["Name"].(string)
	if o.has("id") {
		name = o.str("id")
	}
	var props map[string]string
	properties := o["Properties"]
	if o.has("properties") {
		properties = o["properties"]
	}
	if p, ok := asJobj(properties); ok {
		props = make(map[string]string, len(p))
		for k, x := range p {
			props[k], _ = x.(string)
		}
	}
	s, ok := registry.FindState(name, props)
	if !ok {
		modelFail("unknown block %q", name)
	}
	return s
}

type stateProvider func(r *decorRegion, p [3]int32) uint16

func compileStates(v any) []uint16 {
	l := v.([]any)
	out := make([]uint16, len(l))
	for i, x := range l {
		out[i] = compileBlockState(x)
	}
	return out
}

func compileNoise(o jobj, key string) *terrain.NoiseStack {
	n := jobjOf(o[key])
	if n.has("base_octave") {
		return compileNativeNoise(n).Create(levelgen.NewLegacyRandom(int64(o.num("seed"))))
	}
	var amps []float64
	for _, a := range n["amplitudes"].([]any) {
		amps = append(amps, a.(float64))
	}
	seed := int64(o["seed"].(float64))
	return terrain.NewParityNormalNoise(int(jint(n["firstOctave"])), amps...).Create(levelgen.NewLegacyRandom(seed))
}

// pickByNoise is NoiseProvider.getRandomState.
func pickByNoise(states []uint16, noise float32) uint16 {
	v := min(max((1+noise)/2, 0), 0.9999)
	return states[int(v*float32(len(states)))]
}

// compileState reads a BlockStateProvider.
// vanilla: BlockStateProvider.CODEC
func compileState(v any) stateProvider { return providerCompiler{}.state(v) }

func (c providerCompiler) state(v any) stateProvider {
	v = c.resolve(v)
	o := jobjOf(v)
	if jtype(o) == "" {
		s := compileBlockState(v)
		return func(*decorRegion, [3]int32) uint16 { return s }
	}
	switch jtype(o) {
	case "simple":
		jonly(o, "state")
		s := compileBlockState(o["state"])
		return func(*decorRegion, [3]int32) uint16 { return s }
	case "rotated":
		return c.rotated(o)
	case "copy_properties":
		jonly(o, "source")
		source := c.state(o["source"])
		return func(r *decorRegion, p [3]int32) uint16 {
			state, ok := registry.WithStatePropertiesOf(source(r, p), r.get(p[0], p[1], p[2]))
			if !ok {
				modelFail("incompatible copied properties")
			}
			return state
		}
	case "random_block":
		opt := c.randomBlock(o)
		return stateOrExisting(opt)
	case "weighted":
		jonly(o, "entries")
		var states []uint16
		var weights []int32
		for _, e := range o["entries"].([]any) {
			e := jobjOf(e)
			states = append(states, compileBlockState(e["data"]))
			weights = append(weights, jint(e["weight"]))
		}
		return func(r *decorRegion, _ [3]int32) uint16 { return states[pickWeighted(r.rng, weights)] }
	case "noise":
		jonly(o, "seed", "noise", "scale", "states")
		noise, scale, states := compileNoise(o, "noise"), jfloat(o["scale"]), compileStates(o["states"])
		return func(_ *decorRegion, p [3]int32) uint16 {
			return pickByNoise(states, noise.Get3D(float64(p[0])*scale, float64(p[1])*scale, float64(p[2])*scale))
		}
	case "noise_threshold":
		jonly(o, "seed", "noise", "scale", "threshold", "high_chance", "default_state", "low_states", "high_states")
		noise, scale, threshold := compileNoise(o, "noise"), jfloat(o["scale"]), jfloat(o["threshold"])
		chance, deflt := float32(jfloat(o["high_chance"])), compileBlockState(o["default_state"])
		low, high := compileStates(o["low_states"]), compileStates(o["high_states"])
		return func(r *decorRegion, p [3]int32) uint16 {
			if float64(noise.Get3D(float64(p[0])*scale, float64(p[1])*scale, float64(p[2])*scale)) < threshold {
				return low[r.rng.NextIntN(int32(len(low)))]
			}
			if r.rng.NextFloat() < chance {
				return high[r.rng.NextIntN(int32(len(high)))]
			}
			return deflt
		}
	case "dual_noise":
		jonly(o, "seed", "noise", "scale", "states", "slow_noise", "slow_scale", "variety")
		noise, scale, states := compileNoise(o, "noise"), jfloat(o["scale"]), compileStates(o["states"])
		slow, slowScale := compileNoise(o, "slow_noise"), float32(jfloat(o["slow_scale"]))
		vl := o["variety"].([]any)
		vlo, vhi := jint(vl[0]), jint(vl[1])
		slowAt := func(x, y, z int32) float32 {
			return slow.Get3D(float64(float32(x)*slowScale), float64(float32(y)*slowScale), float64(float32(z)*slowScale))
		}
		return func(_ *decorRegion, p [3]int32) uint16 {
			// vanilla: Mth.clampedMap
			t := min(max((float64(slowAt(p[0], p[1], p[2]))+1)/2, 0), 1)
			lo, hi := float64(vlo), float64(vhi+1)
			n := int(lo + t*(hi-lo))
			picks := make([]uint16, n)
			for i := range picks {
				picks[i] = pickByNoise(states, slowAt(p[0]+int32(i)*54545, p[1], p[2]+int32(i)*34234))
			}
			return pickByNoise(picks, noise.Get3D(float64(p[0])*scale, float64(p[1])*scale, float64(p[2])*scale))
		}
	case "randomized_int":
		jonly(o, "source", "property", "values")
		src, values := c.state(o["source"]), compileInt(o["values"])
		prop, ok := registry.PropNamed(o["property"].(string))
		if !ok {
			return src
		}
		return func(r *decorRegion, p [3]int32) uint16 {
			s := src(r, p)
			if _, err := strconv.Atoi(registry.StateValue(s, prop)); err != nil {
				return s
			}
			next, ok := registry.WithInt(s, prop, int(values(r.rng)))
			if !ok {
				modelFail("invalid randomized property value")
			}
			return next
		}
	case "rule_based":
		return stateOrExisting(c.rules(o))
	}
	modelFail("unhandled state provider %q", jtype(o))
	return nil
}

// optState is BlockStateProvider.getOptionalState: false for a rule-based
// provider none of whose rules matched.
type optState func(r *decorRegion, p [3]int32) (uint16, bool)

// compileOptState reads a BlockStateProvider by its optional state.
func compileOptState(v any) optState { return providerCompiler{}.optional(v) }

func (c providerCompiler) optional(v any) optState {
	v = c.resolve(v)
	o := jobjOf(v)
	switch jtype(o) {
	case "rule_based":
		return c.rules(o)
	case "random_block":
		return c.randomBlock(o)
	}
	sp := c.state(v)
	return func(r *decorRegion, p [3]int32) (uint16, bool) { return sp(r, p), true }
}

// rules reads RuleBasedStateProvider: the first matching child with a state, else the fallback.
func (c providerCompiler) rules(o jobj) optState {
	jonly(o, "rules", "fallback")
	type rule struct {
		ifTrue blockPred
		then   optState
	}
	var rules []rule
	for _, e := range o.arr("rules") {
		e := jobjOf(e)
		rules = append(rules, rule{compilePred(e["if_true"]), c.optional(e["then"])})
	}
	var fallback optState
	if o.has("fallback") {
		fallback = c.optional(o["fallback"])
	}
	return func(r *decorRegion, p [3]int32) (uint16, bool) {
		for _, ru := range rules {
			if ru.ifTrue(r, p) {
				if state, ok := ru.then(r, p); ok {
					return state, true
				}
			}
		}
		if fallback != nil {
			return fallback(r, p)
		}
		return 0, false
	}
}

// --- Block predicates ---

type blockPred func(r *decorRegion, p [3]int32) bool

func compilePred(v any) blockPred {
	o := jobjOf(v)
	// at tests the state at the predicate's offset.
	at := func(test func(uint16) bool) blockPred {
		off := jvec(o)
		return func(r *decorRegion, p [3]int32) bool {
			return test(r.get(p[0]+off[0], p[1]+off[1], p[2]+off[2]))
		}
	}
	switch jtype(o) {
	case "true":
		jonly(o)
		return func(*decorRegion, [3]int32) bool { return true }
	case "inside_world_bounds":
		jonly(o, "offset")
		off := jvec(o)
		return func(r *decorRegion, p [3]int32) bool { return r.inBuildHeight(p[1] + off[1]) }
	case "matching_block_tag":
		jonly(o, "tag", "offset")
		tag := registry.BlockTagOf(o["tag"].(string))
		return at(func(s uint16) bool { return registry.BlockOf(s).InTag(tag) })
	case "matching_blocks":
		jonly(o, "blocks", "offset")
		set := blocksNamed(jstrings(o["blocks"])...)
		return at(func(s uint16) bool { return set[registry.BlockOf(s)] })
	case "matching_fluids":
		jonly(o, "fluids", "offset")
		names := jstrings(o["fluids"])
		return at(func(s uint16) bool { return slices.Contains(names, fluidName(s)) })
	case "solid":
		jonly(o, "offset")
		return at(registry.IsSolid)
	case "replaceable":
		jonly(o, "offset")
		return at(func(s uint16) bool { return registry.BlockOf(s).IsReplaceable() })
	case "has_sturdy_face":
		jonly(o, "offset", "direction")
		d, off := dirIndex(o["direction"].(string)), jvec(o)
		return func(r *decorRegion, p [3]int32) bool {
			return registry.FaceSturdy(r.get(p[0]+off[0], p[1]+off[1], p[2]+off[2]), d, registry.SupportFull)
		}
	case "would_survive":
		jonly(o, "offset", "state")
		st, off := compileBlockState(o["state"]), jvec(o)
		return func(r *decorRegion, p [3]int32) bool {
			return placementSurvives(r, st, p[0]+off[0], p[1]+off[1], p[2]+off[2])
		}
	case "volume_match":
		// vanilla: VolumeMatchPredicate.test; min and max are inclusive offsets.
		jonly(o, "min", "max", "match")
		lo, hi, match := jvecAt(o, "min"), jvecAt(o, "max"), compilePred(o["match"])
		return func(r *decorRegion, p [3]int32) bool {
			for x := lo[0]; x <= hi[0]; x++ {
				for z := lo[2]; z <= hi[2]; z++ {
					for y := lo[1]; y <= hi[1]; y++ {
						if !match(r, [3]int32{p[0] + x, p[1] + y, p[2] + z}) {
							return false
						}
					}
				}
			}
			return true
		}
	case "not":
		jonly(o, "predicate")
		inner := compilePred(o["predicate"])
		return func(r *decorRegion, p [3]int32) bool { return !inner(r, p) }
	case "all_of", "any_of":
		jonly(o, "predicates")
		var list []blockPred
		for _, x := range o["predicates"].([]any) {
			list = append(list, compilePred(x))
		}
		all := jtype(o) == "all_of"
		return func(r *decorRegion, p [3]int32) bool {
			for _, f := range list {
				if f(r, p) != all {
					return !all
				}
			}
			return all
		}
	}
	modelFail("unhandled block predicate %q", jtype(o))
	return nil
}

// fluidName is the registry name of the fluid a state holds.
func fluidName(s uint16) string {
	fs := fluidHolding(s)
	switch {
	case fs.kind == fluidNone:
		return "minecraft:empty"
	case fs.kind == fluidWater && fs.isSource():
		return "minecraft:water"
	case fs.kind == fluidWater:
		return "minecraft:flowing_water"
	case fs.isSource():
		return "minecraft:lava"
	}
	return "minecraft:flowing_lava"
}

// --- Heightmaps ---

type heightmapKind uint8

const (
	hmWorldSurfaceWG heightmapKind = iota
	hmOceanFloorWG
	hmWorldSurface
	hmOceanFloor
	hmMotionBlocking
	hmMotionBlockingNoLeaves
)

var heightmapKinds = map[string]heightmapKind{
	"WORLD_SURFACE_WG": hmWorldSurfaceWG, "OCEAN_FLOOR_WG": hmOceanFloorWG,
	"WORLD_SURFACE": hmWorldSurface, "OCEAN_FLOOR": hmOceanFloor,
	"MOTION_BLOCKING": hmMotionBlocking, "MOTION_BLOCKING_NO_LEAVES": hmMotionBlockingNoLeaves,
}

// oceanFloorByState is Heightmap.Types.OCEAN_FLOOR's blocksMotion read.
var oceanFloorByState = func() (m [65536]bool) {
	for s := range m {
		m[s] = heightmapSolid(uint16(s))
	}
	return m
}()

// heightOf is WorldGenRegion.getHeight: one above the highest block the heightmap
// counts, the dimension's minimum for an empty column.
// vanilla: ChunkStatus.heightmapsAfter, ProtoChunk.setBlockState
func (r *decorRegion) heightOf(kind heightmapKind, x, z int32) int32 {
	switch kind {
	case hmWorldSurfaceWG, hmOceanFloorWG:
		return r.wgHeight(kind, x, z)
	case hmWorldSurface:
		return r.scanDown(x, z, func(s uint16) bool { return !r.src.isAir(s) })
	case hmOceanFloor:
		return r.scanDown(x, z, func(s uint16) bool { return oceanFloorByState[s] })
	case hmMotionBlocking:
		return r.scanDown(x, z, func(s uint16) bool { return motionBlockingByState[s] })
	default:
		return r.scanDown(x, z, func(s uint16) bool { return noLeavesBlockingByState[s] })
	}
}

// --- Placement modifiers ---

// compileMods reads a placement list. A biome filter is the feature's own
// biome set; a placed feature inside a selector has none.
func compileMods(list []any, biomes uint64, top bool) []placementMod {
	mods := make([]placementMod, len(list))
	for i, x := range list {
		mods[i] = compileMod(jobjOf(x), biomes, top)
	}
	return mods
}

// compileAnchor reads a VerticalAnchor once; bounds remain dimension-dependent.
// vanilla: VerticalAnchor.resolveY
func compileAnchor(v any) func(minY, height int32) int32 {
	o := jobjOf(v)
	for k, x := range o {
		value := jint(x)
		switch k {
		case "absolute":
			return func(_, _ int32) int32 { return value }
		case "above_bottom":
			return func(minY, _ int32) int32 { return minY + value }
		case "below_top":
			return func(minY, height int32) int32 { return minY + height - 1 - value }
		}
	}
	modelFail("unhandled vertical anchor %v", o)
	return nil
}

func compileMod(o jobj, biomes uint64, top bool) placementMod {
	switch jtype(o) {
	case "count":
		jonly(o, "count")
		n := compileInt(o["count"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			for range n(r.rng) {
				next.call(p)
			}
		}
	case "in_square":
		jonly(o)
		return inSquareMod
	case "biome":
		jonly(o)
		if !top {
			modelFail("biome filter in a nested placed feature")
		}
		if biomes == 0 { // a feature compiled without a biome set places everywhere
			return func(_ *decorRegion, p [3]int32, next walkNext) { next.call(p) }
		}
		return biomeMod(biomes)
	case "surface_water_depth_filter":
		jonly(o, "max_water_depth")
		maxDepth := jint(o["max_water_depth"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			if r.heightOf(hmWorldSurface, p[0], p[2])-r.heightOf(hmOceanFloor, p[0], p[2]) <= maxDepth {
				next.call(p)
			}
		}
	case "rarity_filter":
		jonly(o, "chance")
		return rarityMod(jint(o["chance"]))
	case "random_chance":
		jonly(o, "chance")
		chance := float32(jfloat(o["chance"]))
		// vanilla: RandomChancePlacement.shouldPlace
		return func(r *decorRegion, p [3]int32, next walkNext) {
			if r.rng.NextFloat() < chance {
				next.call(p)
			}
		}
	case "heightmap":
		jonly(o, "heightmap")
		kind, ok := heightmapKinds[o["heightmap"].(string)]
		if !ok {
			modelFail("unknown heightmap %v", o["heightmap"])
		}
		return func(r *decorRegion, p [3]int32, next walkNext) {
			if y := r.heightOf(kind, p[0], p[2]); y > r.minY {
				next.call([3]int32{p[0], y, p[2]})
			}
		}
	case "surface_relative_threshold_filter":
		jonly(o, "heightmap", "min_inclusive", "max_inclusive")
		kind, ok := heightmapKinds[o.str("heightmap")]
		if !ok {
			modelFail("unknown heightmap %v", o["heightmap"])
		}
		lo, hi := o.integerOr("min_inclusive", math.MinInt32), o.integerOr("max_inclusive", math.MaxInt32)
		// The bounds are offsets from the surface, summed in int64 so the
		// unbounded defaults do not wrap.
		// vanilla: SurfaceRelativeThresholdFilter
		return func(r *decorRegion, p [3]int32, next walkNext) {
			h, y := int64(r.heightOf(kind, p[0], p[2])), int64(p[1])
			if h+int64(lo) <= y && y <= h+int64(hi) {
				next.call(p)
			}
		}
	case "height_range":
		jonly(o, "height")
		h := jobjOf(o["height"])
		jonly(h, "min_inclusive", "max_inclusive")
		kind := jtype(h)
		if kind != "uniform" && kind != "trapezoid" {
			modelFail("unhandled height provider %q", kind)
		}
		low, high := compileAnchor(h["min_inclusive"]), compileAnchor(h["max_inclusive"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			minY, height := r.minY, r.height
			// vanilla: WorldGenerationContext; flat generation starts at 0
			// even when the dimension and its terrain layers start below it.
			if g, ok := r.src.(*OverworldGenerator); ok {
				minY = max(minY, g.minY())
			}
			if r.g != nil {
				height = min(height, netherTop)
			}
			lo, hi := low(minY, height), high(minY, height)
			mod := heightUniformMod(lo, hi)
			if kind == "trapezoid" {
				mod = heightTriangleMod(lo, hi)
			}
			mod(r, p, next)
		}
	case "count_on_every_layer":
		jonly(o, "count")
		return countOnEveryLayerMod(o.integer("count"))
	case "offset":
		jonly(o, "x", "y", "z")
		x, y, z := compileInt(o["x"]), compileInt(o["y"]), compileInt(o["z"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			next.call([3]int32{p[0] + x(r.rng), p[1] + y(r.rng), p[2] + z(r.rng)})
		}
	case "cuboid":
		jonly(o, "xz_size", "y_size", "include_edges", "include_interior")
		xz, ySize := compileInt(o["xz_size"]), compileInt(o["y_size"])
		edges, interior := !o.has("include_edges") || o.flag("include_edges"), !o.has("include_interior") || o.flag("include_interior")
		// vanilla: CuboidPlacement.modify
		return func(r *decorRegion, p [3]int32, next walkNext) {
			height := ySize(r.rng)
			width := xz(r.rng)
			length := xz(r.rng)
			for x := int32(0); x <= width; x++ {
				for y := int32(0); y <= height; y++ {
					for z := int32(0); z <= length; z++ {
						xe, ye, ze := x == 0 || x == width, y == 0 || y == height, z == 0 || z == length
						if edges || (!xe || !ye) && (!ze || !ye) && (!xe || !ze) {
							if interior || xe || ye || ze {
								next.call([3]int32{p[0] + x, p[1] + y, p[2] + z})
							}
						}
					}
				}
			}
		}
	case "random_offset":
		jonly(o, "xz_spread", "y_spread")
		xz, y := compileInt(o["xz_spread"]), compileInt(o["y_spread"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			x := p[0] + xz(r.rng)
			yy := p[1] + y(r.rng)
			next.call([3]int32{x, yy, p[2] + xz(r.rng)})
		}
	case "block_predicate_filter":
		jonly(o, "predicate")
		pred := compilePred(o["predicate"])
		return func(r *decorRegion, p [3]int32, next walkNext) {
			if pred(r, p) {
				next.call(p)
			}
		}
	case "noise_threshold_count":
		jonly(o, "noise_level", "below_noise", "above_noise")
		level, below, above := o["noise_level"].(float64), jint(o["below_noise"]), jint(o["above_noise"])
		return func(_ *decorRegion, p [3]int32, next walkNext) {
			n := above
			if float64(biomeInfoNoise.Get2D(float64(p[0])/200, float64(p[2])/200)) < level {
				n = below
			}
			for range n {
				next.call(p)
			}
		}
	case "noise_based_count":
		jonly(o, "noise_to_count_ratio", "noise_factor", "noise_offset")
		ratio, factor := jint(o["noise_to_count_ratio"]), o["noise_factor"].(float64)
		offset, _ := o["noise_offset"].(float64)
		return func(_ *decorRegion, p [3]int32, next walkNext) {
			n := int32(math.Ceil((float64(biomeInfoNoise.Get2D(float64(p[0])/factor, float64(p[2])/factor)) + offset) * float64(ratio)))
			for range n {
				next.call(p)
			}
		}
	case "environment_scan":
		return compileScan(o)
	case "randomly_selected":
		// vanilla: RandomlySelectedPlacement.modify, Util.getRandom
		jonly(o, "placements")
		mods := compileMods(o.arr("placements"), biomes, top)
		if len(mods) == 0 {
			modelFail("randomly_selected with no placements")
		}
		return func(r *decorRegion, p [3]int32, next walkNext) {
			mods[r.rng.NextIntN(int32(len(mods)))](r, p, next)
		}
	}
	modelFail("unhandled placement modifier %q", jtype(o))
	return nil
}

// compileScan is EnvironmentScanPlacement.
func compileScan(o jobj) placementMod {
	jonly(o, "direction_of_search", "target_condition", "allowed_search_condition", "max_steps")
	dir := dirDelta[dirIndex(o["direction_of_search"].(string))][1]
	target, steps := compilePred(o["target_condition"]), int(jint(o["max_steps"]))
	allowed := blockPred(func(*decorRegion, [3]int32) bool { return true })
	if a, ok := o["allowed_search_condition"]; ok {
		allowed = compilePred(a)
	}
	return func(r *decorRegion, p [3]int32, next walkNext) {
		if !allowed(r, p) {
			return
		}
		for range steps {
			if target(r, p) {
				next.call(p)
				return
			}
			p[1] += dir
			if p[1] < r.minY || p[1] >= r.minY+r.height {
				return
			}
			if !allowed(r, p) {
				break
			}
		}
		if target(r, p) {
			next.call(p)
		}
	}
}

// --- Placed features ---

// compilePlaced reads a placed feature: its placement list and the feature
// they feed. The returned function reports whether anything was placed.
// vanilla: PlacedFeature.placeWithContext
func compilePlaced(doc jobj, biomes uint64, top bool) ([]placementMod, featureFn) {
	if name, ok := doc["feature"].(string); ok { // a tree-table feature, by name
		return compileMods(doc["placement"].([]any), biomes, top), compileConfigured(name)
	}
	return compileMods(doc["placement"].([]any), biomes, top), compileFeature(jobjOf(doc["feature"]))
}

// nestedPlaced is a placed feature a selector holds, run on a position of the
// selector's own.
// vanilla: PlacedFeature.place
func nestedPlaced(doc jobj) featureFn {
	mods, feature := compilePlaced(doc, 0, false)
	return func(r *decorRegion, p [3]int32) bool {
		placed := false
		r.walk(mods, p, func(r *decorRegion, q [3]int32) {
			if feature(r, q) {
				placed = true
			}
		})
		return placed
	}
}

// placedRef is a placed feature a selector holds: a name in the tree table or
// an inline one.
func placedRef(v any) featureFn {
	if name, ok := v.(string); ok {
		f := treePlaced(name, 0)
		return func(r *decorRegion, p [3]int32) bool { return f.run(r, p) }
	}
	return nestedPlaced(jobjOf(v))
}

// compileJSONPlaced builds the placed feature of an overworld feature slot from
// the data generator's JSON; false for a feature the file does not hold.
func compileJSONPlaced(f terrain.OverworldFeature) (overworldPlaced, bool) {
	data := levelgen.PlacedFeatureJSON(f.Name)
	if data == "" {
		return overworldPlaced{}, false
	}
	var doc jobj
	if err := json.Unmarshal([]byte(data), &doc); err != nil {
		modelFail("%s: %v", f.Name, err)
	}
	mods, feature := compilePlaced(doc, f.Biomes, true)
	return overworldPlaced{
		feature: f,
		mods:    mods,
		place:   func(r *decorRegion, p [3]int32) { feature(r, p) },
	}, true
}
