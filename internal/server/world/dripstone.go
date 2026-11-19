package world

import (
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

const (
	// dripstoneWaterChance and dripstoneLavaChance are the transfer chances;
	// the random tick rolls one float against the fluid above.
	// vanilla: PointedDripstoneBlock.maybeTransferFluid
	dripstoneWaterChance float32 = 0.17578125
	dripstoneLavaChance  float32 = 0.05859375

	// dripstoneSearch is vanilla's maxSteps for every column search: blocks 1
	// to 10 away are read.
	// vanilla: SpeleothemBlock.findBlockVertical
	dripstoneSearch = 11

	// dripstoneGrowthChance is the growth die's threshold.
	// vanilla: SpeleothemBlock.randomTick
	dripstoneGrowthChance float32 = 0.011377778

	// cauldronDripDelay is the ticks a drip takes before the cauldron's tick,
	// plus one per block of fall.
	// vanilla: PointedDripstoneBlock.maybeTransferFluid
	cauldronDripDelay = 50

	levelEventDripstoneDrip = 1504
	levelEventCauldronWater = 1047
	levelEventCauldronLava  = 1046
	gameEventBlockChange    = registry.GameEventBlockChange
	gameEventBlockDestroy   = registry.GameEventBlockDestroy

	// cauldronDripEvent tags the queue entry a drip books on its cauldron.
	cauldronDripEvent = 1<<5 | 1<<6 | 1<<7
)

var (
	clayState        = registry.MustState("minecraft:clay", nil)
	dripLavaCauldron = registry.MustState("minecraft:lava_cauldron", nil)
)

// speleothemGrowth holds each block's support and tip-search limit.
// vanilla: SpeleothemBlock.getMaxGrowthLength, SulfurSpikeBlock.getMaxGrowthLength
var speleothemGrowth = [registry.BlockCount]struct {
	growOn    registry.Block
	maxLength int32
}{
	registry.BlockPointedDripstone: {registry.BlockDripstoneBlock, 7},
	registry.BlockSulfurSpike:      {registry.BlockSulfur, 2},
}

// dripstoneSource is what sits above a stalactite's root: the fluid, and
// whether it is the water a mud block is taken to hold.
type dripstoneSource struct {
	x, y, z int32
	kind    fluidKind
	mud     bool
}

// pointedDripstoneRandomTick is the die that landed on a pointed dripstone.
// vanilla: PointedDripstoneBlock.randomTick
func (p *Physics) pointedDripstoneRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	p.dripstoneTransfer(x, y, z, state, p.rng.Float32(), changes)
	p.speleothemRandomTick(x, y, z, state, changes)
}

// speleothemRandomTick rolls the growth die and grows a stalactite whose start
// it lands on. The direction die is spent only after every check passes.
// vanilla: SpeleothemBlock.randomTick
func (p *Physics) speleothemRandomTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	if p.rng.Float32() >= dripstoneGrowthChance || !p.stalactiteStart(x, y, z, state) {
		return
	}
	p.growStalactiteOrStalagmite(x, y, z, registry.BlockOf(state), changes)
}

// RandomTickDripstone runs a pointed dripstone's random tick on dice seeded
// with seed, so a test can pin every draw, and returns the changes.
// vanilla: PointedDripstoneBlock.randomTick
func (p *Physics) RandomTickDripstone(gameTime, seed int64, x, y, z int32) []BlockChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = gameTime
	p.rng = rand.New(rand.NewSource(seed))
	var changes []BlockChange
	p.pointedDripstoneRandomTick(x, y, z, p.w.BlockAt(x, y, z), &changes)
	return changes
}

