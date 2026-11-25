package world

import (
	"math/bits"
	"slices"

	"github.com/golem-mc/golem/registry"
)

// The random tick pass (chunk ticking): each game tick, every ticking chunk
// throws random_tick_speed dice at each section holding a random-ticking block,
// and the block each lands on gets its random tick (leaf decay, grass,
// farmland, crops, cane, lava's fire, ice melting, copper weathering and the
// rest).
//
// First comes iceandsnow: one die in 48 hands a column to tickPrecipitation
// (snowfall.go).
//
// The shape is ServerLevel.tickChunk's: sections earn dice when their
// random-ticking count is above zero (vanilla counts blocks and fluids
// separately and takes the union; sections here keep one count). Positions are
// Level.getBlockRandomPos's, and a roll landing on a non-ticking block is
// spent, not re-rolled. The caller picks the chunks: every one within
// simulation distance of a player.

// The dice per section per tick are the random_tick_speed rule, handed to
// RandomTick each pass with max_snow_accumulation_height; the server reads both
// from the overworld's rules table.

// randomlyTickingStates marks the states the pass acts on, indexed by state ID
// because section.set reads it for every generated block. Each family follows
// its own isRandomlyTicking: non-persistent leaves at distance 7; spreading
// dirt, nylium and farmland in every state; cane and cactus at every age; crops below
// their last age (CropBlock.isRandomlyTicking), but melon and pumpkin stems at
// every age, 7 included, since that tick bears the fruit; saplings in both
// stages and the mangrove propagule (a SaplingBlock overriding randomTick); ice
// for the melt (frosted ice never random-ticks, only its scheduled tick); chorus
// flowers below age 5; nether wart, cocoa and berry bush below 3, 2 and 3
// (ripening.go); weathering copper with a next stage, while waxed copper never
// ticks and is outside every scan (oxidation.go); dried ghast in every state,
// since randomTicks() is per block, its pass gating itself (driedghast.go);
// turtle egg, pointed dripstone, sulfur spike, nether portal and snow layer in every state;
// redstone ore while lit; eyeblossoms, potted or not (eyeblossom.go); bamboo
// sapling, and bamboo stalk at stage 0; lava at every level, for
// LavaFluid.randomTick's fire dice, the only fluid vanilla random-ticks.
// vanilla: BambooSaplingBlock, BambooStalkBlock.isRandomlyTicking
var randomlyTickingStates = func() *[1 << 16]bool {
	m := new([1 << 16]bool)
	for s := range uint16(registry.StateCount) {
		m[s] = ticksInState(registry.BlockOf(s), s)
	}
	return m
}()

// ticksInState is the per-state half of isRandomlyTicking for a block
// randomTickKinds names: the age, stage and lit gates above.
func ticksInState(b registry.Block, s uint16) bool {
	age, _ := registry.StateInt(s, registry.PropAge)
	switch randomTickKinds[b] {
	case rtNone:
		return false
	case rtLeaves:
		distance, _ := registry.StateInt(s, registry.PropDistance)
		return distance == 7 && !registry.StateBool(s, registry.PropPersistent)
	case rtCrop:
		return age < int(cropMaxAge[b])
	case rtNetherWart:
		return age < netherWartMaxAge
	case rtCocoa:
		return age < cocoaMaxAge
	case rtBerryBush:
		return age < berryBushMaxAge
	case rtPitcherCrop:
		// vanilla: PitcherCropBlock.isRandomlyTicking, the lower half below age 4.
		return registry.StateValue(s, registry.PropHalf) == "lower" && age < 4
	case rtBamboo:
		// vanilla: BambooStalkBlock.isRandomlyTicking, stage 0 only.
		stage, _ := registry.StateInt(s, registry.PropStage)
		return stage == 0
	case rtChorusFlower:
		return age < chorusAgeMax
	case rtPlantHead:
		// vanilla: GrowingPlantHeadBlock.isRandomlyTicking, below age 25.
		return age < plantHeadMaxAge
	case rtRedstoneOre:
		return registry.StateBool(s, registry.PropLit)
	}
	return true
}

// randomTickKind is the random tick a block runs when a die lands on it.
type randomTickKind uint8

