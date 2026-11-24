package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The farm blocks: grass and mycelium dying and spreading, farmland wetting and
// drying, crops and stems ripening and bearing fruit, cane and cactus growing a
// block at a time, through RandomTick, updateAsNeighbor and the one-tick visits
// updateShape books. Torchflower and pitcher crops grow here too; the stems'
// bone meal and forced fruit are bonemeal.go's, sapling growth tree.go's.
//
// Left out and reported instead of written: the ground cover bone meal makes of
// a grass block (the placed features the item runs).

// snowyDirtTable marks the spreading dirt family (grass block, mycelium,
// podzol), whose snowy property follows the block above.
var snowyDirtTable = blocksNamed("minecraft:grass_block", "minecraft:mycelium", "minecraft:podzol")

// IsSnowyDirt reports whether the block's snowy property follows the block above.
func IsSnowyDirt(b registry.Block) bool { return snowyDirtTable[b] }

// grownCrops maps the four field crops to their age ceiling; mature states drop
// out of the random tick pass, as CropBlock.isRandomlyTicking keeps full-grown
// crops from ticking.
var grownCrops = map[registry.Block]int{
	registry.BlockWheat:     7,
	registry.BlockCarrots:   7,
	registry.BlockPotatoes:  7,
	registry.BlockBeetroots: 3,
}

// cropMaxAge is grownCrops by block, zero for a block that is no crop.
var cropMaxAge = func() *[registry.BlockCount]int8 {
	t := new([registry.BlockCount]int8)
	for b, age := range grownCrops {
		t[b] = int8(age)
	}
	return t
}()

// dirtState is what dying grass and dried or trampled farmland become.
var dirtState = registry.BlockStates["minecraft:dirt"]

// airBlocks marks the three air blocks (vanilla BlockState.isAir).
var airBlocks = registry.BlocksOf(func(b registry.Block) bool {
	return b == registry.BlockAir || b == registry.BlockCaveAir || b == registry.BlockVoidAir
})

// IsAirState reports whether a block state is air, cave air or void air.
// Unknown states read as air, like registry.BlockOf.
func IsAirState(state uint16) bool { return airBlocks[registry.BlockOf(state)] }

// IsKnownAirState reports whether a known block state is air, cave air or void air.
func IsKnownAirState(state uint16) bool {
	return int(state) < registry.StateCount && airBlocks[registry.BlockOf(state)]
}

// rawLightAt is getRawBrightness(pos, 0): the brighter of block and sky light
// without the hour's darkening, so a night sky over an open field still reads
// 15.
func rawLightAt(r BlockReader, x, y, z int32) int {
	// vanilla: SkyLightSectionStorage.getLightValue
	if region, ok := r.(*decorRegion); ok && region.g == nil {
		sky := region.featuresSkyLight(x, y, z)
		if replay := region.src.decor().lightReplay; sky == 0 && replay != nil {
			region.bindProtos(ChunkPos{X: x >> 4, Z: z >> 4})
			if region.lightAtLeast13(x, y, z, func(x, z int32) bool { return replay[ChunkPos{X: x >> 4, Z: z >> 4}].lit }) {
				return 15
			}
		}
		return sky
	}
	w, ok := r.(*World)
	if !ok {
		return 0
	}
	block := int(w.BlockLightAt(x, y, z))
	sky := int(w.SkyLightAt(x, y, z))
	if sky > block {
		block = sky
	}
	return block
}

// --- The spreading dirt family ---

// snowyDirtCanStayAlive is SpreadingSnowyBlock.canStayAlive, which ignores
// light: one snow layer above shelters it, a full cell of fluid drowns it, and
// anything else is judged by the light-dampening walk between the grass's cube
// and the block above (fully opaque reads 15, a leaf 1, glass 0); below 15 the
// grass lives.
func snowyDirtCanStayAlive(w *World, x, y, z int32) bool {
	above := w.BlockAt(x, y+1, z)
	if int(above) >= registry.StateCount {
		return true
	}
	if registry.BlockOf(above) == registry.BlockSnow {
		layers, ok := registry.StateInt(above, registry.PropLayers)
		return ok && layers == 1
	}
	if fs := fluidHolding(above); fs.amount >= 8 {
		return false
	}
	return !coveredFromAbove(w.BlockAt(x, y, z), above)
}

