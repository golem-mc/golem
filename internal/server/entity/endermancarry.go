package entity

import (
	"cmp"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The enderman's carried block: it takes a holdable block from beside it and
// sets it down again, while mob_griefing allows it.
// vanilla: EnderMan.EndermanTakeBlockGoal, EnderMan.EndermanLeaveBlockGoal

// CarriedBlock is EnderMan.getCarriedBlock, the block state or 0 for none.
func (m *Mob) CarriedBlock() uint16 {
	if m.Type != TypeEnderman {
		return 0
	}
	return m.enderman.carried
}

// SetCarriedBlock is EnderMan.setCarriedBlock.
func (m *Mob) SetCarriedBlock(state uint16) {
	if m.Type == TypeEnderman {
		m.SetMetadataEndermanCarried(state)
	}
}

// outlineBlocks reports whether the cell holds a block with an outline shape
// a clip with Fluid.NONE meets: anything but air and a bare fluid.
func outlineBlocks(w World, x, y, z int32) bool {
	if w.IsAirAt(x, y, z) {
		return false
	}
	switch registry.BlockOf(w.BlockAt(x, y, z)) {
	case registry.BlockWater, registry.BlockLava:
		return false
	}
	return true
}

// takeReachable is EndermanTakeBlockGoal's clip: the horizontal ray between
// the cell centres meets no block before the target. Any block in the way
// counts whole, where vanilla's clip meets only its outline.
func takeReachable(w World, fromX, fromZ, x, y, z int32) bool {
	ox, oz := float64(fromX)+0.5, float64(fromZ)+0.5
	dx, dz := float64(x)+0.5-ox, float64(z)+0.5-oz
	steps := int(math.Ceil(math.Hypot(dx, dz) / 0.05))
	for i := 0; i <= steps; i++ {
		f := 1.0
		if steps > 0 {
			f = float64(i) / float64(steps)
		}
		cx, cz := int32(math.Floor(ox+dx*f)), int32(math.Floor(oz+dz*f))
		if cx == x && cz == z {
			return true
		}
		if outlineBlocks(w, cx, y, cz) {
			return false
		}
	}
	return false
}

type endermanTakeBlockGoal struct{ m *Mob }

func (g *endermanTakeBlockGoal) Priority() int    { return 11 }
func (g *endermanTakeBlockGoal) Flags() GoalFlags { return 0 }

func (g *endermanTakeBlockGoal) CanStart(t *tickView) bool {
	if g.m.enderman.carried != 0 || !t.w.MobGriefing() {
		return false
	}
	return g.m.Rand.Intn(reducedTickDelay(20)) == 0
}

func (g *endermanTakeBlockGoal) Start(*tickView) {}

// ShouldContinue re-rolls the start, as Goal.canContinueToUse defaults to.
func (g *endermanTakeBlockGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *endermanTakeBlockGoal) Tick(t *tickView) {
	m := g.m
	x := int32(math.Floor(m.X - 2 + m.Rand.Float64()*4))
	y := int32(math.Floor(m.Y + m.Rand.Float64()*3))
	z := int32(math.Floor(m.Z - 2 + m.Rand.Float64()*4))
	state := t.w.BlockAt(x, y, z)
	block := registry.BlockOf(state)
	if !block.InTag(registry.TagIDEndermanHoldable) {
		return
	}
	if !takeReachable(t.w, int32(math.Floor(m.X)), int32(math.Floor(m.Z)), x, y, z) {
		return
	}
	m.SetMetadataEndermanCarried(block.DefaultState())
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobTookBlock, X: float64(x), Y: float64(y), Z: float64(z), BlockState: state})
}

func (g *endermanTakeBlockGoal) Stop(*tickView)          {}
func (g *endermanTakeBlockGoal) RequiresEveryTick() bool { return false }

type endermanLeaveBlockGoal struct{ m *Mob }

func (g *endermanLeaveBlockGoal) Priority() int    { return 10 }
func (g *endermanLeaveBlockGoal) Flags() GoalFlags { return 0 }

func (g *endermanLeaveBlockGoal) CanStart(t *tickView) bool {
	if g.m.enderman.carried == 0 || !t.w.MobGriefing() {
		return false
	}
	return g.m.Rand.Intn(reducedTickDelay(2000)) == 0
}

func (g *endermanLeaveBlockGoal) Start(*tickView) {}

