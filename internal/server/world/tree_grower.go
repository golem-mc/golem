package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// weightedFeature is one configured feature key, without "minecraft:", and its weight.
type weightedFeature struct {
	key    string
	weight int
}

// intner is the slice of *rand.Rand a weighted draw needs.
type intner interface{ Intn(n int) int }

// pick draws one key; an empty list draws nothing and returns "".
// vanilla: WeightedList.getRandom
func pick(list []weightedFeature, r intner) string {
	total := 0
	for _, f := range list {
		total += f.weight
	}
	if total == 0 {
		return ""
	}
	n := r.Intn(total)
	for _, f := range list {
		if n -= f.weight; n < 0 {
			return f.key
		}
	}
	panic("unreachable")
}

type treeGrower struct {
	trees, megaTrees, flowerTrees []weightedFeature
	// minHeight is getMinimumHeight: the base height of the shortest tree's
	// trunk placer, 0 for a grower with only mega trees.
	minHeight int32
}

// vanilla: TreeGrower's built-in growers.
var saplingGrowers = map[registry.Block]treeGrower{
	registry.BlockOakSapling: {
		trees:       []weightedFeature{{"oak", 9}, {"fancy_oak", 1}},
		flowerTrees: []weightedFeature{{"oak_bees_005", 9}, {"fancy_oak_bees_005", 1}},
		minHeight:   4,
	},
	registry.BlockSpruceSapling: {
		trees:     []weightedFeature{{"spruce", 1}},
		megaTrees: []weightedFeature{{"mega_spruce", 1}, {"mega_pine", 1}},
		minHeight: 5,
	},
	registry.BlockBirchSapling: {
		trees:       []weightedFeature{{"birch", 1}},
		flowerTrees: []weightedFeature{{"birch_bees_005", 1}},
		minHeight:   5,
	},
	registry.BlockJungleSapling: {
		trees:     []weightedFeature{{"jungle_tree_no_vine", 1}},
		megaTrees: []weightedFeature{{"mega_jungle_tree", 1}},
		minHeight: 4,
	},
	registry.BlockAcaciaSapling: {trees: []weightedFeature{{"acacia", 1}}, minHeight: 5},
	registry.BlockCherrySapling: {
		trees:       []weightedFeature{{"cherry", 1}},
		flowerTrees: []weightedFeature{{"cherry_bees_005", 1}},
		minHeight:   7,
	},
	registry.BlockDarkOakSapling:    {megaTrees: []weightedFeature{{"dark_oak", 1}}},
	registry.BlockPaleOakSapling:    {megaTrees: []weightedFeature{{"pale_oak_bonemeal", 1}}},
	registry.BlockMangrovePropagule: {trees: []weightedFeature{{"mangrove", 15}, {"tall_mangrove", 85}}, minHeight: 2},
	registry.BlockAzalea:            azaleaGrower,
	registry.BlockFloweringAzalea:   azaleaGrower,
	registry.BlockPoplarSapling: {
		trees:     []weightedFeature{{"red_poplar", 1}, {"orange_poplar", 1}, {"yellow_poplar", 1}},
		minHeight: 7,
	},
}

var azaleaGrower = treeGrower{trees: []weightedFeature{{"azalea_tree", 1}}, minHeight: 4}

// feature draws the single tree, from the flower list when flowers are near
// and the grower has one. vanilla: TreeGrower.getConfiguredFeature
func (g treeGrower) feature(r intner, hasFlowers bool) string {
	if hasFlowers && len(g.flowerTrees) > 0 {
		return pick(g.flowerTrees, r)
	}
	return pick(g.trees, r)
}

// canGrow is the sapling's bone meal precondition: a grower with only mega
// trees needs the 2x2. Both draws are spent as vanilla spends them.
// vanilla: TreeGrower.canGrow
func (p *Physics) canGrow(x, y, z int32, g treeGrower) bool {
	feature := g.feature(p.rng, p.treeHasFlowers(x, y, z))
	mega := pick(g.megaTrees, p.rng)
	if feature == "" && mega != "" {
		_, _, ok := p.sapling2x2Origin(x, y, z)
		return ok
	}
	return true
}

