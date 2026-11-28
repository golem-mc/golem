package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TypeFrog is minecraft:frog.
const TypeFrog = registry.EntityTypeFrog

func init() {
	// vanilla: EntityTypes.FROG, Frog.createAttributes
	kinds[TypeFrog] = &kind{
		fireAverse:   true,
		headRotSpeed: 35, maxHeadYRot: 5,
		name: "Frog", width: 0.5, height: 0.5, eyeHeight: float64(float32(0.5) * float32(0.85)),
		maxHealth: 10, speed: 1, attackDamage: 10, stepHeight: 1,
		temptTag: &registry.ItemTagIDFrogFood, temptPace: 1.25, strollPace: 1,
		// vanilla: Frog.playStepSound
		sounds:          MobSounds{Ambient: registry.SoundEntityFrogAmbient, Hurt: registry.SoundEntityFrogHurt, Death: registry.SoundEntityFrogDeath, Step: registry.SoundEntityFrogStep, Eat: registry.SoundEntityFrogEat},
		ambientInterval: 120, // vanilla: Animal.getAmbientSoundInterval
	}
	mobTracking[TypeFrog] = tracking{rangeChunks: 10, updateInterval: 3}
}

// finalizeFrog draws the variant from the frog_variant spawn conditions, then
// the long jump cooldown.
// vanilla: Frog.finalizeSpawn
func (m *Mob) finalizeFrog(dice Dice, ctx rules.SpawnContext) {
	if v, ok := rules.SelectVariantToSpawn("minecraft:frog_variant", ctx, dice); ok {
		m.Variant = v
	}
	m.frog.jump.cooldown = rollJumpGap(m, dice) // vanilla: FrogAi.initMemories
}

// FinishTadpoleGrowth finalizes a conversion and fits the larger body in the world.
// vanilla: Tadpole.ageUp
func (m *Mob) FinishTadpoleGrowth(w World, old *Mob, biome registry.Biome) {
	m.FinalizeSpawn(m.SpawnDice(), biome, nil, rules.LocalDifficulty{})
	m.Persistence = true
	m.refreshDimensions(w, old.width(), old.height())
}

// vanilla: ShootTongue.State
const (
	frogTongueIdle = iota
	frogTongueMoving
	frogTongueCatching
	frogTongueEating
)

// frogTongue holds the attack memory and ShootTongue's animation counters.
type frogTongue struct {
	target                              int32
	phase, timer, elapsed, repath, scan int
	// Entity IDs are unique during this unsaved, 100-tick memory.
	unreachable []int32
	expiry      int

	// jump is LONG_JUMP's state and croak the idle Croak behaviour's counter;
	// croaking is the CROAKING pose.
	jump     goatJump
	croak    int
	croaking bool

	// speed and up are the smooth swimming move control's water input.
	speed, up float64
}

// FrogTongueTarget returns the synched tongue target, or zero when absent.
func (m *Mob) FrogTongueTarget() int32 {
	if m.frog.phase == frogTongueIdle {
		return 0
	}
	return m.frog.target
}

// The frog's long jump and croak numbers.
// vanilla: FrogAi.initJumpActivity, Croak
const (
	frogJumpGapMin   = 100 // FrogAi.TIME_BETWEEN_LONG_JUMPS
	frogJumpGapMax   = 140
	frogJumpHeight   = 2 // LongJumpToPreferredBlock's maxLongJumpHeight
	frogJumpWidth    = 4 // maxLongJumpWidth
	frogPreferChance = 0.5
	frogCroakTicks   = 60 // Croak.CROAK_TICKS
	frogCroakTimeout = 100
)

// frogGround marks the frogs_spawnable_on tag.
var frogGround = registry.BlockTagOf("minecraft:frogs_spawnable_on")

var frogPreferJumpTag = registry.BlockTagOf("minecraft:frog_prefer_jump_to")

