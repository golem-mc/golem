package main

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// These three ask every block state what the light engine, the support rules
// and the collision sweep consult. A dumped state is identified by its block
// and properties; the block report maps each to the state ID the rest of the
// registry uses, and a state on either side without a partner on the other
// stops the generation rather than being written past.

func init() {
	for _, g := range []generator{
		{"blockshapes", "registry/blockshapes_gen.go: the collision boxes of the block families no Go code models", runBlockShapes},
		{"blocksupport", "registry/blocksupport_gen.go: what each block state offers the blocks hanging on it, and the blocks with no collision", runBlockSupport},
		{"lightdata", "registry/lightdata_gen.go: light dampening and face occlusion per block state", runLightData},
	} {
		register(g)
	}
}

// checkCoverage fails unless the dumped states and the report's are the same
// set.
func checkCoverage(dumped func(yield func(stateKey) bool), ids map[stateKey]int) error {
	seen := map[stateKey]bool{}
	var missing []stateKey
	dumped(func(k stateKey) bool {
		seen[k] = true
		if _, ok := ids[k]; !ok {
			missing = append(missing, k)
		}
		return true
	})
	if len(missing) > 0 {
		return fmt.Errorf("%d dumped states have no report entry, e.g. %v", len(missing), missing[0])
	}
	var uncovered []stateKey
	for k := range ids {
		if !seen[k] {
			uncovered = append(uncovered, k)
		}
	}
	if len(uncovered) > 0 {
		slices.SortFunc(uncovered, func(a, b stateKey) int { return ids[a] - ids[b] })
		return fmt.Errorf("%d report states are not in the dump, e.g. %v", len(uncovered), uncovered[0])
	}
	return nil
}

// blockShapeFamily reports whether the table carries a block: the beds, cakes (the
// candle cake is a CakeBlock), anvils, cauldrons, hopper, bell, flower pots, the
// pitcher crop, and the blocks whose shape is not the full cube their collision
// height suggests (cactus, honey block, brewing stand, conduit, dried ghast, heavy
// core, lectern, sea pickle, turtle and sniffer eggs, composter, decorated pot,
// dragon egg, azaleas, chorus plant, copper golem statues).
func blockShapeFamily(name string) bool {
	switch name {
	case "minecraft:cake",
		"minecraft:anvil", "minecraft:chipped_anvil", "minecraft:damaged_anvil",
		"minecraft:cauldron", "minecraft:water_cauldron", "minecraft:lava_cauldron", "minecraft:powder_snow_cauldron",
		"minecraft:hopper", "minecraft:bell", "minecraft:flower_pot", "minecraft:pitcher_crop",
		"minecraft:cactus", "minecraft:honey_block", "minecraft:brewing_stand", "minecraft:conduit",
		"minecraft:dried_ghast", "minecraft:heavy_core", "minecraft:lectern", "minecraft:sea_pickle",
		"minecraft:turtle_egg", "minecraft:sniffer_egg", "minecraft:composter", "minecraft:decorated_pot",
		"minecraft:dragon_egg", "minecraft:azalea", "minecraft:flowering_azalea", "minecraft:chorus_plant":
		return true
	}
	return strings.HasSuffix(name, "_bed") || strings.HasSuffix(name, "candle_cake") ||
		strings.HasPrefix(name, "minecraft:potted_") || strings.HasSuffix(name, "copper_golem_statue")
}

var (
	shapeStateLine = regexp.MustCompile(stateName + `((?:\|[^|\n]*)*)$`)
	shapeBox       = regexp.MustCompile(`\|([^|,\n]+),([^|,\n]+),([^|,\n]+),([^|,\n]+),([^|,\n]+),([^|,\n]+)`)
)

