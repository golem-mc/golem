package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The strider: a lava walker that shivers anywhere cold and takes a saddle,
// steered by a rider holding a warped fungus on a stick, which also boosts it.
// Left out: step sounds.
// vanilla: Strider

// TypeStrider is minecraft:strider.
const TypeStrider = registry.EntityTypeStrider

const (
	// striderColdSpeed is SUFFOCATING_MODIFIER, ADD_MULTIPLIED_BASE.
	striderColdSpeed = float64(float32(-0.34))
	// striderStrollEvery is RandomStrollGoal(this, 1.0, 60).
	striderStrollEvery = 60
	// striderLiquidTop is the top of the column LiquidBlock stands a strider
	// on, 8 sixteenths.
	// vanilla: Strider.getLiquidCollisionShape
	striderLiquidTop = 0.5
	// striderLavaSearch and striderLavaRise are StriderGoToLavaGoal's
	// horizontal and vertical search ranges.
	striderLavaSearch, striderLavaRise = 8, 2
	// striderJockeyOdds and striderBabyJockeyOdds are the one-in-n rolls of a
	// grown strider's finalize.
	striderJockeyOdds, striderBabyJockeyOdds = 30, 10
	// itemBoostMin and itemBoostSpread make the boost last
	// nextInt(841) + 140 ticks.
	// vanilla: ItemBasedSteering.boost
	itemBoostMin, itemBoostSpread = 140, 841
)

func init() {
	// vanilla: EntityTypes.STRIDER (fireImmune, 0.9 by 1.7, tracking range
	// 10), Strider.createAttributes' 0.175f and BABY_DIMENSIONS.
	kinds[TypeStrider] = &kind{
		name: "Strider", width: 0.9, height: 1.7,
		maxHealth: 20, speed: float64(float32(0.175)), fireImmune: true, walksOnLava: true, waterSensitive: true,
		babyWidth: 0.45, babyHeight: 0.85, babyEye: 0.4375,
		// Set here as mob.go's derived pass runs before this init.
		babyable: true,
		temptTag: &registry.ItemTagIDStriderTemptItems,
		sounds: MobSounds{
			Ambient:    registry.SoundEntityStriderAmbient,
			Hurt:       registry.SoundEntityStriderHurt,
			Death:      registry.SoundEntityStriderDeath,
			Eat:        registry.SoundEntityStriderEat,
			Step:       registry.SoundEntityStriderStep,
			StepVolume: 1,
		},

		followPace:      1.0,  // FollowParentGoal(this, 1.0)
		panicPace:       1.65, // PanicGoal(this, 1.65)
		temptPace:       1.4,  // TemptGoal(this, 1.4, ...)
		strollPace:      1.0,  // RandomStrollGoal(this, 1.0, 60)
		ambientInterval: 120,  // Animal.getAmbientSoundInterval's own
	}
	mobTracking[TypeStrider] = tracking{rangeChunks: 10, updateInterval: 3}
}

// striderState is what a strider keeps beyond the common mob.
type striderState struct {
	// cold is DATA_SUFFOCATING: the strider is out of the warm blocks and
	// shivers.
	cold bool
}

// itemSteeringState is shared by pigs and striders.
type itemSteeringState struct {
	// boosting, boostTime and boostTotal are ItemBasedSteering's state; the
	// total is DATA_BOOST_TIME and stays after the boost ends.
	boosting              bool
	boostTime, boostTotal int
}

// IsStrider reports whether the mob is a strider.
func (m *Mob) IsStrider() bool { return m.Type == TypeStrider }

// StriderCold is the strider's DATA_SUFFOCATING.
func (m *Mob) StriderCold() bool { return m.strider.cold }

// striderGoals is Strider.registerGoals.
// vanilla: Strider.registerGoals
func striderGoals(m *Mob) []Goal {
	return []Goal{
		&panicGoal{m: m},
		&breedGoal{m: m},
		&temptGoal{m: m},
		newStriderGoToLavaGoal(m),
		&followParentGoal{m: m},
		&randomStroll{m: m, interval: striderStrollEvery},
		&lookAtPlayer{m: m, priority: priorityLookAround},
		&randomLookAround{m: m},
		&lookAtStrider{m: m},
	}
}

// lookAtStrider is LookAtPlayerGoal(this, Strider.class, 8): a glance at the
// nearest other strider.
// vanilla: Strider.registerGoals
type lookAtStrider struct {
	m       *Mob
	remains int
	target  *Mob
}