// frogAcceptsLanding is FrogAi.isAcceptableLandingSpot up to
// defaultAcceptableLandingSpot: decided is true when the answer is final. A
// fluid in the cell, below or above refuses; a preferred block or a trapdoor
// (path type TRAPDOOR) accepts.
func frogAcceptsLanding(w World, cell Waypoint) (ok, decided bool) {
	for dy := int32(-1); dy <= 1; dy++ {
		if world.HoldsFluid(w.BlockAt(cell.X, cell.Y+dy, cell.Z)) {
			return false, true
		}
	}
	state, below := w.BlockAt(cell.X, cell.Y, cell.Z), w.BlockAt(cell.X, cell.Y-1, cell.Z)
	if registry.BlockOf(state).InTag(frogPreferJumpTag) || registry.BlockOf(below).InTag(frogPreferJumpTag) {
		return true, true
	}
	return isTrapdoorPathType(state) || world.IsAirState(state) && isTrapdoorPathType(below), false
}

// frogJumpBlocked says a higher activity (tongue, lay spawn) or a lure, a
// mate, or the water rules the LONG_JUMP activity out.
// vanilla: FrogAi.initJumpActivity's memory requirements
func (m *Mob) frogJumpBlocked(tempt *temptGoal, t *tickView) bool {
	return m.frog.target != 0 || m.pregnant && !m.InWater() || m.InLove > 0 || tempt.player(t) != nil
}

// FrogCroaking reports the CROAKING pose.
func (m *Mob) FrogCroaking() bool { return m.Type == TypeFrog && m.frog.croaking }

// SetFrogLongJumpCooldown sets the frog's LONG_JUMP_COOLDOWN_TICKS memory.
func (m *Mob) SetFrogLongJumpCooldown(ticks int) { m.frog.jump.cooldown = ticks }

// FrogUsingTongue reports the USING_TONGUE pose.
func (m *Mob) FrogUsingTongue() bool { return m.Type == TypeFrog && m.frog.phase >= frogTongueCatching }

// vanilla: Frog.canEat
func frogCanEat(o *Mob) bool {
	return o.Alive() && !o.Invulnerable && (!o.isCube() || o.Size == 1) && registry.EntityType(o.TypeID()).InTag(registry.EntityTypeTagIDFrogFood)
}

// vanilla: FrogAttackablesSensor, Sensor.tick
func (m *Mob) frogBrainStep(t *tickView) {
	f := &m.frog
	if f.jump.cooldown > 0 {
		f.jump.cooldown--
	}
	m.midJumpStep(t)
	if f.expiry > 0 {
		f.expiry--
		if f.expiry == 0 {
			f.unreachable = nil
		}
	}
	f.scan--
	if f.scan > 0 {
		return
	}
	f.scan = 20
	if f.target != 0 || m.InLove > 0 || m.panicking() {
		return
	}
	best := 100.0
	for _, o := range t.others {
		d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z)
		if o != m && d < best && frogCanEat(o) && !slices.Contains(f.unreachable, o.ID) && m.seesPoint(t.w, o.X, o.Y+o.eyeHeight(), o.Z) {
			m.SetMetadataFrogTarget(o.ID)
			best = d
		}
	}
}

type frogTongueGoal struct{ m *Mob }

func (g *frogTongueGoal) Priority() int           { return priorityBreed }
func (g *frogTongueGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *frogTongueGoal) RequiresEveryTick() bool { return true }
func (g *frogTongueGoal) prey(t *tickView) *Mob {
	target := g.m.frog.target
	if target == 0 {
		return nil
	}
	return t.mobByID(target)
}