const (
	rtNone randomTickKind = iota
	rtLeaves
	rtSpreadingDirt
	rtNylium
	rtFarmland
	rtSugarCane
	rtCactus
	rtLava
	rtMangrovePropagule
	rtBambooSapling
	rtBamboo
	rtSapling
	rtIce
	rtSnowLayer
	rtChorusFlower
	rtNetherWart
	rtCocoa
	rtBerryBush
	rtPlantHead
	rtMushroom
	rtVine
	rtBuddingAmethyst
	rtRedstoneOre
	rtTurtleEgg
	rtNetherPortal
	rtDriedGhast
	rtPointedDripstone
	rtSulfurSpike
	rtEyeblossom
	rtPottedEyeblossom
	rtCopper
	rtCrop
	rtTorchflowerCrop
	rtPitcherCrop
	rtStem
)

// randomTickKinds is each block's random tick, rtNone for none. Leaves use the
// built-in minecraft:leaves tag for vanilla's LeavesBlock class, so a data
// pack's leaves tag makes no block rot.
var randomTickKinds = func() *[registry.BlockCount]randomTickKind {
	t := new([registry.BlockCount]randomTickKind)
	for _, name := range registry.BlockTags["minecraft:leaves"] {
		if b, ok := registry.BlockByName(name); ok {
			t[b] = rtLeaves
		}
	}
	for b, k := range map[registry.Block]randomTickKind{
		registry.BlockGrassBlock:             rtSpreadingDirt,
		registry.BlockMycelium:               rtSpreadingDirt,
		registry.BlockCrimsonNylium:          rtNylium,
		registry.BlockWarpedNylium:           rtNylium,
		registry.BlockFarmland:               rtFarmland,
		registry.BlockSugarCane:              rtSugarCane,
		registry.BlockCactus:                 rtCactus,
		registry.BlockLava:                   rtLava,
		registry.BlockMangrovePropagule:      rtMangrovePropagule,
		registry.BlockBambooSapling:          rtBambooSapling,
		registry.BlockBamboo:                 rtBamboo,
		registry.BlockIce:                    rtIce,
		registry.BlockSnow:                   rtSnowLayer,
		registry.BlockChorusFlower:           rtChorusFlower,
		registry.BlockNetherWart:             rtNetherWart,
		registry.BlockCocoa:                  rtCocoa,
		registry.BlockSweetBerryBush:         rtBerryBush,
		registry.BlockBrownMushroom:          rtMushroom,
		registry.BlockRedMushroom:            rtMushroom,
		registry.BlockVine:                   rtVine,
		registry.BlockBuddingAmethyst:        rtBuddingAmethyst,
		registry.BlockRedstoneOre:            rtRedstoneOre,
		registry.BlockDeepslateRedstoneOre:   rtRedstoneOre,
		registry.BlockTurtleEgg:              rtTurtleEgg,
		registry.BlockNetherPortal:           rtNetherPortal,
		registry.BlockDriedGhast:             rtDriedGhast,
		registry.BlockPointedDripstone:       rtPointedDripstone,
		registry.BlockSulfurSpike:            rtSulfurSpike,
		registry.BlockClosedEyeblossom:       rtEyeblossom,
		registry.BlockOpenEyeblossom:         rtEyeblossom,
		registry.BlockPottedClosedEyeblossom: rtPottedEyeblossom,
		registry.BlockPottedOpenEyeblossom:   rtPottedEyeblossom,
		registry.BlockTorchflowerCrop:        rtTorchflowerCrop,
		registry.BlockPitcherCrop:            rtPitcherCrop,
		registry.BlockMelonStem:              rtStem,
		registry.BlockPumpkinStem:            rtStem,
	} {
		t[b] = k
	}
	for b := range registry.Block(registry.BlockCount) {
		switch {
		case saplingBlocks[b]:
			t[b] = rtSapling
		case plantHeadBlocks[b]:
			t[b] = rtPlantHead
		case copperNext[b] != registry.BlockAir:
			t[b] = rtCopper
		case cropMaxAge[b] > 0:
			t[b] = rtCrop
		}
	}
	return t
}()

// randomlyTicking reports a state the pass acts on: what a rolled position is
// tested against and what a section's count is built from.
func randomlyTicking(state uint16) bool {
	return randomlyTickingStates[state]
}