// ShouldContinue re-rolls the start, as Goal.canContinueToUse defaults to.
func (g *endermanLeaveBlockGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *endermanLeaveBlockGoal) Tick(t *tickView) {
	m := g.m
	x := int32(math.Floor(m.X - 1 + m.Rand.Float64()*2))
	y := int32(math.Floor(m.Y + m.Rand.Float64()*2))
	z := int32(math.Floor(m.Z - 1 + m.Rand.Float64()*2))
	carried := m.enderman.carried
	if carried == 0 || !g.canPlace(t, x, y, z, carried) {
		return
	}
	m.SetMetadataEndermanCarried(0)
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobPlacedBlock, X: float64(x), Y: float64(y), Z: float64(z), BlockState: carried})
}

// canPlace is EndermanLeaveBlockGoal.canPlaceBlock: air over a full
// non-bedrock block, a block that survives there, and no entity in the cell
// (only the tick's players and mobs count).
func (g *endermanLeaveBlockGoal) canPlace(t *tickView, x, y, z int32, carried uint16) bool {
	w := t.w
	below := w.BlockAt(x, y-1, z)
	if !w.IsAirAt(x, y, z) || w.IsAirAt(x, y-1, z) || registry.BlockOf(below) == registry.BlockBedrock ||
		!world.CollisionFull(below) || !w.Survives(x, y, z, carried) {
		return false
	}
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	for i := range t.players {
		p := &t.players[i]
		height := 1.8
		if p.Sneaking {
			height = 1.5
		}
		if boxesOverlap(cx, cy, cz, 1, 1, p.X, p.Y, p.Z, 0.6, height) {
			return false
		}
	}
	for _, o := range t.others {
		if o != g.m && o.Alive() && boxesOverlap(cx, cy, cz, 1, 1, o.X, o.Y, o.Z, o.width(), o.height()) {
			return false
		}
	}
	return true
}

func (g *endermanLeaveBlockGoal) Stop(*tickView)          {}
func (g *endermanLeaveBlockGoal) RequiresEveryTick() bool { return false }

// carriedDrops is EnderMan.dropCustomDeathLoot: the loot of the carried
// block, rolled in level as if mined with a silk touch diamond axe.
// vanilla: VanillaEnchantmentProviders.ENDERMAN_LOOT_DROP
func (m *Mob) carriedDrops(level rules.LootLevel) []protocol.ItemStack {
	state := m.CarriedBlock()
	if state == 0 {
		return nil
	}
	axe := protocol.ItemStack{ItemID: int32(registry.ItemDiamondAxe), Count: 1}
	enchantByKey("minecraft:enderman_loot_drop", diceEnchantRandom{GoDice{m.lootRand}}, &axe, 0)
	ctx := rules.LootContext{
		BlockState: state,
		HasBlock:   true,
		Level:      level,
		Tool:       rules.ToolOf(axe),
	}
	return rules.RollLoot(rules.BlockLootTable(registry.BlockOf(state)), &ctx, m.lootRand)
}

// mobTargetGoal is NearestAttackableTargetGoal<victim>(mustSee): about every
// ten ticks the nearest visible victim-type mob in follow range becomes the
// target. The search box is the follow range on all three axes.
// vanilla: NearestAttackableTargetGoal.getTargetSearchArea
type mobTargetGoal struct {
	m        *Mob
	victim   MobType
	priority int
	unseen   int
	// matches replaces the victim type test; unsighted drops the line of
	// sight (mustSee false); wildOnly is NonTameRandomTargetGoal's gate.
	matches   func(o *Mob) bool
	unsighted bool
	wildOnly  bool
	// fromPrey scans the tick's prey rather than the herd; matches must
	// accept only types isPrey admits.
	fromPrey bool
	// players adds the huntable players to the candidates, ranked with the
	// mobs (NearestAttackableTargetGoal<LivingEntity>).
	players bool
	// interval replaces nearestScanInterval where non-zero.
	interval int
	// gate refuses the scan while it answers false; eager skips the scan
	// interval's roll (randomInterval 0).
	gate  func() bool
	eager bool
	// memory replaces unseenMemoryTicks where nonzero.
	memory int
	// area limits the scan's candidates, as getTargetSearchArea does; the hunt
	// itself is not re-asked. nil for the follow-range box.
	area func(o *Mob) bool
	// started runs when the goal starts, nil for nothing.
	started func()
	// followShare scales the follow range the scan and the hunt hold to, 1 when
	// zero.
	// vanilla: NearestAttackableTargetGoal.getFollowDistance
	followShare float64
}