// vanilla: ShootTongue.checkExtraStartConditions, canPathfindToTarget
func (g *frogTongueGoal) CanStart(t *tickView) bool {
	m, f := g.m, &g.m.frog
	// A running croak or stroll (WALK_TARGET present) finishes first; the
	// target is kept for afterwards.
	if f.croaking || !m.navDone() {
		return false
	}
	o := g.prey(t)
	if o == nil || !frogCanEat(o) || m.InLove > 0 || m.panicking() {
		g.m.SetMetadataFrogTarget(0)
		return false
	}
	path, _, ok := m.createPath(t.w, o.X, o.Y, o.Z)
	reachable := ok && m.cell() == o.cell()
	if ok && len(path) > 0 {
		end := path[len(path)-1]
		target := o.cell()
		reachable = float64(distSq(end, target)) < 1.75*1.75
	}
	if !reachable {
		if !slices.Contains(f.unreachable, o.ID) {
			if len(f.unreachable) == 5 {
				f.unreachable = slices.Delete(f.unreachable, 0, 1)
			}
			f.unreachable = append(f.unreachable, o.ID)
		}
		f.expiry = 100
		g.m.SetMetadataFrogTarget(0)
	}
	return reachable
}
func (g *frogTongueGoal) Start(t *tickView) {
	m, f := g.m, &g.m.frog
	g.m.SetMetadataFrogPhase(frogTongueMoving)
	f.timer, f.elapsed, f.repath = 0, 0, 10
	if o := g.prey(t); o != nil {
		m.seekTo(t.w, o.X, o.Y, o.Z, 2, 0)
	}
}
func (g *frogTongueGoal) ShouldContinue(t *tickView) bool {
	m, f := g.m, &g.m.frog
	o := g.prey(t)
	return f.phase != frogTongueIdle && f.elapsed <= 100 && !m.panicking() && (f.phase == frogTongueEating || o != nil && frogCanEat(o))
}
func (g *frogTongueGoal) Stop(*tickView) {
	g.m.SetMetadataFrogTarget(0)
	g.m.SetMetadataFrogPhase(frogTongueIdle)
	g.m.stopSeeking()
}

// vanilla: ShootTongue.tick
func (g *frogTongueGoal) Tick(t *tickView) {
	m, f := g.m, &g.m.frog
	if !g.ShouldContinue(t) {
		g.Stop(t)
		return
	}
	f.elapsed++
	o := g.prey(t)
	if o != nil {
		m.goatLookAt(o.X, o.Y+o.eyeHeight(), o.Z)
	}
	sound := func(sound registry.Sound) {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 2, Pitch: 1})
	}
	switch f.phase {
	case frogTongueMoving:
		d := math.Sqrt(Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z))
		if d < 1.75 {
			sound(registry.SoundEntityFrogTongue)
			g.m.SetMetadataFrogPhase(frogTongueCatching)
			f.timer = 0
			// vanilla: Vec3.normalize returns zero below 1.0E-4.
			o.VX, o.VY, o.VZ = 0, 0, 0
			if d >= 1e-4 {
				o.VX, o.VY, o.VZ = (m.X-o.X)/d*0.75, (m.Y-o.Y)/d*0.75, (m.Z-o.Z)/d*0.75
			}
		} else if f.repath <= 0 {
			m.seekTo(t.w, o.X, o.Y, o.Z, 2, 0)
			f.repath = 10
		} else {
			f.repath--
		}
	case frogTongueCatching:
		timer := f.timer
		f.timer++
		if timer >= 6 {
			g.m.SetMetadataFrogPhase(frogTongueEating)
			sound(registry.SoundEntityFrogEat)
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobAttackedPlayer, Player: o.asTarget(), Damage: m.attackDamage()})
		}
	case frogTongueEating:
		if f.timer >= 10 {
			g.Stop(t)
		} else {
			f.timer++
		}
	}
}

// frogLaySpawnGoal owns LAY_SPAWN's land search and its walk target.
// vanilla: TryFindLandNearWater, TryLaySpawnOnFluidNearLand
type frogLaySpawnGoal struct {
	m        *Mob
	cooldown int
}

