package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TypeSulfurCube is the non-attacking cube of the sulfur caves.
const TypeSulfurCube = registry.EntityTypeSulfurCube

func init() {
	// vanilla: EntityTypes.SULFUR_CUBE, SulfurCube.createSulfurCubeAttributes
	kinds[TypeSulfurCube] = &kind{
		name: "Sulfur Cube", width: 0.49, height: 0.49, eyeHeight: 0.175,
		maxHealth: 8, speed: 0.4, hostile: true, livesInPeaceful: true, cube: true, spawnScale: 2,
		sounds:   MobSounds{Eat: registry.SoundEntitySmallSulfurCubeEat},
		babyable: true, temptTag: &registry.ItemTagIDSulfurCubeSwallowable,
	}
	mobTracking[TypeSulfurCube] = tracking{rangeChunks: 10, updateInterval: 3}
}

// vanilla: SulfurCube.ageBoundaryReached
func (m *Mob) syncSulfurSize() {
	if m.Type != TypeSulfurCube || m.IsBaby() {
		return
	}
	size := 2
	if m.Size != size {
		m.SetSize(size, true)
	}
}

type sulfurState struct {
	filled      bool
	pickupTimer int
	wanted      LootItem
	hasWanted   bool
	// Expire snapshots when the shared pickup pass sees no ground items.
	wantedUntil int
	// primed and fuse are SulfurCube.fuse (>= 0 once primed); maxFuse is the
	// synched MAX_FUSE, 0 until a priming sets it.
	primed        bool
	fuse, maxFuse int
	// powered is the redstone signal at the cube's cell, read by the server.
	powered bool
	// primedSound queues the priming sound for the next tick's events.
	primedSound bool
	// prof is the cached sulfurProfile of the block whose item is profKey-1.
	prof    *sulfurProfile
	profKey int32
	// modsMap and modsKey are the map and block the archetype modifiers were
	// last put on for.
	modsMap *player.Attributes
	modsKey int32
	mods    []registry.SulfurModifier
	// hitSound queues the hit sound of a shove for the next tick's events.
	hitSound bool
	// pushCooldown is SulfurCube.pushSoundCooldown, in ticks.
	pushCooldown int
	// tntOff is the tnt_explodes rule being off, as of the last tick.
	tntOff bool
}

// SulfurExplosive reports whether the absorbed block makes the cube explosive.
// vanilla: SulfurCube.canExplode's explosionData
func (m *Mob) SulfurExplosive() bool {
	return m.SulfurCubeFilled() && m.sulfurProf().explosion != nil
}

// SulfurPrimed reports a lit fuse. vanilla: SulfurCube.isPrimed
func (m *Mob) SulfurPrimed() bool { return m.Type == TypeSulfurCube && m.sulfur.primed }

// SulfurCanExplode reports a live explosive cube whose fuse is not lit.
// vanilla: SulfurCube.canExplode
func (m *Mob) SulfurCanExplode() bool {
	return m.SulfurExplosive() && m.Alive() && !m.sulfur.primed
}

// SulfurMaxFuse is the synched MAX_FUSE, -1 until a priming sets it.
func (m *Mob) SulfurMaxFuse() int {
	if m.sulfur.maxFuse == 0 {
		return -1
	}
	return m.sulfur.maxFuse
}

// SetSulfurPowered records the redstone signal at the cube's cell.
func (m *Mob) SetSulfurPowered(p bool) { m.sulfur.powered = p }

// PrimeSulfur lights the fuse; imminent rolls TNT's short fuse. It reports
// whether the fuse was lit. The cube is invulnerable from then on.
// vanilla: SulfurCube.primeTime
func (m *Mob) PrimeSulfur(imminent bool) bool {
	return m.PrimeSulfurWithRule(imminent, !m.sulfur.tntOff)
}

