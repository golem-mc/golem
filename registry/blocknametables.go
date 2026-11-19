package registry

// The generated name-keyed maps BlockTypes, NoCollision, ConcreteOf,
// CopperScraped, AxeStrippables, CopperWaxOff and ShovelFlattenables are read
// once here; game logic reads these typed forms.

var (
	blockTypes        [BlockCount]BlockType
	noCollisionBlocks = BlocksWhere(func(name string) bool { return NoCollision[name] })
	concreteOf        = blockPairs(ConcreteOf)
	copperScraped     = blockPairs(CopperScraped)
	axeStrippables    = blockPairs(AxeStrippables)
	copperWaxOff      = blockPairs(CopperWaxOff)
	copperWaxed       = func() *[BlockCount]Block {
		t := new([BlockCount]Block)
		for waxed := range Block(BlockCount) {
			if bare, ok := waxed.Unwaxed(); ok {
				t[bare] = waxed + 1
			}
		}
		return t
	}()
	shovelFlattenables = blockPairs(ShovelFlattenables)
)

func init() {
	byName := make(map[string]BlockType, BlockTypeCount)
	for t, name := range blockTypeNames {
		byName[name] = BlockType(t)
	}
	for name, typ := range BlockTypes {
		if b, ok := BlockByName(name); ok {
			blockTypes[b] = byName[typ]
		}
	}
}

// Name is the registry name of the definition type, such as "minecraft:shelf".
func (t BlockType) Name() string {
	if t >= BlockTypeCount {
		return ""
	}
	return blockTypeNames[t]
}

// blockPairs resolves a name-to-name map into a table indexed by Block.
func blockPairs(m map[string]string) *[BlockCount]Block {
	t := new([BlockCount]Block)
	for from, to := range m {
		f, ok1 := BlockByName(from)
		g, ok2 := BlockByName(to)
		if ok1 && ok2 {
			t[f] = g + 1
		}
	}
	return t
}

// Type is the block's definition class, such as BlockTypeShelf
// (vanilla: the Block subclass).
func (b Block) Type() BlockType {
	if b >= BlockCount {
		return BlockTypeNone
	}
	return blockTypes[b]
}

// NoCollision reports whether every state of the block has an empty collision shape.
func (b Block) NoCollision() bool { return b < BlockCount && noCollisionBlocks[b] }

// Concrete is the concrete a concrete powder turns into on contact with water.
func (b Block) Concrete() (Block, bool) { return pairOf(concreteOf, b) }

// Scraped is the block one oxidation stage below b, which an axe scrapes it to.
func (b Block) Scraped() (Block, bool) { return pairOf(copperScraped, b) }

// Stripped is the axe's stripped block (vanilla: AxeItem.STRIPPABLES).
func (b Block) Stripped() (Block, bool) { return pairOf(axeStrippables, b) }

// Unwaxed is the wax-off block (vanilla: HoneycombItem.WAX_OFF_BY_BLOCK).
func (b Block) Unwaxed() (Block, bool) { return pairOf(copperWaxOff, b) }

// Waxed is the wax-on block (vanilla: HoneycombItem.WAXABLES).
func (b Block) Waxed() (Block, bool) { return pairOf(copperWaxed, b) }

// Flattened is the shovel's path block (vanilla: ShovelItem.FLATTENABLES).
func (b Block) Flattened() (Block, bool) { return pairOf(shovelFlattenables, b) }

// pairOf reads a blockPairs table, which stores the target plus one so that
// the zero value means no entry.
func pairOf(t *[BlockCount]Block, b Block) (Block, bool) {
	if b >= BlockCount || t[b] == 0 {
		return 0, false
	}
	return t[b] - 1, true
}