func (g *frogLaySpawnGoal) Priority() int           { return priorityBreed }
func (g *frogLaySpawnGoal) Flags() GoalFlags        { return GoalMove }
func (g *frogLaySpawnGoal) RequiresEveryTick() bool { return true }
func (g *frogLaySpawnGoal) CanStart(*tickView) bool {
	return g.m.pregnant && !g.m.InWater() && !g.m.panicking() && g.m.frog.target == 0
}
func (g *frogLaySpawnGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *frogLaySpawnGoal) Start(*tickView)                 { g.cooldown = 0 }
func (g *frogLaySpawnGoal) Stop(*tickView)                  { g.m.stopSeeking() }

var frogspawnSupportTag = registry.BlockTagOf("minecraft:supports_frogspawn")

// frogSpawnCell returns the first supported air cell in horizontal direction order.
func frogSpawnCell(w World, at Waypoint) (Waypoint, bool) {
	for _, d := range [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
		p := Waypoint{at.X + d[0], at.Y, at.Z + d[1]}
		below := w.BlockAt(p.X, p.Y-1, p.Z)
		fluid := blocklogic.FluidOf(below)
		supported := fluid.Kind == blocklogic.FluidWater && fluid.IsSource() || registry.BlockOf(below).InTag(frogspawnSupportTag)
		if !rules.IsAir(w.BlockAt(p.X, p.Y, p.Z)) || !supported {
			continue
		}
		top := false
		for _, box := range world.CollisionBoxes(below) {
			if box.MaxY == 1 {
				top = true
				break
			}
		}
		if !top {
			return p, true
		}
	}
	return Waypoint{}, false
}

func (g *frogLaySpawnGoal) Tick(t *tickView) {
	m := g.m
	if !m.InWater() && m.OnGround {
		if p, ok := frogSpawnCell(t.w, m.cell()); ok {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobLaidFrogspawn, X: float64(p.X), Y: float64(p.Y), Z: float64(p.Z)})
			m.pregnant = false
			return
		}
	}
	if g.cooldown > 0 {
		g.cooldown--
		return
	}
	if !m.navDone() || rules.IsWater(t.w.BlockAt(m.cell().X, m.cell().Y, m.cell().Z)) {
		return
	}
	g.cooldown = 40
	target, found := frogManhattanFirst(m.cell(), 8, func(p Waypoint) bool {
		if len(world.CollisionBoxes(t.w.BlockAt(p.X, p.Y, p.Z))) != 0 || len(world.CollisionBoxes(t.w.BlockAt(p.X, p.Y-1, p.Z))) == 0 {
			return false
		}
		for _, d := range [4][2]int32{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
			if rules.IsAir(t.w.BlockAt(p.X+d[0], p.Y, p.Z+d[1])) && rules.IsWaterBlock(t.w.BlockAt(p.X+d[0], p.Y-1, p.Z+d[1])) {
				return true
			}
		}
		return false
	})
	if found {
		m.seekTo(t.w, float64(target.X)+0.5, float64(target.Y), float64(target.Z)+0.5, 1, 0)
	}
}

// frogManhattanFirst returns the cell ok accepts that comes first in the
// box of r around at, ordered by Manhattan distance, outside at's column.
// vanilla: BlockPos.withinBoxByManhattanDistance (ties x then y ascending,
// +z before -z), BlockPos.differsHorizontally
func frogManhattanFirst(at Waypoint, r int32, ok func(Waypoint) bool) (Waypoint, bool) {
	nearest := 3*r + 1
	var target Waypoint
	found := false
	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r; dy++ {
			for dz := r; dz >= -r; dz-- {
				dist := max(dx, -dx) + max(dy, -dy) + max(dz, -dz)
				if dx == 0 && dz == 0 || dist >= nearest {
					continue
				}
				if p := (Waypoint{at.X + dx, at.Y + dy, at.Z + dz}); ok(p) {
					target, nearest, found = p, dist, true
				}
			}
		}
	}
	return target, found
}