// packChunkKey orders as (X ascending, then Z ascending) on the signed
// coordinates.
func packChunkKey(c ChunkPos) uint64 {
	return uint64(uint32(c.X)^0x80000000)<<32 | uint64(uint32(c.Z)^0x80000000)
}

func unpackChunkKey(k uint64) ChunkPos {
	return ChunkPos{X: int32(uint32(k>>32) ^ 0x80000000), Z: int32(uint32(k) ^ 0x80000000)}
}

// RandomTick runs one pass over the server's ticking chunks: a column's sky
// work per 48 dice, then speed dice per random-ticking section, returning the
// changed blocks and decays handed over. speed is random_tick_speed and
// maxSnowHeight max_snow_accumulation_height. The dice are the engine's single
// source (a vanilla level's one random), so the same world and seed replay.
func (p *Physics) RandomTick(gameTime int64, chunks []ChunkPos, speed, maxSnowHeight int) ([]BlockChange, []Decay) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = gameTime
	if speed <= 0 {
		return nil, nil
	}

	// The chunks come from a map, whose order Go randomizes, and one dice
	// stream serves every chunk: sort them so the same world and seed replay.
	// Packed with the sign bit flipped, the keys sort as (X, Z) does.
	keys := p.randomTickKeys[:0]
	for _, c := range chunks {
		keys = append(keys, packChunkKey(c))
	}
	slices.Sort(keys)
	p.randomTickKeys = keys

	var changes []BlockChange
	var decays []Decay
	// The world's read lock is held through the loop, released only around a
	// precipitation or landed-die visit, which may write.
	p.w.mu.RLock()
	for _, key := range keys {
		pos := unpackChunkKey(key)
		// The sky's work first (iceandsnow): one die in 48 hands the column to
		// freezing, snowfall and cauldron filling.
		for range speed {
			if p.rng.Intn(precipitationDie) != 0 {
				continue
			}
			x, _, z := p.blockRandomPos(pos.X*sectionSize, 0, pos.Z*sectionSize)
			p.w.mu.RUnlock()
			p.tickPrecipitation(x, z, int32(maxSnowHeight), &changes)
			p.w.mu.RLock()
		}
		// Keep the selection through visits that may write or unload the chunk.
		// A chunk gone in a gap of the lock reads as air for its remaining dice.
		var sections uint32
		var c *Chunk
		if loaded, ok := p.w.loadedAt(pos); ok {
			c = loaded.chunk
			sections = c.randomTickSections
		}
		for ; sections != 0; sections &= sections - 1 {
			si := bits.TrailingZeros32(sections)
			baseY := worldMinY + int32(si)*sectionSize
			// A view is valid only until the read lock is dropped for a visit.
			// mask is nil for a section with every cell ticking (all) or
			// none, or whose mask is not built.
			read := func() (blocks *blockWords, fill uint16, mask *randomTickMask, all bool) {
				if c == nil {
					return nil, airState, nil, false
				}
				sec := &c.sections[si]
				blocks = sec.blocks.Load()
				return blocks, sec.fill, sec.tickCells(blocks), sec.ticking >= blocksPerSection
			}
			blocks, fill, mask, all := read()
			for range speed {
				x, y, z := p.blockRandomPos(pos.X*sectionSize, baseY, pos.Z*sectionSize)
				i := int((y&15)*256 + (z&15)*16 + x&15)
				if mask != nil && !mask.ticks(i) {
					continue // the die landed on nothing that ticks
				}
				state := fill
				if blocks != nil {
					state = blocks.get(i)
				}
				if !all && !randomlyTicking(state) {
					continue // the die landed on nothing that ticks
				}
				p.w.mu.RUnlock()
				p.randomVisit(x, y, z, state, &changes, &decays)
				p.w.mu.RLock()
				c = nil
				if loaded, ok := p.w.loadedAt(pos); ok {
					c = loaded.chunk
				}
				blocks, fill, mask, all = read()
			}
		}
	}
	p.w.mu.RUnlock()
	return changes, decays
}