// coveredFromAbove reports whether the block above blocks light into the cell:
// its bottom face seals against the cell's top face, or it is fully opaque.
// vanilla: LightEngine.getLightDampeningInto
func coveredFromAbove(state, above uint16) bool {
	return registry.LightFacesSeal(state, registry.LightUp, above) || registry.LightDampening(above) >= 15
}

// nyliumTick reverts nylium to netherrack when covered from above.
// vanilla: NyliumBlock.randomTick
func (p *Physics) nyliumTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if coveredFromAbove(state, p.w.BlockAt(x, y+1, z)) {
		p.setShaped(x, y, z, registry.BlockNetherrack.DefaultState(), changes)
	}
}

// isSnowySetting reports whether the block above counts as snow cover: anything
// in the snow tag.
// vanilla: SpreadingSnowyBlock
func isSnowySetting(state uint16) bool {
	return stateInTag(state, registry.TagIDSnow)
}

// vanilla: SnowyBlock.updateShape
func snowyDirtShapeUpdate(state uint16, toward int, neighbor uint16) uint16 {
	if toward != dirUp {
		return state
	}
	next, _ := registry.WithBool(state, registry.PropSnowy, isSnowySetting(neighbor))
	return next
}

// spreadTick is grass or mycelium's random tick: a block that cannot stay alive
// becomes dirt; one that can, under a bright enough sky, makes four tries
// within one block sideways and up to three down, each turning dirt that could
// itself stay alive (with no water over it) into the spreading block, snowy per
// the sky above it.
// vanilla: SpreadingSnowyBlock.randomTick
func (p *Physics) spreadTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !snowyDirtCanStayAlive(p.w, x, y, z) {
		if dirt, ok := p.veto(HookFade, x, y, z, dirtState, [3]int32{}); ok {
			p.setShaped(x, y, z, dirt, changes)
		}
		return
	}
	if p.w.MaxLocalRawLightAt(x, y+1, z) < 9 {
		return
	}
	for range 4 {
		tx := x + int32(p.rng.Intn(3)) - 1
		ty := y + int32(p.rng.Intn(5)) - 3
		tz := z + int32(p.rng.Intn(3)) - 1
		if p.w.BlockAt(tx, ty, tz) != dirtState {
			continue
		}
		if !snowyDirtCanStayAlive(p.w, tx, ty, tz) {
			continue
		}
		if fs := fluidHolding(p.w.BlockAt(tx, ty+1, tz)); fs.kind == fluidWater {
			continue
		}
		spread, ok := registry.WithBool(registry.BlockOf(state).DefaultState(), registry.PropSnowy,
			isSnowySetting(p.w.BlockAt(tx, ty+1, tz)))
		if !ok {
			continue
		}
		if spread, ok = p.veto(HookSpread, tx, ty, tz, spread, [3]int32{x, y, z}); !ok {
			continue
		}
		p.setShaped(tx, ty, tz, spread, changes)
	}
}

// --- Farmland ---

// farmlandTickDelay, caneTickDelay and cactusTickDelay are the one game tick
// each updateShape's scheduleTick(pos, this, 1) books.
const (
	farmlandTickDelay = 1
	dirtPathTickDelay = 1
	caneTickDelay     = 1
	cactusTickDelay   = 1
)

// FarmlandSurvives reports whether farmland placed here would stand, which
// getStateForPlacement reads to place dirt instead.
// vanilla: FarmlandBlock.canSurvive
func (w *World) FarmlandSurvives(x, y, z int32) bool {
	return farmlandCanSurvive(w, x, y, z)
}

// farmlandCanSurvive: anything solid over the farmland kills it, unless it is a
// block the farmland maintains.
// vanilla: FarmlandBlock.canSurvive
func farmlandCanSurvive(w *World, x, y, z int32) bool {
	above := w.BlockAt(x, y+1, z)
	if collisionSolid(above) {
		return stateInTag(above, registry.TagIDMaintainsFarmland)
	}
	return true
}

// DirtPathSurvives reports whether a dirt path placed here would stand.
// vanilla: DirtPathBlock.canSurvive, getStateForPlacement
func (w *World) DirtPathSurvives(x, y, z int32) bool {
	return dirtPathCanSurvive(w, x, y, z)
}

// dirtPathCanSurvive: a solid block over the path turns it to dirt, unless it
// is a fence gate.
// vanilla: DirtPathBlock.canSurvive
func dirtPathCanSurvive(w *World, x, y, z int32) bool {
	above := w.BlockAt(x, y+1, z)
	if !collisionSolid(above) {
		return true
	}
	return registry.BlockOf(above).InGroup(registry.GroupFenceGate)
}