// growStalactiteOrStalagmite grows the tip down, or a stalagmite on the floor
// below, by a draw spent only after every check passes.
// vanilla: SpeleothemBlock.growStalactiteOrStalagmiteIfPossible
func (p *Physics) growStalactiteOrStalagmite(x, y, z int32, b registry.Block, changes *[]BlockChange) {
	growth := speleothemGrowth[b]
	if !p.blockIs(x, y+1, z, growth.growOn) {
		return
	}
	if b == registry.BlockPointedDripstone {
		// vanilla: PointedDripstoneBlock.canGrow
		if fs := fluidHolding(p.w.BlockAt(x, y+2, z)); fs.kind != fluidWater || !fs.isSource() {
			return
		}
	}
	start := p.w.BlockAt(x, y, z)
	tx, ty, tz, ok := p.findTip(x, y, z, start, growth.maxLength)
	if !ok {
		return
	}
	tip := p.w.BlockAt(tx, ty, tz)
	if !isSpeleothemState(tip) || registry.StateValue(tip, registry.PropVerticalDirection) != "down" ||
		registry.StateValue(tip, registry.PropThickness) != "tip" || registry.StateBool(tip, registry.PropWaterlogged) ||
		!p.canTipGrow(tx, ty, tz, tip) {
		return
	}
	if p.rng.Intn(2) == 0 {
		p.growSpeleothem(tx, ty, tz, -1, b, changes)
	} else {
		p.growStalagmiteBelow(tx, ty, tz, b, changes)
	}
}

// blockIs reports whether the cell holds the block, whatever its state.
func (p *Physics) blockIs(x, y, z int32, b registry.Block) bool {
	return registry.BlockOf(p.w.BlockAt(x, y, z)) == b
}

// findTip is the column's own unmerged tip within maxSteps cells.
// vanilla: SpeleothemBlock.findTip
func (p *Physics) findTip(x, y, z int32, state uint16, maxSteps int32) (int32, int32, int32, bool) {
	if isSpeleothemState(state) && registry.StateValue(state, registry.PropThickness) == "tip" {
		return x, y, z, true
	}
	return p.stalactiteTipWithin(x, y, z, state, maxSteps)
}

// canTipGrow reports air, or the facing unmerged tip of the same block, in the
// cell a tip grows into.
// vanilla: SpeleothemBlock.canTipGrow
func (p *Physics) canTipGrow(x, y, z int32, tip uint16) bool {
	dy := int32(-1)
	if isSpeleothemState(tip) && registry.StateValue(tip, registry.PropVerticalDirection) == "up" {
		dy = 1
	}
	target := p.w.BlockAt(x, y+dy, z)
	if fluidHolding(target).kind != fluidNone {
		return false
	}
	return airBlocks[registry.BlockOf(target)] || p.unmergedTipFacing(target, registry.BlockOf(tip), -dy)
}

// unmergedTipFacing reports whether a state is an unmerged tip of the block
// whose tip points the way dy says (-1 down, 1 up).
// vanilla: SpeleothemBlock.isUnmergedTipWithDirection
func (p *Physics) unmergedTipFacing(state uint16, b registry.Block, dy int32) bool {
	return isSpeleothemState(state) && registry.BlockOf(state) == b && registry.StateValue(state, registry.PropThickness) == "tip" &&
		(registry.StateValue(state, registry.PropVerticalDirection) == "down") == (dy < 0)
}

// growSpeleothem grows the tip at the cell one block toward dy: into a facing
// unmerged tip it merges both, into air or water it makes a new tip.
// vanilla: SpeleothemBlock.grow
func (p *Physics) growSpeleothem(x, y, z, dy int32, b registry.Block, changes *[]BlockChange) {
	ty := y + dy
	target := p.w.BlockAt(x, ty, z)
	switch {
	case p.unmergedTipFacing(target, b, -dy):
		// vanilla: SpeleothemBlock.createMergedTips
		stalactite, stalagmite := ty, ty-1
		if dy < 0 {
			stalactite, stalagmite = ty+1, ty
		}
		// The stalactite is written first.
		p.createSpeleothem(x, stalactite, z, false, "tip_merge", b, changes)
		p.createSpeleothem(x, stalagmite, z, true, "tip_merge", b, changes)
	case airBlocks[registry.BlockOf(target)] || registry.BlockOf(target) == registry.BlockWater:
		p.createSpeleothem(x, ty, z, dy > 0, "tip", b, changes)
	}
}