// randomVisit runs the random tick of the block a die landed on.
func (p *Physics) randomVisit(x, y, z int32, state uint16, changes *[]BlockChange, decays *[]Decay) {
	switch randomTickKinds[registry.BlockOf(state)] {
	case rtLeaves:
		p.leafDecay(x, y, z, state, changes, decays)
	case rtSpreadingDirt:
		p.spreadTick(x, y, z, state, changes)
	case rtNylium:
		p.nyliumTick(x, y, z, state, changes)
	case rtFarmland:
		p.farmlandTick(x, y, z, state, changes)
	case rtSugarCane:
		p.caneTick(x, y, z, state, changes)
	case rtCactus:
		p.cactusTick(x, y, z, state, changes)
	case rtLava:
		// Lava runs twice per landed die: the chunk pass runs the fluid's
		// random tick beside the block's, and LiquidBlock's delegates to the
		// fluid too, so the second throw follows the first's rolls.
		p.lavaTick(x, y, z, changes)
		p.lavaTick(x, y, z, changes)
	case rtMangrovePropagule:
		p.mangrovePropaguleTick(x, y, z, state, changes)
	case rtBambooSapling:
		p.bambooSaplingTick(x, y, z, changes)
	case rtBamboo:
		p.bambooStalkTick(x, y, z, state, changes)
	case rtSapling:
		p.saplingTick(x, y, z, state, changes)
	case rtIce:
		p.iceRandomTick(x, y, z, state, changes)
	case rtSnowLayer:
		p.snowLayerRandomTick(x, y, z, changes)
	case rtChorusFlower:
		p.chorusFlowerTick(x, y, z, state, changes)
	case rtNetherWart:
		p.netherWartTick(x, y, z, state, changes)
	case rtCocoa:
		p.cocoaTick(x, y, z, state, changes)
	case rtBerryBush:
		p.berryBushTick(x, y, z, state, changes)
	case rtPlantHead:
		p.plantHeadTick(x, y, z, state, changes)
	case rtMushroom:
		p.mushroomTick(x, y, z, state, changes)
	case rtVine:
		p.vineRandomTick(x, y, z, state, changes)
	case rtBuddingAmethyst:
		p.buddingAmethystTick(x, y, z, changes)
	case rtRedstoneOre:
		p.redstoneOreRandomTick(x, y, z, state, changes)
	case rtTurtleEgg:
		p.turtleEggRandomTick(x, y, z, state, changes)
	case rtNetherPortal:
		p.portalTicks = append(p.portalTicks, PortalTick{X: x, Y: y, Z: z})
	case rtDriedGhast:
		p.driedGhastRandomTick(x, y, z, state)
	case rtPointedDripstone:
		p.pointedDripstoneRandomTick(x, y, z, state, changes)
	case rtSulfurSpike:
		p.speleothemRandomTick(x, y, z, state, changes)
	case rtEyeblossom:
		p.eyeblossomRandomTick(x, y, z, state, changes)
	case rtPottedEyeblossom:
		p.pottedEyeblossomRandomTick(x, y, z, state, changes)
	case rtCopper:
		p.copperOxidationTick(x, y, z, state, changes)
	case rtCrop:
		p.cropTick(x, y, z, state, changes)
	case rtTorchflowerCrop:
		p.torchflowerCropTick(x, y, z, state, changes)
	case rtPitcherCrop:
		p.pitcherCropTick(x, y, z, changes)
	case rtStem:
		p.stemTick(x, y, z, state, changes)
	}
}

// blockRandomPos is Level.getBlockRandomPos: one step of the level's cheap
// counter split into section offsets, x from the low four bits, y the next
// four, z the next.
func (p *Physics) blockRandomPos(x, y, z int32) (int32, int32, int32) {
	r := int32(p.rng.Uint32()) >> 2
	return x + r&15, y + (r>>4)&15, z + (r>>8)&15
}

// PortalTick is a random tick on a nether portal. The engine owns only the
// visit; the piglin roll needs difficulty, players and mobs, so the server runs
// it.
// vanilla: NetherPortalBlock.randomTick
type PortalTick struct {
	X, Y, Z int32
}

// TakePortalTicks hands the portal visits over and empties them.
func (p *Physics) TakePortalTicks() []PortalTick {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.portalTicks
	p.portalTicks = nil
	return out
}