// farmlandSurvivalScheduled is the one-tick visit FarmlandBlock.updateShape
// books when the block above turned solid and maintains nothing: it becomes
// dirt.
func (p *Physics) farmlandSurvivalScheduled(x, y, z int32, changes *[]BlockChange) {
	if farmlandCanSurvive(p.w, x, y, z) {
		return
	}
	p.turnFarmlandToDirt(x, y, z, changes)
}

// farmlandTick: water within the 9x2x9 field, or rain on the block, wets it
// fully; otherwise it dries a step, and dry farmland nothing maintains becomes
// dirt.
// vanilla: FarmlandBlock.randomTick
func (p *Physics) farmlandTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	moisture, ok := farmlandMoistureOf(state)
	if !ok {
		return
	}
	if p.farmlandNearWater(x, y, z) || p.w.RainingAt(x, y+1, z) {
		if moisture < 7 {
			p.setFarmlandMoisture(x, y, z, 7, changes)
		}
		return
	}
	if moisture > 0 {
		p.setFarmlandMoisture(x, y, z, moisture-1, changes)
		return
	}
	if !farmlandAboveMaintains(p.w, x, y, z) {
		if _, ok := p.veto(HookFade, x, y, z, dirtState, [3]int32{}); ok {
			p.turnFarmlandToDirt(x, y, z, changes)
		}
	}
}

// farmlandAboveMaintains reports whether the block above is in
// minecraft:maintains_farmland. The random tick's death goes by this alone, air
// included, which is what dries uncovered farmland to dirt; canSurvive treats
// open sky as surviving.
// vanilla: FarmlandBlock.shouldMaintainFarmland
func farmlandAboveMaintains(w *World, x, y, z int32) bool {
	return stateInTag(w.BlockAt(x, y+1, z), registry.TagIDMaintainsFarmland)
}

// farmlandNearWater reports any water (source, flow or waterlogged) within four
// blocks on the level and one above.
// vanilla: FarmlandBlock.isNearWater
func (p *Physics) farmlandNearWater(x, y, z int32) bool {
	for dy := int32(0); dy <= 1; dy++ {
		for dx := int32(-4); dx <= 4; dx++ {
			for dz := int32(-4); dz <= 4; dz++ {
				if fs := fluidHolding(p.w.BlockAt(x+dx, y+dy, z+dz)); fs.kind == fluidWater {
					return true
				}
			}
		}
	}
	return false
}

// farmlandMoistureOf reads moisture, 0-7, 7 being fully hydrated.
func farmlandMoistureOf(state uint16) (int, bool) {
	if int(state) >= registry.StateCount || registry.BlockOf(state) != registry.BlockFarmland {
		return 0, false
	}
	return registry.StateInt(state, registry.PropMoisture)
}

// setFarmlandMoisture writes moisture with flag 2: clients told, no neighbour
// woken, the shape pass still telling an observer.
func (p *Physics) setFarmlandMoisture(x, y, z int32, moisture int, changes *[]BlockChange) {
	if next, ok := registry.WithInt(p.w.BlockAt(x, y, z), registry.PropMoisture, moisture); ok {
		if next, ok = p.veto(HookMoisture, x, y, z, next, [3]int32{}); ok {
			p.setClientShaped(x, y, z, next, changes)
		}
	}
}

// turnFarmlandToDirt writes dirt with flags 3 (neighbours wake, observers see
// the shape pass), carrying the rise of the full block over farmland's low top
// so the server pushes entities up (Block.pushEntitiesUp).
// vanilla: FarmlandBlock.turnToDirt
func (p *Physics) turnFarmlandToDirt(x, y, z int32, changes *[]BlockChange) {
	old := p.w.BlockAt(x, y, z)
	n := len(*changes)
	p.set(x, y, z, dirtState, changes)
	if len(*changes) > n {
		(*changes)[n].PushUp = CollisionTop(dirtState) - CollisionTop(old)
		p.shapeUpdateNeighbors(x, y, z, changes)
	}
}