// createSpeleothem writes a tip, waterlogged over water, with a
// flags-3 shape pass.
// vanilla: SpeleothemBlock.createSpeleothem
func (p *Physics) createSpeleothem(x, y, z int32, up bool, thickness string, b registry.Block, changes *[]BlockChange) {
	dir := "down"
	if up {
		dir = "up"
	}
	state, ok := registry.WithProps(b.DefaultState(),
		registry.PropValue{P: registry.PropVerticalDirection, V: dir},
		registry.PropValue{P: registry.PropThickness, V: thickness},
		registry.PropValue{P: registry.PropWaterlogged, V: boolStr(fluidHolding(p.w.BlockAt(x, y, z)).kind == fluidWater)})
	if ok {
		p.setShaped(x, y, z, state, changes)
	}
}

// growStalagmiteBelow grows a stalagmite on the first floor under a
// stalactite tip, or lengthens one already there.
// vanilla: SpeleothemBlock.growStalagmiteBelow
func (p *Physics) growStalagmiteBelow(x, y, z int32, b registry.Block, changes *[]BlockChange) {
	probe, _ := registry.WithProp(b.DefaultState(), registry.PropVerticalDirection, "up")
	for range int32(10) {
		y--
		state := p.w.BlockAt(x, y, z)
		if fluidHolding(state).kind != fluidNone {
			return
		}
		if p.unmergedTipFacing(state, b, 1) && p.canTipGrow(x, y, z, state) {
			p.growSpeleothem(x, y, z, 1, b, changes)
			return
		}
		if p.w.Survives(x, y, z, probe) && fluidHolding(p.w.BlockAt(x, y-1, z)).kind != fluidWater {
			p.growSpeleothem(x, y-1, z, 1, b, changes)
			return
		}
		if b == registry.BlockPointedDripstone && !canDripThrough(state, x, y, z) {
			return
		}
	}
}

// TransferDripstoneFluid runs a pointed dripstone's fluid transfer with roll in
// place of the die, so a caller can pin the chance, and returns the changes.
// vanilla: PointedDripstoneBlock.maybeTransferFluid
func (p *Physics) TransferDripstoneFluid(gameTime int64, x, y, z int32, roll float32) []BlockChange {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = gameTime
	var changes []BlockChange
	p.dripstoneTransfer(x, y, z, p.w.BlockAt(x, y, z), roll, &changes)
	return changes
}

// dripstoneTransfer drips when roll is under the fluid's chance: mud under
// water turns to clay, otherwise a cauldron below the tip books its fill.
// vanilla: PointedDripstoneBlock.maybeTransferFluid
func (p *Physics) dripstoneTransfer(x, y, z int32, state uint16, roll float32, changes *[]BlockChange) {
	if roll > dripstoneWaterChance || registry.BlockOf(state) != registry.BlockPointedDripstone {
		return
	}
	if !p.stalactiteStart(x, y, z, state) {
		return
	}
	src, ok := p.fluidAboveStalactite(x, y, z, state)
	if !ok {
		return
	}
	chance := dripstoneWaterChance
	if src.kind == fluidLava {
		chance = dripstoneLavaChance
	}
	if roll >= chance {
		return
	}
	tx, ty, tz, ok := p.stalactiteTip(x, y, z, state)
	if !ok {
		return
	}
	if src.mud && src.kind == fluidWater {
		old := p.w.BlockAt(src.x, src.y, src.z)
		n := len(*changes)
		p.setShaped(src.x, src.y, src.z, clayState, changes)
		if len(*changes) > n {
			// vanilla: Block.pushEntitiesUp
			(*changes)[n].PushUp = CollisionTop(clayState) - CollisionTop(old)
		}
		p.randomEffects = append(p.randomEffects,
			Effect{GameEvent: gameEventBlockChange, X: src.x, Y: src.y, Z: src.z},
			Effect{LevelEvent: levelEventDripstoneDrip, X: tx, Y: ty, Z: tz})
		return
	}
	cx, cy, cz, ok := p.fillableCauldronBelow(tx, ty, tz, src.kind)
	if !ok {
		return
	}
	p.randomEffects = append(p.randomEffects, Effect{LevelEvent: levelEventDripstoneDrip, X: tx, Y: ty, Z: tz})
	p.scheduleBooked(p.cauldronBooked, cx, cy, cz, p.now+int64(cauldronDripDelay+ty-cy), cauldronDripEvent)
}