// frogSwimGoal is TryFindLand, in water at range 8 and pace 1.5 and
// otherwise at 6 and 1.0, then, while in water, the SWIM activity's stroll
// gate. It writes a walk target and ends.
// vanilla: FrogAi.initSwimActivity, FrogAi.initIdleActivity, TryFindLand
type frogSwimGoal struct {
	m *Mob
	// landNext is each TryFindLand's nextOkStartTime in m.Age ticks, the
	// idle one's then the swim one's.
	landNext [2]int
}

func (g *frogSwimGoal) Priority() int    { return priorityTempt + 1 }
func (g *frogSwimGoal) Flags() GoalFlags { return GoalMove }
func (g *frogSwimGoal) CanStart(t *tickView) bool {
	m := g.m
	if len(m.nav.path) > 0 || m.frog.jump.mid || m.frog.target != 0 {
		return false
	}
	swim := m.InWater()
	i, r, pace := 0, int32(6), 1.0
	if swim {
		i, r, pace = 1, 8, 1.5
	}
	if at := m.cell(); rules.IsWater(t.w.BlockAt(at.X, at.Y, at.Z)) {
		// An attempt before the cooldown is up pushes it back again.
		ready := m.Age >= g.landNext[i]
		g.landNext[i] = m.Age + 60
		if ready {
			if p, ok := frogManhattanFirst(at, r, func(p Waypoint) bool { return frogCanStandOn(t.w, p) }); ok {
				return m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, pace, 1)
			}
		}
	}
	if !swim {
		return false
	}
	// vanilla: GateBehavior ORDERED / TRY_ALL over RandomStroll.swim(0.75f), RandomStroll.stroll(1.0f, true)
	if p, ok := axolotlSwimPos(t.w, m); ok && m.seekTo(t.w, p[0], p[1], p[2], 0.75, 0) {
		return true
	}
	if p, ok := landRandomPos(t.w, m, 10, 7); ok {
		return m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, 1, 0)
	}
	return false
}
func (g *frogSwimGoal) Start(*tickView)               {}
func (g *frogSwimGoal) ShouldContinue(*tickView) bool { return false }
func (g *frogSwimGoal) Tick(*tickView)                {}

// A one-shot writes WALK_TARGET; ending it must not cancel MoveToTargetSink.
func (g *frogSwimGoal) Stop(*tickView)          {}
func (g *frogSwimGoal) RequiresEveryTick() bool { return true }

// frogCanStandOn is TryFindLand's test: no fluid, an empty collision shape
// and a sturdy top below.
// vanilla: TryFindLand.canStandOn
func frogCanStandOn(w World, p Waypoint) bool {
	state := w.BlockAt(p.X, p.Y, p.Z)
	return !world.HoldsFluid(state) && len(world.CollisionBoxes(state)) == 0 &&
		registry.FaceSturdy(w.BlockAt(p.X, p.Y-1, p.Z), 1, registry.SupportFull)
}

// frogCroakGoal is Croak, which RunOne picks with weight 3 of the idle
// activity's 7 (stroll 1, look-walk 1, on-ground no-op 2) on any tick without
// a walk target.
// vanilla: Croak, FrogAi.initIdleActivity
type frogCroakGoal struct{ m *Mob }

func (g *frogCroakGoal) Priority() int           { return priorityStroll }
func (g *frogCroakGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *frogCroakGoal) RequiresEveryTick() bool { return true }
func (g *frogCroakGoal) CanStart(*tickView) bool {
	m := g.m
	return !m.frog.croaking && !m.frog.jump.mid && m.OnGround && !m.InWater() && !m.InLava() &&
		len(m.nav.path) == 0 && !m.panicking() && m.frog.phase == frogTongueIdle && m.Rand.Intn(7) < 3
}
func (g *frogCroakGoal) Start(*tickView) {
	g.m.SetMetadataFrogCroaking(true)
	g.m.frog.croak = 0
}
func (g *frogCroakGoal) ShouldContinue(*tickView) bool {
	m := g.m
	return m.frog.croaking && m.frog.croak < frogCroakTicks && m.frog.croak < frogCroakTimeout
}
func (g *frogCroakGoal) Stop(*tickView) { g.m.SetMetadataFrogCroaking(false) }
func (g *frogCroakGoal) Tick(*tickView) { g.m.frog.croak++ }