// farmlandFallenOn rolls the level's dice once for a body landing on farmland;
// a roll under how far past half a block it fell turns it to dirt. Vanilla
// rolls whatever lands; callers apply the entity gates first (living, player
// permission or mob griefing, size). The change is returned for the server to
// broadcast, and the engine's dice keep seeded tests stable.
// vanilla: FarmlandBlock.fallOn
func (p *Physics) FarmlandFallenOn(x, y, z int32, fallDistance float64) []BlockChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	if registry.BlockOf(p.w.BlockAt(x, y, z)) != registry.BlockFarmland {
		return nil
	}
	if !(p.rng.Float64() < fallDistance-0.5) {
		return nil
	}
	var changes []BlockChange
	p.turnFarmlandToDirt(x, y, z, &changes)
	return changes
}

// --- Crops ---

// cropCanSurvive: on farmland, with raw light 8 or more. A pitcher crop's upper
// half is DoublePlantBlock.canSurvive's, which only needs the lower half.
// vanilla: CropBlock.canSurvive
func cropCanSurvive(w BlockReader, x, y, z int32) bool {
	if rawLightAt(w, x, y, z) < 8 {
		return false
	}
	return farmlandBelow(w, x, y, z)
}

func farmlandBelow(w BlockReader, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y-1, z)) == registry.BlockFarmland
}

// cropTick: in light 9 or more, an unripe crop rolls a growth step, likelier
// the richer the 3x3 ground, written with flag 2 (observers still see it).
// Beetroot first rolls its own die, skipping one random tick in three.
// vanilla: CropBlock.randomTick
func (p *Physics) cropTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	b := registry.BlockOf(state)
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= int(cropMaxAge[b]) {
		return
	}
	if b == registry.BlockBeetroots && p.rng.Intn(3) == 0 {
		return
	}
	p.cropGrowthStep(x, y, z, state, age, changes)
}

// cropGrowthStep rolls a crop's or stem's light-gated growth die and on success
// writes one age older with flag 2.
func (p *Physics) cropGrowthStep(x, y, z int32, state uint16, age int, changes *[]BlockChange) {
	if !p.cropGrowthRoll(x, y, z, registry.BlockOf(state)) {
		return
	}
	if next, ok := registry.WithInt(state, registry.PropAge, age+1); ok {
		if next, ok = p.veto(HookGrow, x, y, z, next, [3]int32{}); !ok {
			return
		}
		p.setClientShaped(x, y, z, next, changes)
	}
}

// cropGrowthRoll is CropBlock.randomTick's gate: light 9, then a
// one-in-(25/speed+1) roll.
func (p *Physics) cropGrowthRoll(x, y, z int32, crop registry.Block) bool {
	if rawLightAt(p.w, x, y, z) < 9 {
		return false
	}
	speed := p.cropGrowthSpeed(x, y, z, crop)
	return p.rng.Intn(int(float32(25.0)/speed)+1) == 0
}

// cropGrowthSpeed: 1, plus per 3x3 ground cell 3 for wet farmland or 1 for dry,
// quartered for the eight off-centre cells; halved when the same crop grows on
// both an X and a Z side, or on any diagonal.
// vanilla: CropBlock.getGrowthSpeed
func (p *Physics) cropGrowthSpeed(x, y, z int32, crop registry.Block) float32 {
	speed := float32(1)
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			bonus := float32(0)
			ground := p.w.BlockAt(x+dx, y-1, z+dz)
			if stateInTag(ground, registry.TagIDGrowsCrops) {
				bonus = 1
				if moisture, ok := registry.StateInt(ground, registry.PropMoisture); ok && moisture > 0 {
					bonus = 3
				}
			}
			if dx != 0 || dz != 0 {
				bonus /= 4
			}
			speed += bonus
		}
	}
	isCrop := func(dx, dz int32) bool {
		s := p.w.BlockAt(x+dx, y, z+dz)
		return int(s) < registry.StateCount && registry.BlockOf(s) == crop
	}
	switch {
	case (isCrop(-1, 0) || isCrop(1, 0)) && (isCrop(0, -1) || isCrop(0, 1)):
		speed /= 2
	case isCrop(-1, -1) || isCrop(1, -1) || isCrop(1, 1) || isCrop(-1, 1):
		speed /= 2
	}
	return speed
}

// torchflowerCropTick skips a third of visits, otherwise grows like a crop; the
// step past age 1 is the torchflower itself.
// vanilla: TorchflowerCropBlock.randomTick, getStateForAge
func (p *Physics) torchflowerCropTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || p.rng.Intn(3) == 0 {
		return
	}
	if !p.cropGrowthRoll(x, y, z, registry.BlockTorchflowerCrop) {
		return
	}
	next := registry.BlockTorchflower.DefaultState()
	if age+1 < 2 {
		var found bool
		if next, found = registry.WithInt(registry.BlockTorchflowerCrop.DefaultState(), registry.PropAge, 1); !found {
			return
		}
	}
	if next, ok = p.veto(HookGrow, x, y, z, next, [3]int32{}); ok {
		p.setClientShaped(x, y, z, next, changes)
	}
}