// stalactiteStart reports whether the cell is the topmost block of a
// hanging pointed dripstone column.
// vanilla: SpeleothemBlock.isStalactiteStartPos
func (p *Physics) stalactiteStart(x, y, z int32, state uint16) bool {
	return isStalactiteState(state) && registry.BlockOf(p.w.BlockAt(x, y+1, z)) != registry.BlockOf(state)
}

// isStalactiteState reports whether a state is a speleothem hanging down.
// vanilla: SpeleothemBlock.isStalactite
func isStalactiteState(state uint16) bool {
	return isSpeleothemState(state) && registry.StateValue(state, registry.PropVerticalDirection) == "down"
}

// isSpeleothemState reports whether a state is in the speleothems tag.
func isSpeleothemState(state uint16) bool {
	return stateInTag(state, registry.TagIDSpeleothems)
}

// fluidAboveStalactite is the fluid above the column's root, or water for mud
// where water does not evaporate; flowing fluid and other blocks give none.
// vanilla: PointedDripstoneBlock.getFluidAboveStalactite
func (p *Physics) fluidAboveStalactite(x, y, z int32, state uint16) (dripstoneSource, bool) {
	if !isStalactiteState(state) {
		return dripstoneSource{}, false
	}
	block := registry.BlockOf(state)
	for i := int32(1); i < dripstoneSearch; i++ {
		ry := y + i
		if p.outsideBuildHeight(ry) {
			return dripstoneSource{}, false
		}
		s := p.w.BlockAt(x, ry, z)
		if registry.BlockOf(s) != block {
			ay := ry + 1
			above := p.w.BlockAt(x, ay, z)
			src := dripstoneSource{x: x, y: ay, z: z}
			if registry.BlockOf(above) == registry.BlockMud && !p.w.WaterEvaporates() {
				src.kind, src.mud = fluidWater, true
				return src, true
			}
			fs := fluidHolding(above)
			if !fs.isSource() {
				return dripstoneSource{}, false
			}
			src.kind = fs.kind
			return src, true
		}
		if !isSpeleothemState(s) || registry.StateValue(s, registry.PropVerticalDirection) != "down" {
			return dripstoneSource{}, false
		}
	}
	return dripstoneSource{}, false
}

// stalactiteTip finds the tip of the stalactite column the cell belongs to,
// merged tips excluded.
// vanilla: SpeleothemBlock.findTip
func (p *Physics) stalactiteTip(x, y, z int32, state uint16) (int32, int32, int32, bool) {
	return p.findTip(x, y, z, state, dripstoneSearch)
}

// stalactiteTipWithin walks down the column for its tip, reading blocks 1 to
// maxSteps-1 below.
// vanilla: SpeleothemBlock.findBlockVertical
func (p *Physics) stalactiteTipWithin(x, y, z int32, state uint16, maxSteps int32) (int32, int32, int32, bool) {
	block := registry.BlockOf(state)
	for i := int32(1); i < maxSteps; i++ {
		ty := y - i
		if p.outsideBuildHeight(ty) {
			return 0, 0, 0, false
		}
		s := p.w.BlockAt(x, ty, z)
		ok := isSpeleothemState(s)
		if ok && registry.StateValue(s, registry.PropThickness) == "tip" {
			return x, ty, z, true
		}
		if registry.BlockOf(s) != block || !ok || registry.StateValue(s, registry.PropVerticalDirection) != "down" {
			return 0, 0, 0, false
		}
	}
	return 0, 0, 0, false
}

// outsideBuildHeight reports whether a Y is out of the dimension's build height.
func (p *Physics) outsideBuildHeight(y int32) bool {
	return y < p.w.MinY() || y >= p.w.MaxY()
}