func (g *lookAtStrider) Priority() int    { return priorityLookAround + 1 }
func (g *lookAtStrider) Flags() GoalFlags { return GoalLook }

func (g *lookAtStrider) CanStart(t *tickView) bool {
	if g.m.Rand.Float64() >= lookChance {
		return false
	}
	var best *Mob
	bestSq := lookRange * lookRange
	for _, o := range t.others {
		if o == g.m || !o.IsStrider() || !o.Alive() || math.Abs(o.Y-g.m.Y) > 3 {
			continue
		}
		dx, dy, dz := o.X-g.m.X, o.Y-g.m.Y, o.Z-g.m.Z
		if sq := dx*dx + dy*dy + dz*dz; sq <= bestSq {
			best, bestSq = o, sq
		}
	}
	g.target = best
	return best != nil
}

func (g *lookAtStrider) Start(*tickView) {
	g.remains = reducedTickDelay(lookForTicks + g.m.Rand.Intn(lookForVaries))
}

func (g *lookAtStrider) ShouldContinue(*tickView) bool {
	o := g.target
	if o == nil || !o.Alive() || g.remains <= 0 {
		return false
	}
	dx, dy, dz := o.X-g.m.X, o.Y-g.m.Y, o.Z-g.m.Z
	return dx*dx+dy*dy+dz*dz <= lookRange*lookRange
}

func (g *lookAtStrider) Tick(*tickView) {
	g.remains--
	if o := g.target; o != nil && o.Alive() {
		dx, dz := o.X-g.m.X, o.Z-g.m.Z
		g.m.HeadYaw = float32(yawToward(dx, dz))
		g.m.Pitch = float32(pitchToward(dx, o.Y+o.eyeHeight()-(g.m.Y+g.m.height()*0.85), dz))
	}
}

func (g *lookAtStrider) Stop(*tickView) { g.target = nil }

func (g *lookAtStrider) RequiresEveryTick() bool { return false }

// striderColdPace is the speed the shivering adds to the strider's pace.
func (m *Mob) striderColdPace() float64 {
	if m.strider.cold && m.attrs == nil {
		return m.kind().speed * striderColdSpeed
	}
	return 0
}

// IsStriderWarm reports whether a block is one of the strider_warm_blocks.
func IsStriderWarm(state uint16) bool {
	return int(state) < registry.StateCount && registry.BlockOf(state).InTag(registry.TagIDStriderWarmBlocks)
}

// striderColdStep sets the shiver: on unless the strider stands in or on a
// warm block, has lava in its box, or rides a warm strider.
// vanilla: Strider.tick, Strider.setSuffocating
func (m *Mob) striderColdStep(w World) {
	at := m.cell()
	warm := IsStriderWarm(w.BlockAt(at.X, at.Y, at.Z)) ||
		IsStriderWarm(w.BlockAt(at.X, int32(math.Floor(m.Y-0.2)), at.Z)) ||
		m.lavaFluid.height > 0
	if v := m.Vehicle; v != nil && v.IsStrider() && !v.strider.cold {
		warm = true
	}
	if m.strider.cold != !warm {
		m.SetMetadataStriderCold(!warm)
		if m.attrs != nil {
			m.SyncSpeedModifiers(m.attrs)
		}
	}
}

// standsOnFluid reports whether the fluid at the feet carries the body.
// vanilla: LivingEntity.canStandOnFluid, isInFluidTravel
func (m *Mob) standsOnFluid(w World) bool {
	if !m.kind().walksOnLava {
		return false
	}
	at := m.cell()
	return rules.IsLava(w.BlockAt(at.X, at.Y, at.Z))
}

// lavaStandBoxes is the lava cell's collision for a body that stands on it:
// the half-block column of a source, but only for a body above it and under
// no lava.
// vanilla: LiquidBlock.getCollisionShape, EntityCollisionContext.isAbove
func lavaStandBoxes(w World, state uint16, x, y, z int32, bottom float64) []world.CollisionBox {
	if !rules.IsLavaSource(state) || !world.BodyAbove(bottom, float64(y)+striderLiquidTop) || rules.IsLava(w.BlockAt(x, y+1, z)) {
		return nil
	}
	return lavaStandColumn
}

var lavaStandColumn = []world.CollisionBox{{MaxX: 1, MaxY: striderLiquidTop, MaxZ: 1}}