func runBlockShapes(e *env) error {
	out, err := e.java("BlockShapes", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	dumped := map[stateKey][][6]int{} // boxes in 32nds of a block
	for line := range strings.SplitSeq(out, "\n") {
		m := shapeStateLine.FindStringSubmatch(line)
		if m == nil || !blockShapeFamily(m[1]) {
			continue
		}
		var boxes [][6]int
		for _, bm := range shapeBox.FindAllStringSubmatch(m[3], -1) {
			var box [6]int
			for i, field := range bm[1:] {
				// The table stores whole 32nds of a block.
				scaled := atof(field) * 2
				if math.Abs(scaled-math.Round(scaled)) > 1e-6 || math.Round(scaled) < -128 || math.Round(scaled) > 127 {
					return fmt.Errorf("%s[%s]: box edge %s is not a 32nd of a block", m[1], m[2], field)
				}
				box[i] = int(math.Round(scaled))
			}
			boxes = append(boxes, box)
		}
		dumped[keyOf(m[1], m[2])] = boxes
	}

	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	ids := stateIDs(blocks)
	family := map[stateKey]bool{}
	for k := range ids {
		if blockShapeFamily(k.name) {
			family[k] = true
		}
	}
	var missing []stateKey
	for k := range family {
		if _, ok := dumped[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d states of the generated families are not in the dump, e.g. %v", len(missing), missing[0])
	}
	for k := range dumped {
		if !family[k] {
			return fmt.Errorf("a dumped state matches no report entry, e.g. %v", k)
		}
	}
	if len(dumped) < 300 {
		return fmt.Errorf("only %d states in the dump; is the classpath right?", len(dumped))
	}

	// The empty shapes stay with the collision-top table; only states with
	// boxes are written.
	boxesByID := map[int][][6]int{}
	for k, boxes := range dumped {
		if len(boxes) > 0 {
			boxesByID[ids[k]] = boxes
		}
	}
	stateIDsSorted := make([]int, 0, len(boxesByID))
	for id := range boxesByID {
		stateIDsSorted = append(stateIDsSorted, id)
	}
	slices.Sort(stateIDsSorted)
	var shapes [][][6]int // distinct box lists
	index := map[string]int{}
	type entry struct{ state, shape int }
	var entries []entry
	for _, id := range stateIDsSorted {
		boxes := boxesByID[id]
		key := fmt.Sprint(boxes)
		i, seen := index[key]
		if !seen {
			i = len(shapes)
			index[key] = i
			shapes = append(shapes, boxes)
		}
		entries = append(entries, entry{id, i})
	}

	f := e.file("registry", "by tools/gen blockshapes from a vanilla {version} server")
	f.WriteString(`// blockShapeEntries names every block state whose family the generated
// table carries, one entry per state: the state ID and the index of its
// shape in blockShapeBoxes.
var blockShapeEntries = [...]struct {
	state uint16
	shape uint16
}{
`)
	for _, en := range entries {
		f.Printf("\t{%d, %d},\n", en.state, en.shape)
	}
	f.WriteString(`}

// blockShapeBoxes holds each distinct shape's boxes in 32nds of a
// block, the form the vanilla block classes' sixteenths quantise to.
var blockShapeBoxes = [][][6]int8{
`)
	for _, boxes := range shapes {
		f.WriteString("\t{\n")
		for _, b := range boxes {
			f.Printf("\t\t{%d, %d, %d, %d, %d, %d},\n", b[0], b[1], b[2], b[3], b[4], b[5])
		}
		f.WriteString("\t},\n")
	}
	f.WriteString("}\n")
	e.logf("wrote %d states with %d shapes", len(entries), len(shapes))
	return e.emit("registry/blockshapes_gen.go", f)
}

var supportStateLine = regexp.MustCompile(stateName +
	`\|(\d+)\|(\d+)\|(\d+)\|(\d+)\|(\d+)\|([^|]+)\|(\d+)\|(\d+)\|(\d+)$`)

func runBlockSupport(e *env) error {
	out, err := e.java("BlockSupport", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	support := map[stateKey]int{}   // support bits
	collision := map[stateKey]int{} // collision bits, 4096ths of a block
	walls := map[stateKey]int{}     // wall test shapes the DOWN face covers
	faces := map[stateKey]int{}     // full collision faces
	for line := range strings.SplitSeq(out, "\n") {
		m := supportStateLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := keyOf(m[1], m[2])
		full, center, rigid, attach, flags := atoi(m[3]), atoi(m[4]), atoi(m[5]), atoi(m[6]), atoi(m[7])
		support[key] = full | center<<6 | rigid<<12 | attach<<18 | flags<<24
		top := atof(m[8])
		scaled := top * 4096
		if math.Abs(scaled-math.Round(scaled)) > 1e-9 {
			return fmt.Errorf("%s[%s]: collision max Y %v is not a 4096th", m[1], m[2], top)
		}
		// 14 bits, enough for the out-of-cell shapes (a piston head's rod
		// reaches a quarter past its cell) plus the flat bit.
		if r := int(math.Round(scaled)); r < 0 || r > 1<<14-1 {
			return fmt.Errorf("%s[%s]: collision max Y %v is out of the range", m[1], m[2], top)
		}
		collision[key] = int(math.Round(scaled)) | atoi(m[9])<<14
		walls[key] = atoi(m[10])
		faces[key] = atoi(m[11])
	}
	if err := needAtLeast(len(support), 20000, "states"); err != nil {
		return err
	}
	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	ids := stateIDs(blocks)
	if err := checkCoverage(func(yield func(stateKey) bool) {
		for k := range support {
			if !yield(k) {
				return
			}
		}
	}, ids); err != nil {
		return err
	}
	byID := func(m map[stateKey]int) map[int]int {
		out := make(map[int]int, len(m))
		for k, v := range m {
			out[ids[k]] = v
		}
		return out
	}
	supportByID, collisionByID, wallsByID := byID(support), byID(collision), byID(walls)
	// A block is in NoCollision only when every one of its states has an empty
	// shape: the set is keyed by name, so a gate that is empty only open, or a
	// snow layer of height 1, must not be.
	solid := map[string]bool{}
	empty := map[string]bool{}
	for k, c := range collision {
		if c&(1<<14-1) != 0 {
			solid[k.name] = true
		} else {
			empty[k.name] = true
		}
	}
	for name := range solid {
		delete(empty, name)
	}
	supportRuns, collisionRuns, wallRuns := runsOf(supportByID), runsOf(collisionByID), runsOf(wallsByID)
	faceRuns := runsOf(byID(faces))

	f := e.file("registry", "by tools/gen blocksupport from a vanilla {version} server")
	f.WriteString(`// blockSupportGen holds each block state's support bits as runs: a run
// starts at its state ID and lasts until the next run's. The bits are
// laid out as the Support constants say.
var blockSupportGen = [...]struct {
	from uint16
	bits uint32
}{
`)
	for _, r := range supportRuns {
		f.Printf("\t{%d, 0x%07x},\n", r.from, r.value)
	}
	f.WriteString(`}

// blockCollisionGen holds each block state's collision bits as runs,
// laid out as the collisionTopMask and collisionFlat bits say.
var blockCollisionGen = [...]struct {
	from uint16
	bits uint16
}{
`)
	for _, r := range collisionRuns {
		f.Printf("\t{%d, 0x%x},\n", r.from, r.value)
	}
	f.WriteString(`}

// blockWallCoverGen holds, as runs, which of WallBlock's test shapes each
// state's DOWN collision face covers: bit 0 the post, bits 1 to 4 the
// north, south, west and east arms.
var blockWallCoverGen = [...]struct {
	from uint16
	bits uint8
}{
`)
	for _, r := range wallRuns {
		f.Printf("\t{%d, 0x%x},\n", r.from, r.value)
	}
	f.WriteString(`}

// blockCollisionFacesGen holds each state's full collision faces as runs,
// one bit per direction in Direction ordinal order.
var blockCollisionFacesGen = [...]struct {
	from uint16
	bits uint8
}{
`)
	for _, r := range faceRuns {
		f.Printf("\t{%d, 0x%x},\n", r.from, r.value)
	}
	f.WriteString("}\n\n")
	f.WriteString("// blockSupportStates is the number of block states the runs cover.\n")
	f.Printf("const blockSupportStates = %d\n", len(supportByID))
	f.WriteString("// blockCollisionStates is the number of block states the collision runs cover.\n")
	f.Printf("const blockCollisionStates = %d\n", len(collisionByID))
	f.WriteString(`// NoCollision is the set of blocks whose collision shape is empty in every
// state, so a body standing in the cell does not stop a placement or block a
// walk. It is read off the same shapes as the collision runs above.
// vanilla: BlockState.getCollisionShape
`)
	f.Set("NoCollision", sortedKeys(empty))
	f.WriteString("\n")
	e.logf("wrote %d states as %d support and %d collision runs, %d blocks with no collision", len(support), len(supportRuns), len(collisionRuns), len(empty))
	return e.emit("registry/blocksupport_gen.go", f)
}

var (
	lightStateLine = regexp.MustCompile(stateName + `\|(\d+)\|([0-9,]+)$`)
	lightMaskLine  = regexp.MustCompile(`MASK (\d+) ([0-9a-f:]+)$`)
)

func runLightData(e *env) error {
	out, err := e.java("LightData", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	type lightState struct {
		damp  int
		codes []int
	}
	states := map[stateKey]lightState{}
	var masks [][]uint64
	for line := range strings.SplitSeq(out, "\n") {
		if m := lightMaskLine.FindStringSubmatch(line); m != nil {
			if idx := atoi(m[1]); idx != len(masks) {
				return fmt.Errorf("mask %d out of order at %d", idx, len(masks))
			}
			var words []uint64
			for w := range strings.SplitSeq(m[2], ":") {
				v, err := strconv.ParseUint(w, 16, 64)
				if err != nil {
					return fmt.Errorf("mask word %q: %w", w, err)
				}
				words = append(words, v)
			}
			masks = append(masks, words)
			continue
		}
		if m := lightStateLine.FindStringSubmatch(line); m != nil {
			var codes []int
			for c := range strings.SplitSeq(m[4], ",") {
				codes = append(codes, atoi(c))
			}
			states[keyOf(m[1], m[2])] = lightState{atoi(m[3]), codes}
		}
	}
	if len(masks) >= 254 {
		return fmt.Errorf("%d face masks; the code space only holds 254", len(masks))
	}
	if err := needAtLeast(len(states), 20000, "states"); err != nil {
		return err
	}
	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	ids := stateIDs(blocks)
	if err := checkCoverage(func(yield func(stateKey) bool) {
		for k := range states {
			if !yield(k) {
				return
			}
		}
	}, ids); err != nil {
		return err
	}
	damp := make([]byte, 65536)
	type faceRow struct {
		state int
		codes []int
	}
	var faceRows []faceRow
	for k, s := range states {
		id := ids[k]
		damp[id] = byte(s.damp)
		if slices.ContainsFunc(s.codes, func(c int) bool { return c != 0 }) {
			faceRows = append(faceRows, faceRow{id, s.codes})
		}
	}
	slices.SortFunc(faceRows, func(a, b faceRow) int { return a.state - b.state })

	f := e.file("registry", "by tools/gen lightdata from a vanilla {version} server")
	f.WriteString(`// lightDampeningGen is getLightDampening for every block state, by state ID:
// 15 for states that occlude light fully, 1 for partial blockers (leaves,
// water, waterlogged states), 0 for states light passes straight through
// (glass, fences, chests, single slabs, stairs).
`)
	var dampText strings.Builder
	for _, b := range damp {
		fmt.Fprintf(&dampText, `\x%02x`, b)
	}
	f.Printf("var lightDampeningGen = \"%s\"\n\n", dampText.String())
	f.WriteString(`// lightFacesGen holds the six face occlusion codes of the states light can
// test per face, by state ID, in Direction ordinal order (down, up, north,
// south, west, east): 0 for an empty face, 1 for a face covering the whole
// plane, otherwise an index into lightFaceMasksGen offset by two. States
// absent from the map have only empty faces and never take part in the
// per-face test.
var lightFacesGen = map[uint16][6]uint8{
`)
	for _, row := range faceRows {
		codes := make([]string, len(row.codes))
		for i, c := range row.codes {
			codes[i] = strconv.Itoa(c)
		}
		f.Printf("\t%d: {%s},\n", row.state, strings.Join(codes, ", "))
	}
	f.WriteString(`}

// lightFaceMasksGen holds the footprint of each partial face shape on the
// 16x16 grid of the two axes that are not the face's own, in X/Y/Z order,
// bit x + z*16 of four words.
var lightFaceMasksGen = [][4]uint64{
`)
	for _, words := range masks {
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = fmt.Sprintf("0x%x", w)
		}
		f.Printf("\t{%s},\n", strings.Join(parts, ", "))
	}
	f.WriteString("}\n")
	e.logf("wrote %d states (%d with face data, %d masks)", len(states), len(faceRows), len(masks))
	return e.emit("registry/lightdata_gen.go", f)
}