// growSaplingTree draws the mega tree first, used only on a 2x2, then the
// flower-sensitive single tree. The private feature region makes refusal atomic.
// vanilla: TreeGrower.growTree
func (p *Physics) growSaplingTree(x, y, z int32, state uint16, changes *[]BlockChange) {
	g := saplingGrowers[registry.BlockOf(state)]
	if mega := pick(g.megaTrees, p.rng); mega != "" {
		if ox, oz, ok := p.sapling2x2Origin(x, y, z); ok {
			replacements := make([]BlockChange, 0, 4)
			for dx := range int32(2) {
				for dz := range int32(2) {
					replacements = append(replacements, BlockChange{X: ox + dx, Y: y, Z: oz + dz, State: airState})
				}
			}
			p.placeGrownFeature(mega, true, ox, y, oz, replacements, changes)
			return
		}
	}
	key := g.feature(p.rng, p.treeHasFlowers(x, y, z))
	if key == "" {
		return
	}
	empty := uint16(airState)
	if registry.StateBool(state, registry.PropWaterlogged) {
		empty = registry.BlockWater.DefaultState()
	}
	p.placeGrownFeature(key, true, x, y, z, []BlockChange{{X: x, Y: y, Z: z, State: empty}}, changes)
}

func (p *Physics) treeHasFlowers(x, y, z int32) bool {
	for dy := int32(-1); dy <= 1; dy++ {
		for dz := int32(-2); dz <= 2; dz++ {
			for dx := int32(-2); dx <= 2; dx++ {
				if registry.BlockOf(p.w.BlockAt(x+dx, y+dy, z+dz)).InTag(registry.TagIDFlowers) {
					return true
				}
			}
		}
	}
	return false
}

// placeGrownFeature grows a configured feature at a cell; structure marks a
// tree or mushroom, which StructureGrowEvent listeners may veto or edit before
// anything is written.
func (p *Physics) placeGrownFeature(key string, structure bool, x, y, z int32, replacements []BlockChange, changes *[]BlockChange) bool {
	// Physics.mu -> World.mu is also the order used by set/BlockAt. World
	// never acquires Physics.mu while holding World.mu; the region only reads
	// loaded chunks and never generates one or calls back into physics.
	cells, extras, ok := p.w.placeConfiguredFeature("minecraft:"+key, p.rng.Int63(), x, y, z, replacements)
	if !ok {
		return false
	}
	if structure && p.listens(HookStructure) {
		d := HookDecision{Kind: HookStructure, X: x, Y: y, Z: z, Old: p.w.BlockAt(x, y, z), Actor: p.boneMealActor, Cells: cells}
		if p.ask(&d) {
			return false
		}
		cells = d.Cells
	}
	var logs, leaves [][3]int32
	var wakes []struct {
		cell BlockChange
		old  uint16
	}
	// Publish the complete shape before waking neighbours: a root or decorator
	// must not break because its supporting cell has not been applied yet.
	for _, c := range cells {
		old := p.w.BlockAt(c.X, c.Y, c.Z)
		if !p.w.SetBlock(c.X, c.Y, c.Z, c.State) {
			continue
		}
		pos := [3]int32{c.X, c.Y, c.Z}
		if b := registry.BlockOf(c.State); b.IsLeaves() {
			leaves = append(leaves, pos)
		} else {
			if b.InTag(registry.TagIDLogs) {
				logs = append(logs, pos)
			}
			*changes = append(*changes, physicsChange(c.X, c.Y, c.Z, old, c.State))
			wakes = append(wakes, struct {
				cell BlockChange
				old  uint16
			}{c, old})
		}
	}
	// A bee nest's bees go in once its block stands.
	// vanilla: BeehiveDecorator.place
	for _, tag := range extras.BlockEntities {
		id, _ := nbt.Get[nbt.String](tag, "id")
		bx, by, bz := carriedPos(tag)
		p.w.SetBlockEntityTag(bx, by, bz, string(id), tag)
	}
	walked := p.settleGrownDistances(logs, leaves)
	p.noteGrownCells(leaves, walked, changes)
	for _, wake := range wakes {
		c := wake.cell
		p.neighborUpdate(c.X, c.Y, c.Z, wake.old, c.State, changes)
	}
	return true
}

// GrowFeature places the configured feature key (a tree or huge mushroom) with
// its origin at (x, y, z), as a sapling's growth does without the sapling; false
// when it does not fit or the key is not a placeable feature.
func (p *Physics) GrowFeature(key string, x, y, z int32) ([]BlockChange, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var changes []BlockChange
	ok := p.placeGrownFeature(key, true, x, y, z, nil, &changes)
	return changes, ok
}