// canDripThrough reports air, or a fluid-free block whose collision leaves the
// 4-pixel centre column empty.
// vanilla: PointedDripstoneBlock.canDripThrough
func canDripThrough(state uint16, x, y, z int32) bool {
	if airBlocks[registry.BlockOf(state)] {
		return true
	}
	if registry.IsSolidRender(state) || fluidHolding(state).kind != fluidNone {
		return false
	}
	return !StateCollides(state, x, y, z,
		float64(x)+0.375, float64(y), float64(z)+0.375,
		float64(x)+0.625, float64(y)+1, float64(z)+0.625)
}

// cauldronTakesDrip reports whether a cauldron block accepts the fluid.
// vanilla: AbstractCauldronBlock.canReceiveStalactiteDrip
func cauldronTakesDrip(block registry.Block, kind fluidKind) bool {
	switch block {
	case registry.BlockCauldron:
		return kind == fluidWater || kind == fluidLava
	case registry.BlockWaterCauldron:
		return kind == fluidWater
	}
	return false
}

// fillableCauldronBelow walks down from the tip to the first cauldron that
// takes the fluid, through blocks a drop falls through.
// vanilla: PointedDripstoneBlock.findFillableCauldronBelowStalactiteTip
func (p *Physics) fillableCauldronBelow(x, y, z int32, kind fluidKind) (int32, int32, int32, bool) {
	for i := int32(1); i < dripstoneSearch; i++ {
		cy := y - i
		if p.outsideBuildHeight(cy) {
			return 0, 0, 0, false
		}
		s := p.w.BlockAt(x, cy, z)
		if cauldronTakesDrip(registry.BlockOf(s), kind) {
			return x, cy, z, true
		}
		if !canDripThrough(s, x, cy, z) {
			return 0, 0, 0, false
		}
	}
	return 0, 0, 0, false
}

// tipAboveCauldron walks up from the cauldron to the free-hanging tip over
// it.
// vanilla: PointedDripstoneBlock.findStalactiteTipAboveCauldron
func (p *Physics) tipAboveCauldron(x, y, z int32) (int32, int32, int32, uint16, bool) {
	for i := int32(1); i < dripstoneSearch; i++ {
		ty := y + i
		if p.outsideBuildHeight(ty) {
			return 0, 0, 0, 0, false
		}
		s := p.w.BlockAt(x, ty, z)
		if isSpeleothemState(s) && registry.StateValue(s, registry.PropVerticalDirection) == "down" &&
			registry.StateValue(s, registry.PropThickness) == "tip" && !registry.StateBool(s, registry.PropWaterlogged) {
			return x, ty, z, s, true
		}
		if !canDripThrough(s, x, ty, z) {
			return 0, 0, 0, 0, false
		}
	}
	return 0, 0, 0, 0, false
}

// cauldronDripTick is a cauldron's booked drip tick: it finds the tip above
// again and takes the fluid that tip still drips.
// vanilla: AbstractCauldronBlock.tick, LayeredCauldronBlock.receiveStalactiteDrip,
// CauldronBlock.receiveStalactiteDrip
func (p *Physics) cauldronDripTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	block := registry.BlockOf(state)
	if block != registry.BlockCauldron && block != registry.BlockWaterCauldron {
		return
	}
	tx, ty, tz, tip, ok := p.tipAboveCauldron(x, y, z)
	if !ok {
		return
	}
	src, ok := p.fluidAboveStalactite(tx, ty, tz, tip)
	if !ok || !cauldronTakesDrip(block, src.kind) {
		return
	}
	var next uint16
	event := levelEventCauldronWater
	switch {
	case block == registry.BlockCauldron && src.kind == fluidLava:
		next, event = dripLavaCauldron, levelEventCauldronLava
	case block == registry.BlockCauldron:
		next = waterCauldronByLevel[1]
	default:
		level, _ := registry.StateInt(state, registry.PropLevel)
		if level >= cauldronFullLevel {
			return
		}
		next = waterCauldronByLevel[level+1]
	}
	if !p.setCauldron(x, y, z, next, changes) {
		return
	}
	p.effects = append(p.effects, Effect{LevelEvent: event, GameEvent: gameEventBlockChange, X: x, Y: y, Z: z})
}