// follow is the follow range the goal scans and holds its target within.
func (g *mobTargetGoal) follow() float64 {
	if g.followShare != 0 {
		return g.m.followRange() * g.followShare
	}
	return g.m.followRange()
}

// wants reports whether o is a mob this goal hunts.
func (g *mobTargetGoal) wants(o *Mob) bool {
	if g.matches != nil {
		return g.matches(o)
	}
	return o.Type == g.victim
}

func (g *mobTargetGoal) Priority() int    { return g.priority }
func (g *mobTargetGoal) Flags() GoalFlags { return GoalTarget }

func (g *mobTargetGoal) CanStart(t *tickView) bool {
	m := g.m
	if g.wildOnly && m.Tamable.Tame {
		return false
	}
	interval := nearestScanInterval
	if g.interval != 0 {
		interval = g.interval
	}
	if m.TargetID != 0 || g.gate != nil && !g.gate() {
		return false
	}
	if !g.eager && m.Rand.Intn(reducedTickDelay(interval)) != 0 {
		return false
	}
	var nearest *Mob
	best := math.MaxFloat64
	follow := g.follow()
	follow2 := follow * follow
	cands := t.others
	if g.fromPrey && t.preyKnown {
		cands = t.prey
	}
	for _, o := range cands {
		// The range test is a field read, so it goes first: most of t.others is
		// far from a zombie's three scans.
		if Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) > follow2 || o == m || m.alliedToMob(o) || !g.wants(o) || !o.Alive() || o.Invulnerable || g.area != nil && !g.area(o) {
			continue
		}
		if !g.unsighted && !g.sees(t.w, o) {
			continue
		}
		if rank := Dist2(o.X, o.Y, o.Z, m.X, m.Y+m.eyeHeight(), m.Z); rank < best {
			best, nearest = rank, o
		}
	}
	if g.players {
		// A tie goes to the mob.
		if p := m.nearestHuntable(t, nil, !g.unsighted); p != nil &&
			Dist2(p.X, p.Y, p.Z, m.X, m.Y+m.eyeHeight(), m.Z) < best {
			m.SetMetadataTargetID(p.EntityID)
			return true
		}
	}
	if nearest == nil {
		return false
	}
	m.SetMetadataTargetID(nearest.ID)
	return true
}

// sees is the line of sight from the hunter's eyes to the victim's.
func (g *mobTargetGoal) sees(w World, o *Mob) bool {
	return rayClear(w, g.m.X, g.m.Y+g.m.eyeHeight(), g.m.Z, o.X, o.Y+o.eyeHeight(), o.Z)
}

func (g *mobTargetGoal) Start(*tickView) {
	g.unseen = 0
	if g.started != nil {
		g.started()
	}
}

// ShouldContinue is TargetGoal.canContinueToUse: the target alive, inside the
// follow range, and not out of sight for more than sixty ticks.
func (g *mobTargetGoal) ShouldContinue(t *tickView) bool {
	o := t.mobByID(g.m.TargetID)
	if o == nil && g.players {
		return g.keepsPlayer(t)
	}
	if o == nil || !o.Alive() || g.m.alliedToMob(o) || !g.wants(o) || o.Invulnerable {
		return false
	}
	follow := g.follow()
	if Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z) > follow*follow {
		return false
	}
	return g.inSight(g.unsighted || g.sees(t.w, o))
}

// keepsPlayer is ShouldContinue for a player target.
// vanilla: TargetGoal.canContinueToUse
func (g *mobTargetGoal) keepsPlayer(t *tickView) bool {
	p := g.m.targetIn(t)
	if p == nil || g.m.alliedTo(p) || g.m.distanceToPlayer(p) > g.follow() {
		return false
	}
	return g.inSight(g.unsighted || g.m.seesTarget(t.w, p))
}

// inSight counts the target's time out of sight and reports whether the
// unseen memory still holds it.
func (g *mobTargetGoal) inSight(seen bool) bool {
	if seen {
		g.unseen = 0
		return true
	}
	g.unseen++
	return g.unseen <= reducedTickDelay(cmp.Or(g.memory, unseenMemoryTicks))
}

func (g *mobTargetGoal) Tick(*tickView) {}

func (g *mobTargetGoal) Stop(*tickView) {
	g.unseen = 0
	g.m.SetMetadataTargetID(0)
}

func (g *mobTargetGoal) RequiresEveryTick() bool { return false }