// floatStrider keeps a strider that sank into lava up, and stands one that
// rests on the surface.
// vanilla: Strider.floatStrider
func (m *Mob) floatStrider(w World) {
	if !m.InLava() {
		return
	}
	at := m.cell()
	if !world.BodyAbove(m.Y, float64(at.Y)+striderLiquidTop) || rules.IsLava(w.BlockAt(at.X, at.Y+1, at.Z)) {
		m.VX *= 0.5
		m.VY = m.VY*0.5 + 0.05
		m.VZ *= 0.5
		return
	}
	m.OnGround = true
}

// StriderCanTakeSaddle reports whether a saddle fits: a living, grown
// pig or strider with the slot free.
// vanilla: Strider.canUseSlot, Equippable.equipOnTarget
func (m *Mob) StriderCanTakeSaddle() bool {
	return (m.IsStrider() || m.Type == TypePig) && m.Alive() && !m.IsBaby() && m.Horse.Saddle.Empty()
}

// StriderClick is what a click on a pig or strider does.
type StriderClick int

const (
	// StriderClickNone: nothing for the strider to do.
	StriderClickNone StriderClick = iota
	// StriderClickMount: the click seats the player.
	StriderClickMount
	// StriderClickSaddle: the saddle is fitted.
	StriderClickSaddle
)

// StriderUseItem decides a click that took no lead, name tag or food: a
// saddled pig or strider with nobody on it seats a player who is not sneaking;
// otherwise a saddle in hand is fitted.
// vanilla: Strider.mobInteract
func (m *Mob) StriderUseItem(held protocol.ItemStack, sneaking bool) StriderClick {
	if !m.Horse.Saddle.Empty() && m.Horse.Rider == uuid.Nil && !sneaking {
		return StriderClickMount
	}
	if !held.Empty() && IsSaddle(held) && m.StriderCanTakeSaddle() {
		held.Count = 1
		m.SetMetadataHorseSaddle(held)
		m.SaddleDropChance = guaranteedDropChance
		return StriderClickSaddle
	}
	return StriderClickNone
}

// IsStriderFood reports whether a stack is the strider's breeding food.
// vanilla: ItemTags.STRIDER_FOOD
func IsStriderFood(stack protocol.ItemStack) bool {
	return !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDStriderFood)
}

// IsSteeringStick reports whether a stack controls the given kind.
// vanilla: Strider.getControllingPassenger
func IsSteeringStick(kind MobType, stack protocol.ItemStack) bool {
	if stack.Empty() {
		return false
	}
	return kind == TypeStrider && registry.Item(stack.ItemID) == registry.ItemWarpedFungusOnAStick ||
		kind == TypePig && registry.Item(stack.ItemID) == registry.ItemCarrotOnAStick
}

// striderGoToLavaGoal is StriderGoToLavaGoal: a strider out of lava walks to
// lava with air over it.
// vanilla: Strider.StriderGoToLavaGoal
type striderGoToLavaGoal struct{ *moveToBlock }

func newStriderGoToLavaGoal(m *Mob) *striderGoToLavaGoal {
	g := &striderGoToLavaGoal{newMoveToBlock(m, 1.0, striderLavaSearch, striderLavaTarget)}
	g.vertRange = striderLavaRise
	g.onBlock = true
	g.recalcEvery = 20
	return g
}

// striderLavaTarget is StriderGoToLavaGoal.isValidTarget: a lava block with a
// land-pathfindable cell over it.
func striderLavaTarget(w World, x, y, z int32) bool {
	return rules.IsLava(w.BlockAt(x, y, z)) && landPathfindable(w.BlockAt(x, y+1, z))
}