// PrimeSulfurWithRule lights the fuse using the caller's live tnt_explodes
// value instead of the last tick's snapshot.
func (m *Mob) PrimeSulfurWithRule(imminent, enabled bool) bool {
	if !enabled || !m.SulfurCanExplode() {
		return false
	}
	fuse := m.sulfurProf().explosion.Fuse
	if imminent {
		fuse = rules.TNTShortFuse(m.Rand, fuse)
	}
	m.Invulnerable = true
	m.sulfur.primed = true
	m.sulfur.fuse = fuse
	m.SetMetadataSulfurMaxFuse(fuse)
	m.sulfur.primedSound = true
	return true
}

// PrimeSulfurByFireArrow primes a cube holding a block that a burning arrow
// hit, before the arrow's own hurt.
// vanilla: SulfurCube.hurtServer
func (m *Mob) PrimeSulfurByFireArrow() {
	if m.Type == TypeSulfurCube && m.SulfurCanExplode() {
		m.PrimeSulfur(false)
	}
}

// sulfurHurtHook primes a cube a fire or an explosion hurts.
// vanilla: SulfurCube.hurtServer
func (m *Mob) sulfurHurtHook(damageType registry.DamageType) {
	if m.Type != TypeSulfurCube || !m.SulfurCanExplode() {
		return
	}
	switch {
	case damageType.InTag(registry.DamageTypeTagIDIsFire):
		m.PrimeSulfur(false)
	case damageType.InTag(registry.DamageTypeTagIDIsExplosion):
		m.PrimeSulfur(true)
	}
}

// sulfurFuseStep winds the fuse, primes a cube on power and blasts at zero.
// vanilla: SulfurCube.tick (tickFuse, primeWhenOnPoweredPosition)
func (m *Mob) sulfurFuseStep(w World, events []MobEvent) ([]MobEvent, bool) {
	if m.SulfurExplosive() {
		m.sulfur.tntOff = !w.BoolRule(world.RuleTNTExplodes)
	}
	if m.sulfur.primed && m.sulfur.fuse > 0 {
		m.sulfur.fuse--
	}
	if m.sulfur.primed && m.sulfur.fuse == 0 && m.SulfurExplosive() {
		m.gone = true
		if m.sulfur.tntOff {
			return append(events, MobEvent{Mob: m, Kind: MobVanished}), true
		}
		return append(events, MobEvent{Mob: m, Kind: MobExploded,
			X: m.X, Y: m.Y + m.BbHeight()*0.0625, Z: m.Z, Radius: float64(m.sulfurProf().explosion.Power),
			Fire: m.sulfurProf().explosion.CausesFire}), true
	}
	if m.sulfur.powered && m.SulfurCanExplode() {
		m.PrimeSulfur(false)
	}
	if m.sulfur.hitSound {
		m.sulfur.hitSound = false
		events = append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: m.sulfurProf().hitSound, Volume: 1, Pitch: 1})
	}
	if m.sulfur.primedSound {
		m.sulfur.primedSound = false
		events = append(events, MobEvent{Mob: m, Kind: MobPrimed})
	}
	return events, false
}

// SulfurCubeFilled reports whether the cube contains a block.
func (m *Mob) SulfurCubeFilled() bool { return m.Type == TypeSulfurCube && !m.Body.Empty() }

// AbsorbSulfurBlock equips one swallowable item, returning the displaced block.
// vanilla: SulfurCube.equipItem
func (m *Mob) AbsorbSulfurBlock(stack protocol.ItemStack) (protocol.ItemStack, bool) {
	if m.Type != TypeSulfurCube || !m.Alive() || m.IsBaby() || stack.Empty() ||
		!registry.Item(stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable) ||
		!m.Body.Empty() && m.Body.ItemID == stack.ItemID {
		return protocol.ItemStack{}, false
	}
	old := m.Body
	stack.Count = 1
	m.SetEquipment(6, stack)
	m.BodyDropChance = guaranteedDropChance
	return old, true
}