// pitcherCropTick grows a pitcher crop's lower half with no light roll of its
// own; growth itself needs light 8.
// vanilla: PitcherCropBlock.randomTick
func (p *Physics) pitcherCropTick(x, y, z int32, changes *[]BlockChange) {
	state := p.w.BlockAt(x, y, z)
	if _, ok := registry.StateInt(state, registry.PropAge); !ok {
		return
	}
	if p.rng.Intn(int(float32(25.0)/p.cropGrowthSpeed(x, y, z, registry.BlockPitcherCrop))+1) != 0 {
		return
	}
	p.growPitcherCrop(x, y, z, state, changes)
}

// stemTick grows a melon or pumpkin stem below age 7; at 7 it hands to
// stemFruit, whose own light gate and roll keep the visit from being thrown
// twice.
// vanilla: StemBlock.randomTick
func (p *Physics) stemTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return
	}
	if age >= 7 {
		p.stemFruit(x, y, z, registry.BlockOf(state), changes)
		return
	}
	p.cropGrowthStep(x, y, z, state, age, changes)
}

// attachedStemUpdateShape turns an attached stem whose fruit is gone back to a
// plain stem at age 7, which can bear again.
// vanilla: AttachedStemBlock.updateShape
func (p *Physics) attachedStemUpdateShape(x, y, z int32, state uint16, from [3]int32, changed uint16, changes *[]BlockChange) {
	if directionOf(from) != registry.StateFacing(state) {
		return
	}
	fruit, stem := registry.BlockPumpkin, registry.BlockPumpkinStem
	if registry.BlockOf(state) == registry.BlockAttachedMelonStem {
		fruit, stem = registry.BlockMelon, registry.BlockMelonStem
	}
	if registry.BlockOf(changed) == fruit {
		return
	}
	if next, ok := registry.WithInt(stem.DefaultState(), registry.PropAge, 7); ok {
		p.setShaped(x, y, z, next, changes)
	}
}

// --- Sugar cane ---

// caneCanSurvive: on cane, or on supports_sugar_cane ground with water (source,
// flow or held) beside it, or frosted ice where the adjacent tag allows.
// vanilla: SugarCaneBlock.canSurvive
func caneCanSurvive(w BlockReader, x, y, z int32) bool {
	below := w.BlockAt(x, y-1, z)
	if registry.BlockOf(below) == registry.BlockSugarCane {
		return true
	}
	if !stateInTag(below, registry.TagIDSupportsSugarCane) {
		return false
	}
	for _, d := range horizontal4 {
		near := w.BlockAt(x+d[0], y-1, z+d[1])
		if fs := fluidHolding(near); fs.kind == fluidWater {
			return true
		}
		if stateInTag(near, registry.TagIDSupportsSugarCaneAdjacently) {
			return true
		}
	}
	return false
}

// caneTick grows cane into air at the top of a stack shorter than three, one
// block per 15 ages. The new cane is a flags-3 setBlockAndUpdate; age writes
// are flag 260 (no client or neighbour told), and either write's shape pass
// reaches observers.
// vanilla: SugarCaneBlock.randomTick
func (p *Physics) caneTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !p.airAt(x, y+1, z) {
		return
	}
	height := int32(1)
	for p.blockIs(x, y-height, z, registry.BlockSugarCane) {
		height++
	}
	if height >= 3 {
		return
	}
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return
	}
	if age == 15 {
		defaultState := registry.BlockSugarCane.DefaultState()
		grown, ok := p.veto(HookGrow, x, y+1, z, defaultState, [3]int32{x, y, z})
		if !ok {
			return // the age stays 15 and the next roll asks again
		}
		p.setShaped(x, y+1, z, grown, changes)
		p.writeBlockShaped(x, y, z, defaultState, changes)
		return
	}
	if next, found := registry.WithInt(state, registry.PropAge, age+1); found {
		p.writeBlockShaped(x, y, z, next, changes)
	}
}