// landPathfindable is BlockBehaviour.isPathfindable(LAND) over the overrides
// that differ from the default of "not a full-block collision shape".
func landPathfindable(state uint16) bool {
	if state == 0 {
		return true
	}
	if rules.IsLava(state) {
		return false
	}
	if int(state) >= registry.StateCount {
		return true
	}
	block := registry.BlockOf(state)
	switch block {
	case registry.BlockSnow:
		layers, _ := registry.StateInt(state, registry.PropLayers)
		return layers < 5
	case registry.BlockPowderSnow:
		return true
	case registry.BlockChest, registry.BlockTrappedChest, registry.BlockEnderChest, registry.BlockCake,
		registry.BlockCampfire, registry.BlockSoulCampfire, registry.BlockComposter, registry.BlockCactus,
		registry.BlockAnvil, registry.BlockChippedAnvil, registry.BlockDamagedAnvil, registry.BlockHopper,
		registry.BlockFarmland, registry.BlockDirtPath, registry.BlockMud, registry.BlockSoulSand,
		registry.BlockBrewingStand, registry.BlockEnchantingTable, registry.BlockGrindstone,
		registry.BlockLectern, registry.BlockBell, registry.BlockConduit, registry.BlockDragonEgg,
		registry.BlockStonecutter, registry.BlockEndPortalFrame, registry.BlockRespawnAnchor,
		registry.BlockSeaPickle, registry.BlockCocoa, registry.BlockBamboo,
		registry.BlockLightningRod, registry.BlockEndRod, registry.BlockDecoratedPot, registry.BlockHeavyCore,
		registry.BlockSculkSensor, registry.BlockCalibratedSculkSensor, registry.BlockSnifferEgg,
		registry.BlockDriedGhast, registry.BlockPointedDripstone, registry.BlockChorusPlant, registry.BlockFloweringAzalea, registry.BlockAzalea:
		return false
	}
	switch {
	case block.InGroup(registry.GroupSlab | registry.GroupStairs | registry.GroupWall | registry.GroupFence |
		registry.GroupBed | registry.GroupHead | registry.GroupCauldron | registry.GroupLantern |
		registry.GroupHangingSign | registry.GroupCake | registry.GroupFlowerPot | registry.GroupChain |
		registry.GroupShelf | registry.GroupCopperGolemStatue | registry.GroupBars):
		return false
	case block.InGroup(registry.GroupDoor | registry.GroupTrapdoor | registry.GroupFenceGate):
		return registry.StateBool(state, registry.PropOpen)
	}
	return !registry.CollisionShapeFull(state)
}

func (g *striderGoToLavaGoal) Priority() int { return 4 }

func (g *striderGoToLavaGoal) CanStart(t *tickView) bool {
	return !g.m.InLava() && g.canUse(t)
}

func (g *striderGoToLavaGoal) Start(t *tickView) { g.start(t) }

func (g *striderGoToLavaGoal) ShouldContinue(t *tickView) bool {
	return !g.m.InLava() && g.valid(t.w, g.target.X, g.target.Y, g.target.Z)
}

func (g *striderGoToLavaGoal) Tick(t *tickView) { g.tick(t) }

// striderWalkTargetValue is Strider.getWalkTargetValue: lava is worth ten, and
// a strider in lava wants nowhere else.
func (m *Mob) striderWalkTargetValue(w World, cell Waypoint) float64 {
	if rules.IsLava(w.BlockAt(cell.X, cell.Y, cell.Z)) {
		return 10
	}
	if m.InLava() {
		return math.Inf(-1)
	}
	return 0
}

// striderTempted reports whether the tempt goal holds the legs.
func (m *Mob) striderTempted() bool {
	for _, g := range m.ai.running {
		if _, ok := g.(*temptGoal); ok {
			return true
		}
	}
	return false
}

// striderVoiceStep is the strider's tick sounds: a happy chirp one tick in a
// hundred and forty while tempted, else a retreat call one in sixty while
// panicking.
// vanilla: Strider.tick
func (m *Mob) striderVoiceStep(events []MobEvent) []MobEvent {
	var sound registry.Sound
	switch {
	case m.striderTempted() && m.Rand.Intn(140) == 0:
		sound = registry.SoundEntityStriderHappy
	case m.panicking() && m.Rand.Intn(60) == 0:
		sound = registry.SoundEntityStriderRetreat
	default:
		return events
	}
	if m.Silent {
		return events
	}
	return append(events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: m.SoundVolume(), Pitch: m.VoicePitch()})
}

// ItemSteeringBoost starts the steering stick's boost and reports whether it took: not
// while one is already running.
// vanilla: ItemBasedSteering.boost
func (m *Mob) ItemSteeringBoost() bool {
	s := &m.itemSteering
	if s.boosting {
		return false
	}
	s.boosting, s.boostTime = true, 0
	m.SetMetadataItemSteeringBoostTotal(m.Rand.Intn(itemBoostSpread) + itemBoostMin)
	return true
}

// ItemSteeringBoostTime is DATA_BOOST_TIME, the length the last boost was rolled.
func (m *Mob) ItemSteeringBoostTime() int32 { return int32(m.itemSteering.boostTotal) }

// itemSteeringBoostStep is ItemBasedSteering.tickBoost, run while a player steers.
func (m *Mob) itemSteeringBoostStep() {
	s := &m.itemSteering
	if s.boosting {
		s.boostTime++
		if s.boostTime-1 > s.boostTotal {
			s.boosting = false
		}
	}
}