// frogBrainFields saves the frog's Brain memories.
// vanilla: Brain.serialize, MemoryModuleType.IS_PREGNANT (a Unit, saved as an
// empty compound)
func (m *Mob) frogBrainFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	if load {
		memories := brainMemories(tag)
		_, m.pregnant = nbt.Get[nbt.Compound](memories, "minecraft:is_pregnant")
		m.loadJumpCooldown(memories)
		return
	}
	memories := m.newMemoryMap()
	if m.pregnant {
		memories.Set("minecraft:is_pregnant", memoryTag(nbt.NewOrderedCompound(0)))
	}
	m.saveJumpCooldown(memories)
	out.Set("Brain", brainTag(memories))
}

// goatBrainFields saves the goat's Brain memories.
// vanilla: Brain.serialize, MemoryModuleType.LONG_JUMP_COOLDOWN_TICKS and RAM_COOLDOWN_TICKS
func (m *Mob) goatBrainFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	if load {
		memories := brainMemories(tag)
		m.loadJumpCooldown(memories)
		m.Goat.ram.cooldown = brainTicks(memories, "minecraft:ram_cooldown_ticks")
		return
	}
	memories := m.newMemoryMap()
	m.saveJumpCooldown(memories)
	if m.Goat.ram.cooldown > 0 {
		memories.Set("minecraft:ram_cooldown_ticks", memoryTag(nbt.Int(m.Goat.ram.cooldown)))
	}
	out.Set("Brain", brainTag(memories))
}

func brainMemories(tag nbt.Compound) nbt.Compound {
	brain, _ := nbt.Get[nbt.Compound](tag, "Brain")
	memories, _ := nbt.Get[nbt.Compound](brain, "memories")
	return memories
}

// brainTicks reads a timed memory's {value}; an absent or expired memory is 0.
func brainTicks(memories nbt.Compound, key string) int {
	mem, _ := nbt.Get[nbt.Compound](memories, key)
	v, _ := nbt.IntValue(mem["value"])
	return max(int(v), 0)
}

// brainGlobalPos reads a GlobalPos memory's {value}; nil when absent or malformed.
func brainGlobalPos(memories nbt.Compound, key string) *GlobalPos {
	mem, _ := nbt.Get[nbt.Compound](memories, key)
	v, _ := nbt.Get[nbt.Compound](mem, "value")
	dim, ok := nbt.Get[nbt.String](v, "dimension")
	p, ok2 := nbt.Get[nbt.IntArray](v, "pos")
	if !ok || !ok2 || len(p) != 3 {
		return nil
	}
	return &GlobalPos{string(dim), p[0], p[1], p[2]}
}

// putBrainGlobalPos writes g as a GlobalPos memory; a nil g writes nothing.
func putBrainGlobalPos(memories *memoryMap, key string, g *GlobalPos) {
	if g != nil {
		memories.Set(key, memoryTag(globalPosTag(*g)))
	}
}

func (m *Mob) loadJumpCooldown(memories nbt.Compound) {
	m.longJump().cooldown = brainTicks(memories, "minecraft:long_jump_cooling_down")
}

func (m *Mob) saveJumpCooldown(memories *memoryMap) {
	if c := m.longJump().cooldown; c > 0 {
		memories.Set("minecraft:long_jump_cooling_down", memoryTag(nbt.Int(c)))
	}
}