// LootPickupEnabled reports the kind's canPickUpLoot decision.
func (m *Mob) LootPickupEnabled() bool {
	if m.Type == TypeSulfurCube {
		return m.Body.Empty()
	}
	if m.Type == TypeAllay {
		// vanilla: Allay.canPickUpLoot
		return !m.allay.pickupWaitSet && m.AllayHolding()
	}
	return m.CanPickUpLoot
}

// vanilla: SulfurCube.collectEquipmentChanges, customServerAiStep
func (m *Mob) sulfurStep() {
	if !m.NoAI && m.sulfur.pickupTimer > 0 {
		m.sulfur.pickupTimer--
	}
	if !m.NoAI && m.sulfur.pushCooldown > 0 {
		m.sulfur.pushCooldown--
	}
	filled := m.SulfurCubeFilled()
	if filled == m.sulfur.filled {
		return
	}
	m.sulfur.filled = filled
	m.ai.clear()
	m.stopSeeking()
	m.cube.wanted, m.jump = false, false
	if !filled {
		m.ai.wire(cubeGoals(m)...)
	}
}

func (m *Mob) canSearchSulfurItems() bool {
	return m.Alive() && !m.IsBaby() && !m.SulfurCubeFilled() && m.sulfur.pickupTimer <= 0
}

// vanilla: SulfurCubeSearchForItemsGoal.canUse
func (m *Mob) senseSulfurItems(items []LootItem, grid *BoxGrid) {
	m.sulfur.hasWanted = false
	if !m.canSearchSulfurItems() {
		return
	}
	best := 1e30
	box := m.mobBox()
	consider := func(it *LootItem) {
		if it.Stack.Empty() ||
			it.X+halfSize < box.minX-8 || it.X-halfSize > box.maxX+8 ||
			it.Y+halfSize < box.minY-8 || it.Y-halfSize > box.maxY+8 ||
			it.Z+halfSize < box.minZ-8 || it.Z-halfSize > box.maxZ+8 ||
			!registry.Item(it.Stack.ItemID).InTag(registry.ItemTagIDSulfurCubeSwallowable) {
			return
		}
		if d := Dist2(m.X, m.Y, m.Z, it.X, it.Y, it.Z); d < best {
			best = d
			m.sulfur.wanted, m.sulfur.hasWanted, m.sulfur.wantedUntil = *it, true, m.Age+2
		}
	}
	if grid == nil {
		for i := range items {
			consider(&items[i])
		}
		return
	}
	grid.Query(box.Inflate(8), func(i int32) { consider(&items[i]) })
}

type sulfurSearchGoal struct{ m *Mob }

func (g *sulfurSearchGoal) Priority() int    { return 3 }
func (g *sulfurSearchGoal) Flags() GoalFlags { return GoalLook }
func (g *sulfurSearchGoal) CanStart(*tickView) bool {
	m := g.m
	return m.sulfur.hasWanted && m.Age <= m.sulfur.wantedUntil && !m.IsBaby() && !m.SulfurCubeFilled() && m.sulfur.pickupTimer <= 0
}
func (g *sulfurSearchGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *sulfurSearchGoal) Start(*tickView)                 {}
func (g *sulfurSearchGoal) Stop(*tickView)                  {}
func (g *sulfurSearchGoal) RequiresEveryTick() bool         { return false }
func (g *sulfurSearchGoal) Tick(*tickView) {
	m := g.m
	it := m.sulfur.wanted
	m.Yaw = turnToward(m.Yaw, float32(yawToward(it.X-m.X, it.Z-m.Z)), 10)
	m.HeadYaw = m.Yaw
	m.Pitch = turnToward(m.Pitch, float32(pitchToward(it.X-m.X, it.Y+itemEyeHeight-m.Y-m.eyeHeight(), it.Z-m.Z)), 10)
	m.cube.yRot, m.cube.aggressive = m.Yaw, true
}