// caneSurvivalScheduled is the one-tick visit SugarCaneBlock.updateShape books
// when the cane can no longer survive: it breaks with its loot.
// vanilla: destroyBlock
func (p *Physics) caneSurvivalScheduled(x, y, z int32, changes *[]BlockChange) {
	if caneCanSurvive(p.w, x, y, z) {
		return
	}
	state := p.w.BlockAt(x, y, z)
	p.setShaped(x, y, z, airState, changes)
	p.effects = append(p.effects, destroyBlockEffect(state, nil, x, y, z))
}

// --- Cactus ---

// cactusCanSurvive: nothing solid or lava beside it, sand-like or cactus below,
// and no liquid block above (a waterlogged block holds water but does not
// count).
// vanilla: CactusBlock.canSurvive
func cactusCanSurvive(w BlockReader, x, y, z int32) bool {
	for _, d := range horizontal4 {
		near := w.BlockAt(x+d[0], y, z+d[1])
		if collisionSolid(near) {
			return false
		}
		if fs := fluidHolding(near); fs.kind == fluidLava {
			return false
		}
	}
	below := w.BlockAt(x, y-1, z)
	if registry.BlockOf(below) != registry.BlockCactus && !stateInTag(below, registry.TagIDSupportsCactus) {
		return false
	}
	if liquidBlock(w.BlockAt(x, y+1, z)) {
		return false
	}
	return true
}

// cactusTick grows into air at the top of its stack: at age 8 it rolls for a
// flower (likelier on a full three-tall stack), at age 15 it plants one up and
// resets, unless the stack is already three. New blocks and flowers are flags-3
// writes, age writes flag 260; either's shape pass reaches observers.
// vanilla: CactusBlock.randomTick
func (p *Physics) cactusTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if !p.airAt(x, y+1, z) {
		return
	}
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return
	}
	height := int32(1)
	for p.blockIs(x, y-height, z, registry.BlockCactus) {
		height++
		if height == 3 && age == 15 {
			return
		}
	}
	switch age {
	case 8:
		if cactusCanSurvive(p.w, x, y+1, z) {
			chance := 0.1
			if height >= 3 {
				chance = 0.25
			}
			if p.rng.Float64() <= chance {
				if flower, ok := p.veto(HookGrow, x, y+1, z, registry.BlockCactusFlower.DefaultState(), [3]int32{x, y, z}); ok {
					p.setShaped(x, y+1, z, flower, changes)
				}
			}
		}
	case 15:
		if height < 3 {
			defaultState := registry.BlockCactus.DefaultState()
			grown, ok := p.veto(HookGrow, x, y+1, z, defaultState, [3]int32{x, y, z})
			if !ok {
				return
			}
			p.setShaped(x, y+1, z, grown, changes)
			p.writeBlockShaped(x, y, z, defaultState, changes)
		}
		return
	}
	if age < 15 {
		if next, found := registry.WithInt(state, registry.PropAge, age+1); found {
			p.writeBlockShaped(x, y, z, next, changes)
		}
	}
}

// cactusSurvivalScheduled is the one-tick visit CactusBlock.updateShape books
// when the cactus can no longer survive: it breaks with its loot.
func (p *Physics) cactusSurvivalScheduled(x, y, z int32, changes *[]BlockChange) {
	if cactusCanSurvive(p.w, x, y, z) {
		return
	}
	state := p.w.BlockAt(x, y, z)
	p.setShaped(x, y, z, airState, changes)
	p.effects = append(p.effects, destroyBlockEffect(state, nil, x, y, z))
}

// --- Placement ---

// PlacementSurvives is the canSurvive BlockItem.canPlace runs: the farm
// families' and soul fire's rules, else support.go's table.
func (w *World) PlacementSurvives(state uint16, x, y, z int32) bool {
	return placementSurvives(w, state, x, y, z)
}

func placementSurvives(w BlockReader, state uint16, x, y, z int32) bool {
	switch registry.BlockOf(state) {
	case registry.BlockSugarCane:
		return caneCanSurvive(w, x, y, z)
	case registry.BlockCactus:
		return cactusCanSurvive(w, x, y, z)
	case registry.BlockSoulFire:
		return blocklogic.SoulFireBase(w.BlockAt(x, y-1, z))
	}
	return canSurvive(w, x, y, z, state)
}

// --- Small reads ---

// statePropsOf is a state's property string, empty for unnamed states.
func statePropsOf(state uint16) string {
	_, props, ok := registry.BlockStateName(state)
	if !ok {
		return ""
	}
	return props
}

// registryStateName is a state's block name, empty for unnamed states.
func registryStateName(state uint16) string {
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		return ""
	}
	return name
}
