package main

import (
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// blockClass is the coarse kind of a block that the terrain-shape stage
// decides before surface rules and features dress it.
type blockClass uint8

const (
	classAir blockClass = iota
	classFluid
	classSolid
)

// classOf sorts a state into air, fluid or solid. Waterlogged blocks and the
// always-submerged plants count as fluid, since the shape stage put water
// there and a feature only decorated it.
func classOf(state uint16) blockClass {
	switch blockName(state) {
	case "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
		return classAir
	case "minecraft:water", "minecraft:lava", "minecraft:bubble_column",
		"minecraft:kelp", "minecraft:kelp_plant", "minecraft:seagrass", "minecraft:tall_seagrass":
		return classFluid
	}
	if state != unknownState && registry.StateProps(state)["waterlogged"] == "true" {
		return classFluid
	}
	return classSolid
}

// classCache memoizes classOf over every state ID; the comparison calls it
// for every block of every chunk on both sides.
var classCache = func() []blockClass {
	out := make([]blockClass, 1<<16)
	for i := range out {
		out[i] = classOf(uint16(i))
	}
	return out
}()

// ratio is a matched-of-total count.
type ratio struct {
	Match int `json:"match"`
	Total int `json:"total"`
}

func (r *ratio) add(ok bool) {
	r.Total++
	if ok {
		r.Match++
	}
}

func (r ratio) merge(o ratio) ratio { return ratio{r.Match + o.Match, r.Total + o.Total} }

// Percent is the share matched, 100 for an empty count.
func (r ratio) Percent() float64 {
	if r.Total == 0 {
		return 100
	}
	return 100 * float64(r.Match) / float64(r.Total)
}

func (r ratio) String() string { return fmt.Sprintf("%6.2f%%", r.Percent()) }

// chunkResult is the comparison of one chunk.
type chunkResult struct {
	DifferingBlocks   int         `json:"differing_blocks"`
	BubbleColumnWater int         `json:"bubble_column_water"`
	FluidTickResidue  int         `json:"fluid_tick_residue"`
	PostProcessAge    int         `json:"post_process_age"`
	Pairs             []confusion `json:"pairs"`
	X                 int32       `json:"x"`
	Z                 int32       `json:"z"`
	Exact             ratio       `json:"exact"`
	// NonAir is the exact match over the blocks that are not air on at least
	// one side, which the empty sky does not inflate.
	NonAir ratio `json:"non_air"`
	Shape  ratio `json:"shape"`
	Biome  ratio `json:"biome"`
	// Surface counts columns whose highest non-air block is at the same Y,
	// and SurfaceDiff sums the absolute Y differences.
	Surface     ratio `json:"surface"`
	SurfaceDiff int   `json:"surface_diff"`
	// TerrainTop is Surface over the terrain blocks alone, so that trees,
	// plants, snow layers, ice and fluid do not move it.
	TerrainTop     ratio `json:"terrain_top"`
	TerrainTopDiff int   `json:"terrain_top_diff"`
}

// confusion is a vanilla block that our generator put something else in
// place of, counted over the whole range.
type confusion struct {
	Vanilla string `json:"vanilla"`
	Ours    string `json:"ours"`
	Count   int    `json:"count"`
}

// firstDifference locates a chunk's residuals in the recorded FEATURES order.
type firstDifference struct {
	X                 int32       `json:"x"`
	Z                 int32       `json:"z"`
	OrderIndex        int         `json:"order_index"`
	DifferingBlocks   int         `json:"differing_blocks"`
	BubbleColumnWater int         `json:"bubble_column_water"`
	FluidTickResidue  int         `json:"fluid_tick_residue"`
	PostProcessAge    int         `json:"post_process_age"`
	Pairs             []confusion `json:"pairs"`
}

// report is the whole comparison.
type report struct {
	FirstDiffering []firstDifference `json:"first_differing"`
	Seed           int64             `json:"seed"`
	Dimension      string            `json:"dimension"`
	Range          chunkRange        `json:"range"`
	Exact          ratio             `json:"exact"`
	NonAir         ratio             `json:"non_air"`
	Shape          ratio             `json:"shape"`
	Biome          ratio             `json:"biome"`
	Surface        ratio             `json:"surface"`
	SurfaceDiff    float64           `json:"surface_mean_abs_diff"`
	TerrainTop     ratio             `json:"terrain_top"`
	// TerrainTopDiff is the mean absolute difference of the terrain top.
	TerrainTopDiff float64       `json:"terrain_top_mean_abs_diff"`
	SectionY       []sectionStat `json:"sections"`
	Confusions     []confusion   `json:"confusions"`
	Chunks         []chunkResult `json:"chunks"`
	// PostProcessAge counts growing-plant heads that differ only in age, which
	// vanilla draws from the level random when post-processing makes the head.
	PostProcessAge int               `json:"post_process_age"`
	Stages         *stageReport      `json:"stages_report"`
	BlockEntities  blockEntityReport `json:"block_entities"`
	Entities       entityReport      `json:"entities"`
}

// blockEntityReport compares the saved block entities of the range.
type blockEntityReport struct {
	Vanilla int `json:"vanilla"`
	Ours    int `json:"ours"`
	// SameID counts positions both sides have with the same id, Identical
	// those whose compounds are equal bar position and keepPacked.
	SameID    int `json:"same_id"`
	Identical int `json:"identical"`
	// Pairs counts, per id and top-level key, the positions where that key
	// differs; "<only vanilla>" and "<only ours>" stand for a missing entity.
	Pairs []blockEntityPair `json:"pairs"`
}

// entityReport compares the saved entities of the range, paired per chunk by
// id and nearest position (pairEntities).
type entityReport struct {
	// Recorded is false for a vanilla cache entry without its entities.
	Recorded bool `json:"recorded"`
	Vanilla  int  `json:"vanilla"`
	Ours     int  `json:"ours"`
	Paired   int  `json:"paired"`
	// Identical counts pairs with no differing key.
	Identical int `json:"identical"`
	// Pairs counts, per id and top-level key, the pairs where that key
	// differs; "<only vanilla>" and "<only ours>" stand for an unpaired entity.
	Pairs []blockEntityPair `json:"pairs"`
	// FluidDrops counts vanilla's item entities, block drops from water
	// flowing while vanilla records; they are neither paired nor in Vanilla.
	FluidDrops int `json:"fluid_drops"`
}

type blockEntityPair struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// sectionStat is the exact and shape match of one section height over the
// whole range.
type sectionStat struct {
	Y     int   `json:"y"`
	Exact ratio `json:"exact"`
	Shape ratio `json:"shape"`
}

// compare compares every chunk of the range present on both sides. A chunk
// vanilla lacks is an error, since the reference run was asked for all of
// them.
func compare(d dimension, seed int64, r chunkRange, vanilla, ours map[[2]int32]*chunkData, diff io.Writer) (*report, error) {
	rep := &report{Seed: seed, Dimension: d.Name, Range: r, SectionY: make([]sectionStat, d.sections())}
	for i := range rep.SectionY {
		rep.SectionY[i].Y = d.MinY/16 + i
	}
	ticks := map[[3]int32]bool{}
	for _, c := range ours {
		for pos, scheduled := range c.Ticks {
			if scheduled {
				ticks[pos] = true
			}
		}
	}
	pairs := map[[2]uint16]int{}
	bePairs := map[[2]string]int{}
	entityPairs := map[[2]string]int{}
	rep.Entities.Recorded = len(vanilla) > 0
	for _, c := range vanilla {
		rep.Entities.Recorded = rep.Entities.Recorded && c.EntitiesRecorded
	}
	surfaceDiff, terrainDiff := 0, 0
	for cz := r.Z0; cz <= r.Z1; cz++ {
		for cx := r.X0; cx <= r.X1; cx++ {
			key := [2]int32{cx, cz}
			v, o := vanilla[key], ours[key]
			if v == nil || o == nil {
				return nil, fmt.Errorf("chunk %d,%d is missing (vanilla %t, ours %t)", cx, cz, v != nil, o != nil)
			}
			res, err := compareChunk(v, o, ticks, rep.SectionY, pairs, diff)
			if err != nil {
				return nil, err
			}
			if err := rep.BlockEntities.addChunk(v, o, bePairs, diff); err != nil {
				return nil, err
			}
			if rep.Entities.Recorded {
				if err := rep.Entities.addChunk(v.Entities, o.Entities, entityPairs, diff); err != nil {
					return nil, err
				}
			}
			rep.Chunks = append(rep.Chunks, res)
			rep.PostProcessAge += res.PostProcessAge
			rep.Exact = rep.Exact.merge(res.Exact)
			rep.NonAir = rep.NonAir.merge(res.NonAir)
			rep.Shape = rep.Shape.merge(res.Shape)
			rep.Biome = rep.Biome.merge(res.Biome)
			rep.Surface = rep.Surface.merge(res.Surface)
			rep.TerrainTop = rep.TerrainTop.merge(res.TerrainTop)
			surfaceDiff += res.SurfaceDiff
			terrainDiff += res.TerrainTopDiff
		}
	}
	rep.Stages = stageReportOf(d, vanilla, ours)
	if rep.Surface.Total > 0 {
		rep.SurfaceDiff = float64(surfaceDiff) / float64(rep.Surface.Total)
		rep.TerrainTopDiff = float64(terrainDiff) / float64(rep.TerrainTop.Total)
	}

	rep.Confusions = sortedConfusions(pairs)
	rep.BlockEntities.Pairs = sortedBlockEntityPairs(bePairs)
	rep.Entities.Pairs = sortedBlockEntityPairs(entityPairs)
	return rep, nil
}

// addChunk compares the entities of one chunk, tallying each differing
// (id, key) in pairs and writing it to diff as "entity x y z id key".
func (rep *entityReport) addChunk(v, o []nbt.Compound, pairs map[[2]string]int, diff io.Writer) error {
	kept := make([]nbt.Compound, 0, len(v))
	for _, e := range v {
		if id, _ := nbt.Get[nbt.String](e, "id"); id == "minecraft:item" {
			rep.FluidDrops++
		} else {
			kept = append(kept, e)
		}
	}
	v = kept
	rep.Vanilla += len(v)
	rep.Ours += len(o)
	v, o = sortedEntities(v), sortedEntities(o)
	matched := pairEntities(v, o)
	tally := func(e nbt.Compound, key string) error {
		id, _ := nbt.Get[nbt.String](e, "id")
		pairs[[2]string{string(id), key}]++
		if diff == nil {
			return nil
		}
		pos, _ := entityPos(e)
		if _, err := fmt.Fprintf(diff, "entity %g %g %g %s %s\n", pos[0], pos[1], pos[2], id, key); err != nil {
			return fmt.Errorf("write entity diff: %w", err)
		}
		return nil
	}
	pairedOurs := make([]bool, len(o))
	for i, ve := range v {
		j := matched[i]
		if j < 0 {
			if err := tally(ve, "<only vanilla>"); err != nil {
				return err
			}
			continue
		}
		pairedOurs[j] = true
		rep.Paired++
		keys := differingEntityKeys(ve, o[j])
		if len(keys) == 0 {
			rep.Identical++
		}
		for _, key := range keys {
			if err := tally(ve, key); err != nil {
				return err
			}
		}
	}
	for j, oe := range o {
		if !pairedOurs[j] {
			if err := tally(oe, "<only ours>"); err != nil {
				return err
			}
		}
	}
	return nil
}

// entityPairDistance is how far apart, in blocks, two entities of one id may
// stand and still pair.
const entityPairDistance = 1.0

// pairEntities pairs each vanilla entity, in order, with the nearest unpaired
// ours of the same id within entityPairDistance; of equally near ones the
// first in o wins. Both are sorted by sortedEntities. The result holds the
// index into o per vanilla entity, -1 for none.
func pairEntities(v, o []nbt.Compound) []int {
	out := make([]int, len(v))
	taken := make([]bool, len(o))
	for i, ve := range v {
		out[i] = -1
		vid, _ := nbt.Get[nbt.String](ve, "id")
		vp, ok := entityPos(ve)
		if !ok {
			continue
		}
		best := entityPairDistance * entityPairDistance
		for j, oe := range o {
			oid, _ := nbt.Get[nbt.String](oe, "id")
			op, ok := entityPos(oe)
			if taken[j] || oid != vid || !ok {
				continue
			}
			dx, dy, dz := vp[0]-op[0], vp[1]-op[1], vp[2]-op[2]
			if d := dx*dx + dy*dy + dz*dz; d < best || d == best && out[i] < 0 {
				best, out[i] = d, j
			}
		}
		if out[i] >= 0 {
			taken[out[i]] = true
		}
	}
	return out
}

// sortedEntities is a copy of the entities sorted by id, then x, y and z.
func sortedEntities(es []nbt.Compound) []nbt.Compound {
	es = slices.Clone(es)
	slices.SortStableFunc(es, func(a, b nbt.Compound) int {
		aid, _ := nbt.Get[nbt.String](a, "id")
		bid, _ := nbt.Get[nbt.String](b, "id")
		if c := strings.Compare(string(aid), string(bid)); c != 0 {
			return c
		}
		ap, _ := entityPos(a)
		bp, _ := entityPos(b)
		return slices.Compare(ap[:], bp[:])
	})
	return es
}

// entityPos is an entity's saved Pos; false without three doubles.
func entityPos(e nbt.Compound) ([3]float64, bool) {
	var out [3]float64
	list, _ := nbt.Get[nbt.List](e, "Pos")
	if len(list.Items) != 3 {
		return out, false
	}
	for i, t := range list.Items {
		d, ok := t.(nbt.Double)
		if !ok {
			return out, false
		}
		out[i] = float64(d)
	}
	return out, true
}

// differingEntityKeys lists, sorted, the top-level keys a pair differs in:
// Pos and Rotation always, then every other key both sides have but UUID and
// the unseededEntityKeys. Passengers compare as their ids, attributes by id
// (attributesEqual) and Rotation with the yaw folded into [0, 360); two empty
// lists are equal whatever their element types.
func differingEntityKeys(v, o nbt.Compound) []string {
	id, _ := nbt.Get[nbt.String](v, "id")
	var keys []string
	for key, tag := range v {
		other, ok := o[key]
		var equal bool
		switch {
		case key == "UUID" || unseededEntityKeys[[2]string{string(id), key}]:
			continue
		case !ok:
			if key == "Pos" || key == "Rotation" {
				keys = append(keys, key)
			}
			continue
		case key == "Passengers":
			equal = tagsEqual(passengerIDs(tag), passengerIDs(other))
		case key == "attributes":
			equal = attributesEqual(id, tag, other)
		case key == "Rotation":
			equal = rotationsEqual(tag, other)
		default:
			equal = tagsEqual(tag, other)
		}
		if !equal {
			keys = append(keys, key)
		}
	}
	for _, key := range []string{"Pos", "Rotation"} {
		if _, inV := v[key]; !inV {
			if _, inO := o[key]; inO {
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

// unseededEntityKeys are the (id, key) pairs vanilla rolls on the entity's
// own unseeded random, so no generator can match them.
var unseededEntityKeys = map[[2]string]bool{
	{"minecraft:chicken", "EggLayTime"}:   true, // vanilla: Chicken.eggTime
	{"minecraft:armadillo", "scute_time"}: true, // vanilla: Armadillo.pickNextScuteDropTime
	{"minecraft:shulker", "Rotation"}:     true, // vanilla: LivingEntity.<init> (unseeded yaw; end-city sentries skip finalizeSpawn)
}

// tagsEqual compares NBT tags by value, treating plain and ordered compounds
// alike and treating any two empty lists as equal regardless of element type.
func tagsEqual(a, b nbt.Tag) bool {
	switch a := a.(type) {
	case nbt.List:
		b, ok := b.(nbt.List)
		if !ok || len(a.Items) != len(b.Items) {
			return false
		}
		if len(a.Items) == 0 {
			return true
		}
		return a.Elem == b.Elem && slices.EqualFunc(a.Items, b.Items, tagsEqual)
	case nbt.CompoundTag:
		b, ok := b.(nbt.CompoundTag)
		return ok && maps.EqualFunc(a.Entries(), b.Entries(), tagsEqual)
	}
	return reflect.DeepEqual(a, b)
}

// yawTolerance is how far apart, in degrees, two folded yaws may be and still
// match: a float32 yaw stored past 360 has lost about 3e-5 of precision.
const yawTolerance = 1e-3

// rotationsEqual compares two Rotation lists, the yaw folded into [0, 360)
// and the pitch as it is.
func rotationsEqual(a, b nbt.Tag) bool {
	al, _ := a.(nbt.List)
	bl, _ := b.(nbt.List)
	if len(al.Items) != 2 || len(bl.Items) != 2 {
		return tagsEqual(a, b)
	}
	ay, aok := al.Items[0].(nbt.Float)
	by, bok := bl.Items[0].(nbt.Float)
	if !aok || !bok {
		return tagsEqual(a, b)
	}
	d := math.Abs(foldYaw(float64(ay)) - foldYaw(float64(by)))
	return min(d, 360-d) <= yawTolerance && tagsEqual(al.Items[1], bl.Items[1])
}

// foldYaw is a yaw in degrees folded into [0, 360).
func foldYaw(yaw float64) float64 {
	if yaw = math.Mod(yaw, 360); yaw < 0 {
		yaw += 360
	}
	return yaw
}

// zombieKinds are the entities whose attributes vanilla rolls in part on
// the entity's unseeded random (vanilla: Zombie.handleAttributes).
var zombieKinds = map[nbt.String]bool{
	"minecraft:zombie": true, "minecraft:husk": true, "minecraft:drowned": true,
	"minecraft:zombie_villager": true, "minecraft:zombified_piglin": true,
}

// zombieRolledModifiers are, per attribute, the modifier ids
// Zombie.handleAttributes rolls; leader_zombie_bonus is dropped on every
// attribute.
var zombieRolledModifiers = map[string]nbt.String{
	"minecraft:knockback_resistance": "minecraft:random_spawn_bonus",
	"minecraft:follow_range":         "minecraft:zombie_random_spawn_bonus",
}

// attributeEntry is one attribute of an attributes list: its base, nil when
// unsaved or dropped, and its modifiers sorted by id.
type attributeEntry struct {
	base      nbt.Tag
	modifiers []nbt.Tag
}

// attributeEntries maps an attributes list's ids to their entries, less what
// a zombie kind rolls on its unseeded random.
func attributeEntries(id nbt.String, tag nbt.Tag) map[string]attributeEntry {
	list, _ := tag.(nbt.List)
	out := make(map[string]attributeEntry, len(list.Items))
	zombie := zombieKinds[id]
	for _, item := range list.Items {
		c, _ := item.(nbt.Compound)
		attr, _ := nbt.Get[nbt.String](c, "id")
		e := attributeEntry{base: c["base"]}
		if zombie && attr == "minecraft:spawn_reinforcements" {
			e.base = nil
		}
		mods, _ := nbt.Get[nbt.List](c, "modifiers")
		for _, m := range mods.Items {
			mid, _ := nbt.Get[nbt.String](m.(nbt.Compound), "id")
			if zombie && (mid == "minecraft:leader_zombie_bonus" || mid == zombieRolledModifiers[string(attr)]) {
				continue
			}
			e.modifiers = append(e.modifiers, m)
		}
		slices.SortStableFunc(e.modifiers, func(a, b nbt.Tag) int {
			aid, _ := nbt.Get[nbt.String](a.(nbt.Compound), "id")
			bid, _ := nbt.Get[nbt.String](b.(nbt.Compound), "id")
			return strings.Compare(string(aid), string(bid))
		})
		out[string(attr)] = e
	}
	return out
}

// attributesEqual compares two attributes lists by id, since vanilla's order
// comes from AttributeMap's hash: the ids both hold must agree on base and
// modifiers, and an id only one holds differs only when it has modifiers.
func attributesEqual(id nbt.String, a, b nbt.Tag) bool {
	ae, be := attributeEntries(id, a), attributeEntries(id, b)
	for attr, x := range ae {
		y, ok := be[attr]
		if !ok {
			if len(x.modifiers) > 0 {
				return false
			}
			continue
		}
		if !tagsEqual(x.base, y.base) || !slices.EqualFunc(x.modifiers, y.modifiers, tagsEqual) {
			return false
		}
	}
	for attr, y := range be {
		if _, ok := ae[attr]; !ok && len(y.modifiers) > 0 {
			return false
		}
	}
	return true
}

// passengerIDs is a Passengers list's ids in order, as a list of strings.
func passengerIDs(tag nbt.Tag) nbt.Tag {
	list, _ := tag.(nbt.List)
	out := nbt.List{Elem: nbt.TypeString, Items: make([]nbt.Tag, 0, len(list.Items))}
	for _, item := range list.Items {
		p, _ := item.(nbt.Compound)
		id, _ := nbt.Get[nbt.String](p, "id")
		out.Items = append(out.Items, id)
	}
	return out
}

// addChunk compares the block entities of one chunk, tallying each differing
// (id, key) in pairs and writing it to diff as "be x y z id key".
func (rep *blockEntityReport) addChunk(v, o *chunkData, pairs map[[2]string]int, diff io.Writer) error {
	positions := slices.Collect(maps.Keys(v.BlockEntities))
	for pos := range o.BlockEntities {
		if _, ok := v.BlockEntities[pos]; !ok {
			positions = append(positions, pos)
		}
	}
	slices.SortFunc(positions, func(a, b [3]int32) int {
		return slices.Compare(a[:], b[:])
	})
	for _, pos := range positions {
		vb, inV := v.BlockEntities[pos]
		ob, inO := o.BlockEntities[pos]
		vb, ob = comparableBlockEntity(vb), comparableBlockEntity(ob)
		id, _ := nbt.Get[nbt.String](vb, "id")
		if !inV {
			id, _ = nbt.Get[nbt.String](ob, "id")
		}
		var keys []string
		switch {
		case !inO:
			rep.Vanilla++
			keys = []string{"<only vanilla>"}
		case !inV:
			rep.Ours++
			keys = []string{"<only ours>"}
		default:
			rep.Vanilla++
			rep.Ours++
			if tagsEqual(vb["id"], ob["id"]) {
				rep.SameID++
			}
			if tagsEqual(vb, ob) {
				rep.Identical++
				continue
			}
			for key, tag := range vb {
				if other, ok := ob[key]; !ok || !tagsEqual(tag, other) {
					keys = append(keys, key)
				}
			}
			for key := range ob {
				if _, ok := vb[key]; !ok {
					keys = append(keys, key)
				}
			}
			slices.Sort(keys)
		}
		for _, key := range keys {
			pairs[[2]string{string(id), key}]++
			if diff != nil {
				if _, err := fmt.Fprintf(diff, "be %d %d %d %s %s\n", pos[0], pos[1], pos[2], id, key); err != nil {
					return fmt.Errorf("write block entity diff: %w", err)
				}
			}
		}
	}
	return nil
}

// comparableBlockEntity is a copy of a compound without the keys that only
// locate it; nil stays nil.
func comparableBlockEntity(c nbt.Compound) nbt.Compound {
	if c == nil {
		return nil
	}
	c = maps.Clone(c)
	for _, key := range []string{"x", "y", "z", "keepPacked"} {
		delete(c, key)
	}
	return c
}

// sortedBlockEntityPairs orders the tally by count descending, then id and key.
func sortedBlockEntityPairs(pairs map[[2]string]int) []blockEntityPair {
	out := make([]blockEntityPair, 0, len(pairs))
	for p, n := range pairs {
		out = append(out, blockEntityPair{p[0], p[1], n})
	}
	slices.SortFunc(out, func(a, b blockEntityPair) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out
}

// sortedConfusions aggregates state differences by block name.
func sortedConfusions(pairs map[[2]uint16]int) []confusion {
	byName := map[[2]string]int{}
	for p, n := range pairs {
		byName[[2]string{blockName(p[0]), blockName(p[1])}] += n
	}
	out := make([]confusion, 0, len(byName))
	for p, n := range byName {
		out = append(out, confusion{p[0], p[1], n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Vanilla != b.Vanilla {
			return a.Vanilla < b.Vanilla
		}
		return a.Ours < b.Ours
	})
	return out
}

// rankFirst keeps residual chunks in recorded order, excluding classified scheduled-tick differences.
func (rep *report) rankFirst(recorded [][2]int32, top int) {
	if recorded == nil {
		return
	}
	rep.FirstDiffering = make([]firstDifference, 0)
	chunks := make(map[[2]int32]chunkResult, len(rep.Chunks))
	for _, c := range rep.Chunks {
		chunks[[2]int32{c.X, c.Z}] = c
	}
	for i, pos := range recorded {
		c, ok := chunks[pos]
		if !ok || c.DifferingBlocks == 0 {
			continue
		}
		if len(rep.FirstDiffering) >= top {
			break
		}
		rep.FirstDiffering = append(rep.FirstDiffering, firstDifference{c.X, c.Z, i, c.DifferingBlocks, c.BubbleColumnWater, c.FluidTickResidue, c.PostProcessAge, c.Pairs[:min(3, len(c.Pairs))]})
		delete(chunks, pos)
	}
}

// stateText formats a state with sorted property keys for a diff line.
func stateText(state uint16) string {
	var out strings.Builder
	out.WriteString(strip(blockName(state)))
	props := registry.StateProps(state)
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		out.WriteString("," + key + "=" + props[key])
	}
	return out.String()
}

// compareChunk compares one chunk, adding to the per-section stats and to
// the mismatched (vanilla, ours) state pairs.
func compareChunk(v, o *chunkData, ticks map[[3]int32]bool, sections []sectionStat, pairs map[[2]uint16]int, diff io.Writer) (chunkResult, error) {
	res := chunkResult{X: v.X, Z: v.Z}
	localPairs := map[[2]uint16]int{}
	for i, vs := range v.Blocks {
		ourState := o.Blocks[i]
		sec := &sections[i/(columnBlocks*16)]
		exact := vs == ourState
		shape := classCache[vs] == classCache[ourState]
		res.Exact.add(exact)
		res.Shape.add(shape)
		if classCache[vs] != classAir || classCache[ourState] != classAir {
			res.NonAir.add(exact)
		}
		sec.Exact.add(exact)
		sec.Shape.add(shape)
		if !exact {
			pairs[[2]uint16{vs, ourState}]++
			if blockName(vs) == "minecraft:bubble_column" && blockName(ourState) == "minecraft:water" {
				res.BubbleColumnWater++
			} else if headAgeOnly(vs, ourState) {
				res.PostProcessAge++
			} else if pos := [3]int32{v.X*16 + int32(i%16), int32(v.Dim.MinY + i/columnBlocks), v.Z*16 + int32(i/16%16)}; fluidTickResidue(vs, ourState, pos, ticks) ||
				i >= columnBlocks && vineTipResidue(vs, ourState) && fluidTickResidue(v.Blocks[i-columnBlocks], o.Blocks[i-columnBlocks], [3]int32{pos[0], pos[1] - 1, pos[2]}, ticks) {
				res.FluidTickResidue++
			} else {
				res.DifferingBlocks++
				localPairs[[2]uint16{vs, ourState}]++
			}
			if diff != nil {
				if _, err := fmt.Fprintf(diff, "%d %d %d %s %s\n", int64(v.X)*16+int64(i%16), v.Dim.MinY+i/columnBlocks, int64(v.Z)*16+int64(i/16%16), stateText(vs), stateText(ourState)); err != nil {
					return res, fmt.Errorf("write block diff: %w", err)
				}
			}
		}
	}
	for i, vb := range v.Biomes {
		res.Biome.add(vb == o.Biomes[i])
	}
	for z := range 16 {
		for x := range 16 {
			vy, oy := surfaceY(v, x, z), surfaceY(o, x, z)
			res.Surface.add(vy == oy)
			res.SurfaceDiff += abs(vy - oy)
			vt, ot := terrainTopY(v, x, z), terrainTopY(o, x, z)
			res.TerrainTop.add(vt == ot)
			res.TerrainTopDiff += abs(vt - ot)
		}
	}
	res.Pairs = sortedConfusions(localPairs)
	return res, nil
}

// fluidTickResidue conservatively classifies eligible pairs at or beside a generated tick.
func fluidTickResidue(vanilla, ours uint16, pos [3]int32, ticks map[[3]int32]bool) bool {
	v, o := strip(blockName(vanilla)), strip(blockName(ours))
	eligible := (v == "water" || v == "lava") && (o == "air" || o == "cave_air" || o == "water" || o == "lava" || categoryOf(o) == "plant" || caveVine(o)) ||
		(v == "obsidian" || v == "cobblestone") && o == "lava"
	if !eligible {
		return false
	}
	for _, offset := range [][3]int32{{}, {1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if ticks[[3]int32{pos[0] + offset[0], pos[1] + offset[1], pos[2] + offset[2]}] {
			return true
		}
	}
	return false
}

// headAgeOnly is whether both states are the same growing-plant head and
// differ only in age.
// vanilla: GrowingPlantHeadBlock.getStateForPlacement, LevelChunk.postProcessGeneration
func headAgeOnly(vanilla, ours uint16) bool {
	b := registry.BlockOf(vanilla)
	if registry.BlockOf(ours) != b || !plantHeads[b] {
		return false
	}
	age, _ := registry.StateInt(vanilla, registry.PropAge)
	same, ok := registry.WithInt(ours, registry.PropAge, age)
	return ok && same == vanilla
}

var plantHeads = map[registry.Block]bool{
	registry.BlockCaveVines: true, registry.BlockWeepingVines: true,
	registry.BlockTwistingVines: true, registry.BlockKelp: true,
}

// vineTipResidue is whether a cave vine can be vanilla's tip only because
// fluid washed out the vine below it, which the caller checks is residue.
// vanilla: GrowingPlantBodyBlock.updateShape
func vineTipResidue(vanilla, ours uint16) bool {
	return strip(blockName(vanilla)) == "cave_vines" && strip(blockName(ours)) == "cave_vines_plant"
}

// caveVine is whether a stripped block name is part of a cave vine, which
// flowing fluid washes out since it does not block motion.
// vanilla: FlowingFluid.canHoldAnyFluid
func caveVine(name string) bool { return name == "cave_vines" || name == "cave_vines_plant" }

// surfaceY is the Y of the highest non-air block in a column, or one below
// the dimension for a column of nothing but air.
// vanilla: Heightmap.Types.WORLD_SURFACE
func surfaceY(c *chunkData, x, z int) int {
	for y := c.Dim.MinY + c.Dim.Height - 1; y >= c.Dim.MinY; y-- {
		if classCache[c.Blocks[c.index(x, y, z)]] != classAir {
			return y
		}
	}
	return c.Dim.MinY - 1
}

// terrainTopY is the Y of the highest terrain block in a column: stone and the
// blocks that replace it, not fluid, plants, trees, snow layers or ice. It is
// the column's height before surface rules and features dress it, bar caves
// that open it.
func terrainTopY(c *chunkData, x, z int) int {
	for y := c.Dim.MinY + c.Dim.Height - 1; y >= c.Dim.MinY; y-- {
		if terrainCache[c.Blocks[c.index(x, y, z)]] {
			return y
		}
	}
	return c.Dim.MinY - 1
}

// terrainCache is whether a state ID is a terrain block.
var terrainCache = func() []bool {
	out := make([]bool, 1<<16)
	for i := range out {
		name := strip(blockName(uint16(i)))
		switch stageCategories[categoryIndex[categoryOf(name)]].Name {
		case "stone", "deepslate", "bedrock", "surface layer", "ore blob", "ore":
			out[i] = true
		default:
			out[i] = name == "snow_block"
		}
	}
	return out
}()

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// writeText prints the report for a reader: the totals per stage, the match
// per section height, the commonest mismatches and the worst chunks.
func (rep *report) writeText(w io.Writer, top int) {
	r := rep.Range
	fmt.Fprintf(w, "seed %d, %s, chunks %d,%d to %d,%d (%d chunks)\n\n", rep.Seed, rep.Dimension, r.X0, r.Z0, r.X1, r.Z1, len(rep.Chunks))
	fmt.Fprintf(w, "  exact block state         %s\n", rep.Exact)
	fmt.Fprintf(w, "  exact, non-air blocks     %s\n", rep.NonAir)
	fmt.Fprintf(w, "  shape (air/fluid/solid)   %s\n", rep.Shape)
	fmt.Fprintf(w, "  biome (4x4x4 cells)       %s\n", rep.Biome)
	fmt.Fprintf(w, "  surface height (columns)  %s  (mean |dy| %.2f)\n", rep.Surface, rep.SurfaceDiff)
	fmt.Fprintf(w, "  terrain top (columns)     %s  (mean |dy| %.2f)\n", rep.TerrainTop, rep.TerrainTopDiff)
	fmt.Fprintf(w, "  post-process head age     %d\n\n", rep.PostProcessAge)

	fmt.Fprintln(w, "per section (y: exact / shape):")
	for _, s := range slices.Backward(rep.SectionY) {
		fmt.Fprintf(w, "  %4d..%-4d %s / %s\n", s.Y*16, s.Y*16+15, s.Exact, s.Shape)
	}

	fmt.Fprintf(w, "\ntop mismatches (vanilla -> ours, blocks):\n")
	for i, c := range rep.Confusions {
		if i == top {
			break
		}
		fmt.Fprintf(w, "  %9d  %s -> %s\n", c.Count, strip(c.Vanilla), strip(c.Ours))
	}

	worst := append([]chunkResult(nil), rep.Chunks...)
	sort.SliceStable(worst, func(i, j int) bool { return worst[i].Exact.Percent() < worst[j].Exact.Percent() })
	fmt.Fprintf(w, "\nworst chunks (exact / non-air / shape / biome / surface):\n")
	for i, c := range worst {
		if i == top {
			break
		}
		fmt.Fprintf(w, "  %4d,%-4d %s / %s / %s / %s / %s\n", c.X, c.Z, c.Exact, c.NonAir, c.Shape, c.Biome, c.Surface)
	}
	if rep.FirstDiffering != nil {
		fmt.Fprintln(w, "\nfirst differing chunks in decoration order (x,z  order-index  differing-blocks  top-3 pairs; bubble_column -> water, fluid-tick residue and post-process age counted separately):")
		for _, c := range rep.FirstDiffering {
			fmt.Fprintf(w, "  %d,%d  %d  %d", c.X, c.Z, c.OrderIndex, c.DifferingBlocks)
			for _, p := range c.Pairs {
				fmt.Fprintf(w, "  %s -> %s (%d)", strip(p.Vanilla), strip(p.Ours), p.Count)
			}
			fmt.Fprintf(w, "  bubble_column -> water: %d  fluid-tick residue: %d  post-process age: %d\n", c.BubbleColumnWater, c.FluidTickResidue, c.PostProcessAge)
		}
	}
	if rep.Stages != nil {
		rep.Stages.writeText(w, top)
	}
	rep.BlockEntities.writeText(w, top)
	rep.Entities.writeText(w, top)
}

func strip(name string) string { return strings.TrimPrefix(name, "minecraft:") }