// finalizeStrider rolls a grown strider's jockey on dice: a zombified piglin
// with a fungus on a stick on a saddled strider, else a baby strider. The
// rider is finalized there and then, on the same dice, before the strider's
// own Mob step.
// vanilla: Strider.finalizeSpawn, Strider.spawnJockey
func (m *Mob) finalizeStrider(dice Dice, ctx rules.SpawnContext, local rules.LocalDifficulty) {
	if m.IsBaby() {
		return
	}
	var rider *Mob
	if dice.NextIntN(striderJockeyOdds) == 0 && !m.MonsterJockeysDisabled {
		rider = m.striderRider(TypeZombifiedPiglin)
		group := &SpawnGroup{rng: dice, claimed: true, zombie: true, noChicken: true}
		group.baby = dice.NextFloat() < babyOdds // vanilla: Zombie.getSpawnAsBabyOdds
		rider.finalizeSpawn(dice, ctx, group, local, false)
		rider.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemWarpedFungusOnAStick), Count: 1})
		m.SetMetadataHorseSaddle(protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1})
		m.SaddleDropChance = guaranteedDropChance
	} else if dice.NextIntN(striderBabyJockeyOdds) == 0 {
		rider = m.striderRider(TypeStrider)
		rider.SetBaby(true)
		rider.finalizeSpawn(dice, ctx, nil, local, false)
	} else {
		return
	}
	m.Jockey, m.Rider = JockeyBuiltRider, rider
}

// striderRider is a strider's unfinalized jockey at its spot, ID 0 until the
// server stands it. Its seed is salted off the strider's loot seed.
// vanilla: Strider.spawnJockey's snapTo
func (m *Mob) striderRider(t MobType) *Mob {
	rider := NewMob(t, 0, m.X, m.Y, m.Z, m.lootSeed^striderRiderSalt)
	rider.Yaw = m.Yaw
	return rider
}

// striderRiderSalt gives a strider's rider its own seed.
const striderRiderSalt = 0x5DEECE66D2B7E151

// StriderDismountSpot is a dismounted player's spot: the first dry floored
// cell among the five escape directions (turned from the rider's yaw) that
// fits the player, else on top.
// vanilla: Strider.getDismountLocationForPassenger
func StriderDismountSpot(w rules.BoatWorld, m *Mob, riderYaw float32) player.Position {
	width, height := m.width(), m.height()
	poses := rules.PlayerDismountPoses
	passengerWidth := poses[0].Dims.Width
	span := (width + passengerWidth + 1e-5) / 2
	top, bottom := m.Y+height, m.Y-0.5
	var cells []Waypoint
	seen := map[Waypoint]bool{}
	for _, turn := range [...]float64{0, -22.5, 22.5, -45, 45} {
		rad := (float64(riderYaw) + turn) * math.Pi / 180
		sin, cos := -math.Sin(rad), math.Cos(rad)
		scale := math.Max(math.Abs(sin), math.Abs(cos))
		cell := cellAt(m.X+sin*span/scale, top, m.Z+cos*span/scale)
		for y := top; y > bottom; y-- {
			if !seen[cell] {
				seen[cell] = true
				cells = append(cells, cell)
			}
			cell.Y--
		}
	}
	for _, c := range cells {
		if rules.IsLava(w.BlockAt(c.X, c.Y, c.Z)) {
			continue
		}
		floor := rules.DismountHeightAt(w, c.X, c.Y, c.Z)
		if !rules.CanDismountInBlock(floor) {
			continue
		}
		x, y, z := float64(c.X)+0.5, float64(c.Y)+floor, float64(c.Z)+0.5
		for _, p := range poses {
			half := p.Dims.Width / 2
			if !w.BoxCollides(x-half, y, z-half, x+half, y+p.Dims.Height, z+half) {
				return player.Position{X: x, Y: y, Z: z}
			}
		}
	}
	return player.Position{X: m.X, Y: m.Y + m.height(), Z: m.Z}
}

// striderSpawnOk is the strider's spawn rule: the cell holds lava, and the
// first cell above the lava column is air.
// vanilla: SpawnPlacementTypes.IN_LAVA, Strider.checkStriderSpawnRules
func striderSpawnOk(w SpawnWorld, x, y, z int32) bool {
	if !rules.IsLava(w.BlockAt(x, y, z)) {
		return false
	}
	for y++; rules.IsLava(w.BlockAt(x, y, z)); y++ {
	}
	return rules.IsAir(w.BlockAt(x, y, z))
}
