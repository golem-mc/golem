package rules

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The environment's tick on a body (lava and fire burns, drowning,
// suffocation, cactus and berry pricks, magma, powder-snow freeze) as plain
// functions over caller-gathered block states and dice. The hurt pipeline
// (invulnerability, packets, death) is internal/server/'s.
// vanilla: Entity.baseTick, LivingEntity.baseTick, LivingEntity.aiStep,
// Entity.applyEffectsFromBlocks
//
// The order is vanilla's and matters, since the invulnerability window lets
// only the first same-tick blow through: Entity.baseTick's burn, then
// LivingEntity.baseTick's suffocation and drowning, then the blocks (magma,
// fire and lava ignition, freeze clearing, hurts, cactus and berry pricks,
// water and cauldron douses), then the rain's douse. The powder snow's
// counter and bite run after all of these, in TickVitals, so a fire lit the
// same tick has already cleared the freeze.

// The environment's numbers, damage in half-hearts:
//
//   - fire ignites for 8 s (BaseFireBlock's igniteForSeconds(8)), lava for 15
//     (Entity.lavaIgnite); a burn bites 1 on each tick a whole second into
//     the counter (Entity.baseTick), and standing in lava or fire bites
//     directly (Entity.lavaHurt 4, the fire block 1, SoulFireBlock 2), all
//     paced by the invulnerability window;
//   - air tops out at 300 ticks (Entity.getMaxAirSupply), drains 1 a tick
//     with the eyes under water and refills 4 out of it; water breathing,
//     conduit power and breath of the nautilus stop the drain, the nautilus
//     alone holding the breath still under water; at -20
//     (LivingEntity.shouldTakeDrowningDamage) it drowns for 2;
//   - a doused or burnt-out body sits 20 ticks in the fire cooldown
//     (Player.getFireImmuneTicks): fire can hurt but not re-light it until
//     the counter is back at 0, while lava reignites straight through;
//   - powder snow counts a frozen tick up per tick the box shares a cell
//     with it, capped at 140 (Entity.getTicksRequiredToFreeze), and two down
//     outside; fully frozen bites 1 every 40th tick of age (5 for
//     freeze_hurts_extra_types) unless freeze_immune_wearables are worn or
//     the body is a spectator, which hold counter and hurt alike (the
//     freeze_damage rule holds the hurt alone);
//   - cactus, berry bush, magma and the block at the eyes bite 1 each.
const (
	// MaxAirSupply is the air a body breathes with, in ticks: vanilla's
	// getMaxAirSupply, the value the air metadata carries fresh.
	MaxAirSupply = 300

	// FireSecondTicks is the length of a second of fire, the tick count the
	// on-fire bite runs its once-a-second check against.
	FireSecondTicks = 20

	// FireIgniteTicks and LavaIgniteTicks are the burns a fire block and a
	// lava one leave a body with, the eight and the fifteen seconds their
	// igniteForSeconds calls grant.
	FireIgniteTicks = 160
	LavaIgniteTicks = 300

	// DouseCooldown is the negative fire counter a doused or burnt-out body
	// waits through before fire can light it again; fire walks it up a tick
	// at a time, lava ignores it.
	// vanilla: Player.getFireImmuneTicks
	DouseCooldown = 20

	// AirDrownFloor is the air, twenty ticks below nothing, at which the
	// next breath drowns.
	AirDrownFloor = -20

	// FullyFrozenTicks is the powder-snow ticks to a full freeze, the
	// ticks-frozen metadata's ceiling.
	// vanilla: Entity.getTicksRequiredToFreeze
	FullyFrozenTicks = 140

	// FreezeThawTicks is the frozen ticks a body outside the powder snow
	// sheds a tick, LivingEntity.aiStep's two.
	FreezeThawTicks = 2

	// FreezeHurtTicks is the span of the body's own age between the hurts
	// of a fully frozen body, vanilla's every-fortieth-tick freeze bite.
	FreezeHurtTicks = 40

	// airRefillPerTick is how fast a body out of the water breathes back
	// up: LivingEntity.increaseAirSupply's four a tick.
	airRefillPerTick = 4

	// Blow sizes in half-hearts; freeze_hurts_extra_types' five is the
	// tag's own decision.
	lavaDamage  = 4.0
	drownDamage = 2.0
	fireDamage  = 1.0
	soulFireDmg = 2.0
	burnDamage  = 1.0
	touchDamage = 1.0
	crushingDmg = 1.0
	freezeDmg   = 1.0
	freezeExtra = 5.0

	// freezeSlow is the movement_speed the powder snow takes off a fully
	// frozen body, a share of it at a part-frozen count:
	// LivingEntity.tryAddFrost's -0.05.
	freezeSlow = 0.05

	// berryPrickStep is the least horizontal step, in blocks, that counts as
	// moving through a berry bush; below it the bush does not prick.
	// vanilla: SweetBerryBushBlock.entityInside
	berryPrickStep = 0.003
)

// BerryBushMoving reports whether a tick's horizontal known movement is
// enough to prick in a berry bush.
// vanilla: SweetBerryBushBlock.entityInside
func BerryBushMoving(dx, dz float64) bool {
	return math.Abs(dx) >= berryPrickStep || math.Abs(dz) >= berryPrickStep
}

// Damage types by minecraft:damage_type name: the Damage Event names the
// type, and the death message is picked per type.
const (
	DamageTypeOnFire         = registry.DamageTypeOnFire
	DamageTypeInFire         = registry.DamageTypeInFire
	DamageTypeLava           = registry.DamageTypeLava
	DamageTypeDrown          = registry.DamageTypeDrown
	DamageTypeCramming       = registry.DamageTypeCramming
	DamageTypeDragonBreath   = registry.DamageTypeDragonBreath
	DamageTypeInWall         = registry.DamageTypeInWall
	DamageTypeCactus         = registry.DamageTypeCactus
	DamageTypeSulfurCubeHot  = registry.DamageTypeSulfurCubeHot
	DamageTypeHotFloor       = registry.DamageTypeHotFloor
	DamageTypeSweetBerryBush = registry.DamageTypeSweetBerryBush
	DamageTypeFreeze         = registry.DamageTypeFreeze
	DamageTypeCampfire       = registry.DamageTypeCampfire
)

// Hurt is one environmental blow, in vanilla's landing order: the type and
// half-hearts. The caller's pipeline refuses one inside another's
// invulnerability window.
type Hurt struct {
	DamageType registry.DamageType
	Amount     float64
}

// CrammingDamage is the hurt of a cramming tick.
// vanilla: LivingEntity.pushEntities
const CrammingDamage = 6

// CrammingHurt says whether a body sharing its box with total pushable bodies,
// standing of them riding nothing, takes the cramming hurt this tick. roll is
// nextInt(4), asked only once the total is over the limit; limit 0 is off.
// vanilla: LivingEntity.pushEntities
func CrammingHurt(limit, total, standing int, roll func() int) bool {
	if limit <= 0 || total <= limit-1 || roll() != 0 {
		return false
	}
	return standing > limit-1
}

// Habitat is the caller-gathered world around a body for one tick: the cells
// its box overlaps, the block underfoot, rain, and what the body is doing
// where vanilla asks (sneaking on magma, moving through a berry bush).
type Habitat struct {
	// Overlapped are the states of the cells the body's box overlaps, any
	// order (vanilla's checkInsideBlocks scan).
	Overlapped []uint16
	// Ground is the block directly under the feet, the one vanilla's
	// stepOn asks after when the body stands on it.
	Ground uint16
	// EyesInWater reports the water at the eyes, isEyeInFluid(WATER), the
	// check the breath runs on.
	EyesInWater bool
	// EyesInBubbleColumn reports a bubble column at the eyes, where the
	// breath is not lost.
	// vanilla: LivingEntity.baseTick
	EyesInBubbleColumn bool
	// WaterBreathing reports water_breathing, conduit_power or
	// breath_of_the_nautilus, which keep the breath full under water.
	// vanilla: MobEffectUtil.hasWaterBreathing
	WaterBreathing bool
	// HoldAir reports breath_of_the_nautilus without the other two: under
	// water the breath neither drains nor refills; out of it, it refills.
	// vanilla: MobEffectUtil.shouldEffectsRefillAirsupply returning false
	HoldAir bool
	// EyesInWall reports the eyes' small cube reaching into a block that
	// crushes, vanilla's isInWall, which is what suffocation bites on.
	EyesInWall bool
	// BurningTimeDelta is the body's burning_time attribute minus its base
	// of 1, so the zero value is an unmodified body; 1+delta scales fire
	// and lava ignitions.
	BurningTimeDelta float64
	// Raining reports the sky raining on the spot the body stands in,
	// vanilla's isRainingAt: a spot the sky reaches that the heightmap
	// leaves open and its biome precipitates as rain on.
	Raining bool
	// Sneaking reports the sneak key held, vanilla's isSteppingCarefully,
	// which walks a magma block unseared.
	Sneaking bool
	// Moving reports the tick's known movement as a step a berry bush
	// pricks (BerryBushMoving).
	Moving bool
	// OnGround reports the body standing on something, which is the only
	// way the block under the feet gets to sear it.
	OnGround bool
	// InLava reports the body's box touching lava, during which a fall
	// counts half.
	// vanilla: Entity.baseTick
	InLava bool
	// FreezeImmune reports a freeze_immune_wearables piece in an armour
	// slot, or a spectator player; either holds the frozen counter at 0, as
	// freeze_immune_entity_types does for a mob.
	// vanilla: Entity.canFreeze, LivingEntity.canFreeze
	FreezeImmune bool
	// Cauldrons are the water and powder-snow cauldrons the body's own box
	// overlaps, each with where it stands: a burning body inside one loses
	// its fire and the cauldron drops a level (Douse).
	Cauldrons []CauldronCell
	// PowderSnows are the cells of powder snow the body's box overlaps: a
	// burning body inside puts its fire out and melts them (Douse).
	PowderSnows []CauldronCell
}

// CauldronCell is a layered cauldron the body's box overlaps, at the cell it
// stands in: what a Douse hands back to drop a level.
type CauldronCell struct {
	X, Y, Z int32
}

// Douse is what one tick put out. Cauldrons is set by any fire a cauldron
// took; PowderSnows are the cells a burning body melted; Sound is set only
// for a body already burning when the blocks began, since vanilla fizzes for
// the burn the blocks found, not one a fire block lit this tick.
type Douse struct {
	Sound       bool
	Cauldrons   []CauldronCell
	PowderSnows []CauldronCell
}

// IsLava reports whether a block state is lava, the fluid that burns what
// it touches rather than carrying it.
func IsLava(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockLava
}

// IsLavaCauldron reports a lava cauldron, which ignites and burns like lava
// but is not a fluid, so the burn tick still bites there.
// vanilla: LavaCauldronBlock.entityInside
func IsLavaCauldron(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockLavaCauldron
}

// IsFire reports whether a block state is a fire — the plain one or the
// soul fire — the block that burns what stands in it. The two ignite alike
// but the soul fire bites twice as hard.
func IsFire(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockFire || b == registry.BlockSoulFire
}

// campfireBlocks marks the minecraft:campfires tag.
var campfireBlocks = registry.BlockTagOf("minecraft:campfires")

// CampfireDamage is a lit campfire's damage to a body inside: 1, or 2 for
// a soul campfire; false otherwise.
// vanilla: CampfireBlock.entityInside, Blocks.CAMPFIRE, SOUL_CAMPFIRE
func CampfireDamage(state uint16) (float64, bool) {
	b := registry.BlockOf(state)
	if !b.InTag(campfireBlocks) || !registry.StateLit(state) {
		return 0, false
	}
	if b == registry.BlockSoulCampfire {
		return 2, true
	}
	return 1, true
}

// IsSoulFire reports whether a fire block state is the soul fire, whose
// flame bites double.
func IsSoulFire(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockSoulFire
}

// IsCactus reports whether a block state is a cactus, which pricks what
// stands against it.
func IsCactus(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockCactus
}

// IsHotFloor reports whether a block state is a magma block, which sears
// what stands on it.
func IsHotFloor(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockMagmaBlock
}

// IsRedstoneOre reports whether a block state is a redstone ore, lit or not,
// the deepslate one included.
func IsRedstoneOre(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockRedstoneOre || b == registry.BlockDeepslateRedstoneOre
}

// IsSweetBerryBush reports whether a block state is a sweet berry bush.
func IsSweetBerryBush(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockSweetBerryBush
}

// IsWitherRose reports whether a block state is a wither rose, which withers
// a body standing in it.
func IsWitherRose(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockWitherRose
}

// IsTrampledCrop reports whether a ravager inside the block destroys it: the
// CropBlock subclasses and the pitcher crop. The crops tag also holds the
// stems, which are not CropBlocks.
// vanilla: CropBlock.entityInside, PitcherCropBlock.entityInside
func IsTrampledCrop(state uint16) bool {
	switch registry.BlockOf(state) {
	case registry.BlockWheat, registry.BlockCarrots, registry.BlockPotatoes, registry.BlockBeetroots,
		registry.BlockTorchflowerCrop, registry.BlockPitcherCrop:
		return true
	}
	return false
}

// eyeblossomPoisonTicks is the poison an open eyeblossom gives a bee.
// vanilla: EyeblossomBlock.entityInside
const eyeblossomPoisonTicks = 25

// EyeblossomPoison is the poison a bee inside an open eyeblossom gets: none
// in peaceful or once it has the effect. The caller found the flower in the
// bee's cells.
// vanilla: EyeblossomBlock.entityInside
func EyeblossomPoison(difficulty uint8, state uint16, hasPoison bool) (effect.Instance, bool) {
	if difficulty == DifficultyPeaceful || hasPoison || !BeeAttracts(state) {
		return effect.Instance{}, false
	}
	return effect.New(effectPoison, eyeblossomPoisonTicks, 0, false, true, true), true
}

// witherRoseTicks is the forty ticks of wither, at amplifier 0, that a
// wither rose plants.
// vanilla: WitherRoseBlock.entityInside
const witherRoseTicks = 40

// WitherRoseEffect is a wither rose's wither: none in peaceful or for a
// wither-immune body. The caller found the flower in the body's cells.
// vanilla: WitherRoseBlock.entityInside
func WitherRoseEffect(difficulty uint8, invulnerableToWither bool) (effect.Instance, bool) {
	if difficulty == DifficultyPeaceful || invulnerableToWither {
		return effect.Instance{}, false
	}
	return effect.New(effectWither, witherRoseTicks, 0, false, true, true), true
}

// IsStuckIn reports the web and sweet berry bush, whose entityInside calls
// makeStuckInBlock and so writes a fall off whole. Powder snow sticks only
// at the feet (IsPowderSnow).
// vanilla: WebBlock.entityInside, SweetBerryBushBlock.entityInside
func IsStuckIn(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockCobweb || b == registry.BlockSweetBerryBush
}

// StuckMultiplier is the per-axis velocity multiplier a block hands
// makeStuckInBlock: a cobweb a quarter, its fall a twentieth (less under
// weaving); a berry bush only for a living body. Powder snow's is the
// caller's (feet block if living, any block if not).
// vanilla: WebBlock.entityInside, SweetBerryBushBlock.entityInside,
// PowderSnowBlock.entityInside
func StuckMultiplier(state uint16, living, weaving bool) (x, y, z float64, ok bool) {
	switch registry.BlockOf(state) {
	case registry.BlockCobweb:
		if living && weaving {
			return 0.5, 0.25, 0.5, true
		}
		return 0.25, float64(float32(0.05)), 0.25, true
	case registry.BlockSweetBerryBush:
		return float64(float32(0.8)), 0.75, float64(float32(0.8)), living
	case registry.BlockPowderSnow:
		return float64(float32(0.9)), 1.5, float64(float32(0.9)), true
	}
	return 0, 0, 0, false
}

// IsPowderSnow reports powder snow, which sticks a body only as its feet
// block (getInBlockState().is(this)).
// vanilla: PowderSnowBlock.entityInside
func IsPowderSnow(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockPowderSnow
}

// IsLayeredCauldron reports a water or powder-snow cauldron, which a burning
// body inside drops a level; empty and lava ones have no fill to drop.
// vanilla: LayeredCauldronBlock
func IsLayeredCauldron(state uint16) bool {
	b := registry.BlockOf(state)
	return b == registry.BlockWaterCauldron || b == registry.BlockPowderSnowCauldron
}

// FreezeHurt is the hurt a fully frozen body takes: one half-heart, or the
// five vanilla's freeze_hurts_extra_types take.
// vanilla: LivingEntity.aiStep's freeze hurt
func FreezeHurt(hurtsExtra bool) float64 {
	if hurtsExtra {
		return freezeExtra
	}
	return freezeDmg
}

// FreezeImmuneWearables reports a freeze_immune_wearables piece (worn
// leather among them), which keeps the frozen ticks from starting. The
// caller passes only armour slots.
// vanilla: Entity.canFreeze over ItemTags.FREEZE_IMMUNE_WEARABLES
func FreezeImmuneWearables(stacks ...protocol.ItemStack) bool {
	for _, stack := range stacks {
		if !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDFreezeImmuneWearables) {
			return true
		}
	}
	return false
}

// FreezeTicksStep advances the frozen counter a tick: up one in powder snow
// when freezable, capped at FullyFrozenTicks, else down two, floored at 0.
// vanilla: InsideBlockEffectType.FREEZE, LivingEntity.aiStep
func FreezeTicksStep(ticks int, inSnow, canFreeze bool) int {
	if inSnow && canFreeze {
		return min(FullyFrozenTicks, ticks+1)
	}
	return max(0, ticks-FreezeThawTicks)
}

// TickArrowCount works off one tick of the arrows stuck in a body.
// vanilla: LivingEntity.tick
func TickArrowCount(v *player.Vitals) {
	StuckStep(&v.ArrowCount, &v.RemoveArrowTime)
}

// TickStingerCount works off one tick of the bee stingers stuck in a body,
// on the arrows' schedule.
// vanilla: LivingEntity.tick
func TickStingerCount(v *player.Vitals) {
	StuckStep(&v.StingerCount, &v.RemoveStingerTime)
}

// StuckStep works off one tick of a count of arrows or stingers stuck in a
// body: one falls out every 20 * (30 - count) ticks, so the more there are
// the faster they go.
// vanilla: LivingEntity.tick
func StuckStep(count, timer *int) {
	if *count <= 0 {
		return
	}
	if *timer <= 0 {
		*timer = 20 * (30 - *count)
	}
	*timer--
	if *timer <= 0 {
		*count--
	}
}

// TickFreeze steps the counter and returns the bite a fully frozen body is
// owed every 40th tick of age. Holding the bite back from an unhurtable mode
// is the caller's; the counter runs regardless, so creative's overlay still
// climbs.
// vanilla: InsideBlockEffectType.FREEZE, LivingEntity.aiStep
func TickFreeze(v *player.Vitals, inSnow, canFreeze bool) []Hurt {
	v.TicksFrozen = FreezeTicksStep(v.TicksFrozen, inSnow, canFreeze)
	if v.TicksFrozen >= FullyFrozenTicks && v.AgeTicks%FreezeHurtTicks == 0 {
		return []Hurt{{DamageTypeFreeze, FreezeHurt(false)}}
	}
	return nil
}

// FreezeSpeedAmount is powder snow's transient movement_speed ADD_VALUE:
// -0.05 times the frozen share, flat whatever the body's pace (half a
// player's 0.1 base, a quarter of a cow's 0.2).
// vanilla: LivingEntity.tryAddFrost
func FreezeSpeedAmount(ticksFrozen int) float64 {
	return -freezeSlow * float64(ticksFrozen) / float64(FullyFrozenTicks)
}

// IsAir reports whether a block state is air.
func IsAir(state uint16) bool { return state == airState }

// BerryBushBears reports whether a berry bush state carries berries, the
// age property above nought that is the only kind that pricks: a bush
// still growing lets a body walk through it untouched.
func BerryBushBears(state uint16) bool {
	age := registry.StateValue(state, registry.PropAge)
	return age != "" && age != "0"
}

// IsSuffocating reports whether a block at the eyes suffocates: by default a
// full collision shape that blocks motion (IsSolid; a chorus flower is a full
// cube but never suffocates), minus named exemptions, plus the four partial
// blocks registered with isSuffocating(Blocks::always).
// vanilla: BlockBehaviour.BlockStateBase.isSuffocating
func IsSuffocating(state uint16) bool {
	if int(state) >= registry.StateCount {
		return false
	}
	b := registry.BlockOf(state)
	if alwaysSuffocating(b) {
		return true
	}
	return registry.CollisionShapeFull(state) && registry.IsSolid(state) && !neverSuffocatingBlocks[b]
}

// alwaysSuffocating names the four blocks whose collision stops below the
// cell's top yet suffocate, so the eye box is tested against that top
// (EyesInWall).
// vanilla: Blocks' isSuffocating(Blocks::always) registrations
func alwaysSuffocating(b registry.Block) bool {
	switch b {
	case registry.BlockSoulSand, registry.BlockMud, registry.BlockFarmland, registry.BlockDirtPath:
		return true
	}
	return false
}

// neverSuffocatingBlocks marks the blocks vanilla exempts from suffocation
// despite their full collision — isSuffocating(Blocks::never).
// vanilla: Blocks' NOT_EXTENDED_PISTON and isSuffocating overrides
var neverSuffocatingBlocks = registry.BlocksOf(neverSuffocating)

func neverSuffocating(b registry.Block) bool {
	switch {
	case b == registry.BlockGlass, b == registry.BlockTintedGlass,
		b == registry.BlockMangroveRoots, b == registry.BlockCopperGrate,
		b.InGroup(registry.GroupStainedGlass),
		b.InGroup(registry.GroupGrateVariant):
		return true
	}
	return b.IsLeaves()
}

// BlockAt is the one cell a body's eye walk reads: the block state at a
// world coordinate.
type BlockAt interface {
	BlockAt(x, y, z int32) uint16
}

// CellRange returns the inclusive cell range a span overlaps: floor(from)
// to ceil(to)-1, as vanilla's BlockPos.betweenClosedStream walks a box.
func CellRange(from, to float64) (int32, int32) {
	return int32(math.Floor(from)), int32(math.Ceil(to)) - 1
}

// OnPos is the block supporting a body at (x, y, z): the cell 0.2 below the
// feet, or the block the feet box rests on when that cell has no collision
// (on a hay bale's edge, the hay). A fence, wall or gate there is the answer
// itself, its 1.5-high post leaving the cell below empty. halfWidth is the
// body's.
// vanilla: Entity.getOnPos, getOnPosLegacy
func OnPos(w BlockAt, x, y, z, halfWidth float64) uint16 {
	_, _, _, state := OnPosCell(w, x, y, z, halfWidth)
	return state
}

// OnPosCell is OnPos with the cell of the block it found; air gets the cell
// under the centre.
func OnPosCell(w BlockAt, x, y, z, halfWidth float64) (int32, int32, int32, uint16) {
	return onPosCell(w, x, y, z, halfWidth, false)
}

// FeetSupport is OnPos for a body at rest on its feet: a found block whose
// collision stops short of the feet (a shelf mushroom's short stalk beside
// its backing stone) gives way to a footprint block in the same row whose
// collision reaches them, so a slime or stone beside it decides the
// restitution.
// vanilla: Entity.getOnPos, mainSupportingBlockPos
func FeetSupport(w BlockAt, x, feetY, z, halfWidth float64) uint16 {
	_, _, _, state := onPosCell(w, x, feetY, z, halfWidth, true)
	return state
}

// onPosCell is the one support selector: the colliding cell under the
// centre, else the first in the footprint. With reach, a colliding cell
// whose shape stops short of y gives way to one that reaches it, and is
// the answer only when none does.
func onPosCell(w BlockAt, x, y, z, halfWidth float64, reach bool) (int32, int32, int32, uint16) {
	sx, sy, sz, sstate := standingBlock(w, x, y, z, halfWidth)
	if isPostBlock(sstate) {
		return sx, sy, sz, sstate
	}
	cy := int32(math.Floor(y - 0.2))
	cx, cz := int32(math.Floor(x)), int32(math.Floor(z))
	// Only a block with collision carries a body: a flower under the centre
	// gives way to the hay beside it, as a flower beside an edge supports
	// nothing.
	fx, fz, fallback := cx, cz, airState
	pick := func(xx, zz int32) bool {
		state := sstate
		if xx != sx || cy != sy || zz != sz {
			state = w.BlockAt(xx, cy, zz)
		}
		if registry.CollisionHeight(state) <= 0 {
			return false
		}
		if !reach || reachesFeet(state, cy, y) {
			fx, fz, fallback = xx, zz, state
			return true
		}
		if fallback == airState {
			fx, fz, fallback = xx, zz, state
		}
		return false
	}
	if pick(cx, cz) {
		return fx, cy, fz, fallback
	}
	minX, maxX := CellRange(x-halfWidth, x+halfWidth)
	minZ, maxZ := CellRange(z-halfWidth, z+halfWidth)
	for zz := minZ; zz <= maxZ; zz++ {
		for xx := minX; xx <= maxX; xx++ {
			if (xx != cx || zz != cz) && pick(xx, zz) {
				return fx, cy, fz, fallback
			}
		}
	}
	return fx, cy, fz, fallback
}

// reachesFeet reports whether a collision box of the state in row cellY
// tops out at the feet. A state whose shape is not a plain box list reaches
// when it collides at all; an empty shape never does.
func reachesFeet(state uint16, cellY int32, feetY float64) bool {
	boxes, plain := world.PlainCollision(state)
	if !plain {
		return registry.CollisionHeight(state) > 0
	}
	for _, b := range boxes {
		if float64(cellY)+b.MaxY >= feetY-1e-5 {
			return true
		}
	}
	return false
}

// isPostBlock reports whether a state is a fence, wall or fence gate, the
// blocks getOnPos returns whole when they are the main supporting block.
// The fences and walls are the tags in force; the gates are vanilla's
// FenceGateBlock class, which no tag changes.
func isPostBlock(state uint16) bool {
	b := registry.BlockOf(state)
	return b.InTag(registry.TagIDFences) || b.InTag(registry.TagIDWalls) || fenceGateBlocks[b]
}

var fenceGateBlocks = registry.BlocksInGroup(registry.GroupFenceGate)

// StandingBlock is the cell a body stands on: the first colliding cell the
// feet box overlaps 1e-5 below the feet, centre column first; else the cell
// below reaching the feet (a fence's 1.5 post); else the cell under the
// centre. The box scan stands in for mainSupportingBlockPos, which this
// server does not keep.
// vanilla: Entity.getOnPos, mainSupportingBlockPos
func StandingBlock(w BlockAt, x, y, z, halfWidth float64) (bx, by, bz int32) {
	bx, by, bz, _ = standingBlock(w, x, y, z, halfWidth)
	return bx, by, bz
}

// standingBlock is StandingBlock with the state it read at the answer, so a
// caller need not read the cell again. Reads each cell at most once per row.
func standingBlock(w BlockAt, x, y, z, halfWidth float64) (bx, by, bz int32, state uint16) {
	const eps = 1e-5
	cx, cz := int32(math.Floor(x)), int32(math.Floor(z))
	cy := int32(math.Floor(y - eps))
	minX, maxX := CellRange(x-halfWidth, x+halfWidth)
	minZ, maxZ := CellRange(z-halfWidth, z+halfWidth)
	// centre is the state read at (cx, cy, cz), kept for the miss answer.
	var centre uint16
	scan := func(yy int32, reach float64) (int32, int32, uint16, bool) {
		ok := func(xx, zz int32) (uint16, bool) {
			s := w.BlockAt(xx, yy, zz)
			h := registry.CollisionHeight(s)
			return s, h > 0 && float64(yy)+h >= reach
		}
		s, found := ok(cx, cz)
		if yy == cy {
			centre = s
		}
		if found {
			return cx, cz, s, true
		}
		for zz := minZ; zz <= maxZ; zz++ {
			for xx := minX; xx <= maxX; xx++ {
				if xx == cx && zz == cz {
					continue
				}
				if s, found := ok(xx, zz); found {
					return xx, zz, s, true
				}
			}
		}
		return 0, 0, 0, false
	}
	if xx, zz, s, found := scan(cy, math.Inf(-1)); found {
		return xx, cy, zz, s
	}
	if xx, zz, s, found := scan(cy-1, y-eps); found {
		return xx, cy - 1, zz, s
	}
	return cx, cy, cz, centre
}

// FrostSlowAir reports whether the block the frost slow reads under a body
// is air: OnPos on the ground, else the raw cell 0.2 below. A body falling
// through powder snow has no supporting block, but getBlockStateOnLegacy
// still reads the snow, so the slow stays.
// vanilla: LivingEntity.tryAddFrost, Entity.getOnPosLegacy
func FrostSlowAir(w BlockAt, x, y, z, halfWidth float64, onGround bool) bool {
	if onGround {
		return IsAir(OnPos(w, x, y, z, halfWidth))
	}
	return IsAir(w.BlockAt(int32(math.Floor(x)), int32(math.Floor(y-0.2)), int32(math.Floor(z))))
}

// EyesInWall reports whether a suffocating block's collision reaches the eye
// slab: 0.8 of the width and 1e-6 tall at eye height; any taller and a jump
// against a ceiling would count.
// vanilla: Entity.isInWall
func EyesInWall(w BlockAt, x, y, z, width, eyeHeight float64) bool {
	hw := width * 0.8 / 2
	const eyeHalf = 5e-7 // half of the 1e-6 the vanilla slab stands tall
	eyeY := y + eyeHeight
	minX, maxX := CellRange(x-hw, x+hw)
	minY, maxY := CellRange(eyeY-eyeHalf, eyeY+eyeHalf)
	minZ, maxZ := CellRange(z-hw, z+hw)
	for cy := minY; cy <= maxY; cy++ {
		for cz := minZ; cz <= maxZ; cz++ {
			for cx := minX; cx <= maxX; cx++ {
				state := w.BlockAt(cx, cy, cz)
				// The slab bites where the block's collision still
				// reaches it: a full cube fills to the crown, the four
				// isSuffocating(Blocks::always) blocks stop lower.
				if IsSuffocating(state) &&
					eyeY-eyeHalf < float64(cy)+registry.CollisionHeight(state) {
					return true
				}
			}
		}
	}
	return false
}

// igniteTicks is an ignition of ticks scaled by the body's burning_time.
// vanilla: LivingEntity.igniteForTicks
func (h Habitat) igniteTicks(ticks int) int {
	return ScaledIgniteTicks(ticks, 1+h.BurningTimeDelta)
}

// OnFire reports whether a body is burning: fire left on the counter, the
// state the on-fire metadata flag shows and the on-fire tick hurts from.
func OnFire(v *player.Vitals) bool { return v.FireTicks > 0 }

// setFireTicks sets the burn, held at one for creative and spectator so an
// unfelt burn ends the next tick.
// vanilla: Player.setRemainingFireTicks
func setFireTicks(v *player.Vitals, ticks int) {
	if ModeInvulnerable(v.GameMode) && ticks > 1 {
		ticks = 1
	}
	v.FireTicks = ticks
}

// IgniteForTicks raises the burn to at least ticks and thaws the freeze;
// Player's clamp holds an invulnerable body at one tick.
// vanilla: Entity.igniteForTicks, Player.setRemainingFireTicks
func IgniteForTicks(v *player.Vitals, ticks int) {
	if ticks > v.FireTicks {
		setFireTicks(v, ticks)
	}
	v.TicksFrozen = 0
}

// TickEnvironment runs a body's environmental tick: the burn, breath under
// water and against walls, and the blocks' blows. It updates the vitals in
// place and returns the blows in vanilla's order plus the tick's Douse. Burn,
// douses and breath run for every mode; an unhurtable mode only takes no new
// flame or block blow.
//
// rng rolls a player's fire-block ignition (one or two ticks of burn a
// second); nil means a mob, which piles nothing. oxygenBonus is the
// attribute respiration raises: the share of underwater ticks the breath is
// spared.
func TickEnvironment(v *player.Vitals, around Habitat, rng *rand.Rand, oxygenBonus float64) (hurts []Hurt, douse Douse) {
	// The invulnerable modes take no hurt and no new flame from the blocks;
	// the burn they already carry is Entity.baseTick's and still runs.
	invulnerable := ModeInvulnerable(v.GameMode)
	// Entity.baseTick's burn: deep in lava it does not bite (the lava's
	// own does), but the counter spends a tick every tick regardless.
	inLava := overlaps(around.Overlapped, IsLava)
	if v.FireTicks > 0 {
		if v.FireTicks%FireSecondTicks == 0 && !inLava {
			hurts = append(hurts, Hurt{DamageTypeOnFire, burnDamage})
		}
		setFireTicks(v, v.FireTicks-1)
	}

	// LivingEntity.baseTick: a block at the eyes suffocates and water at
	// the eyes drowns. Under water the air drops one a tick unless the
	// oxygen bonus spares it (decreaseAirSupply: spent only when a roll
	// against one over bonus-plus-one comes up short; always without a
	// bonus). At -20 it drowns for 2 and resets. The water-breathing
	// effects stop the drain; out of water the air refills 4 a tick;
	// breath of the nautilus alone holds it under water.
	if around.EyesInWall {
		hurts = append(hurts, Hurt{DamageTypeInWall, crushingDmg})
	}
	hurts = append(hurts, tickAir(v, around, rng, oxygenBonus, !invulnerable)...)

	// The powder snow's counter and bite are the caller's, run after the
	// blocks (TickFreeze): a fire lit this tick has already cleared the
	// freeze by then.

	// The blocks, applyEffectsFromBlocks's pass. Magma underfoot first
	// (stepOn): standing on it unsneaking sears for 1. Then each overlapped
	// cell: fire piles 1-2 ticks of burn on a player, holding it at 8 s
	// unless higher; lava holds it at 15 s at least. Either hurts whatever
	// the cooldown (a doused body takes fire's bites without flame), but
	// only a counter at 0 or above lights. Cactus and berry bush prick (the
	// bush only a moving body, only with berries). Water gathers a douse
	// rather than clearing the burn in place, so a body half in fire and
	// half in water ends as vanilla's collector ordering has it: every
	// block's effect, then the water's. Overlapped layered cauldrons take
	// the burn too, dropping a level.
	fireBefore := v.FireTicks
	if around.OnGround && !around.Sneaking && IsHotFloor(around.Ground) {
		hurts = append(hurts, Hurt{DamageTypeHotFloor, touchDamage})
	}
	inWater := false
	for _, state := range around.Overlapped {
		switch {
		case IsFire(state):
			if invulnerable {
				// The mode's clamp would leave at most a tick of flame
				// anyway; vanilla shows the body none of it.
				break
			}
			if v.FireTicks < 0 {
				v.FireTicks++
			} else if rng != nil {
				v.FireTicks += 1 + rng.Intn(2)
			}
			// Only a counter at 0 or above ignites, and so thaws; below is
			// the douse cooldown.
			// vanilla: BaseFireBlock.fireIgnite, Entity.igniteForTicks
			if v.FireTicks >= 0 {
				if ticks := around.igniteTicks(FireIgniteTicks); v.FireTicks < ticks {
					setFireTicks(v, ticks)
				}
				v.TicksFrozen = 0
			}
			amount := fireDamage
			if IsSoulFire(state) {
				amount = soulFireDmg
			}
			hurts = append(hurts, Hurt{DamageTypeInFire, amount})
		case IsLava(state), IsLavaCauldron(state):
			if invulnerable {
				break
			}
			if ticks := around.igniteTicks(LavaIgniteTicks); v.FireTicks < ticks {
				setFireTicks(v, ticks)
			}
			v.TicksFrozen = 0
			hurts = append(hurts, Hurt{DamageTypeLava, lavaDamage})
		case IsCactus(state):
			hurts = append(hurts, Hurt{DamageTypeCactus, touchDamage})
		case IsSweetBerryBush(state):
			if around.Moving && BerryBushBears(state) {
				hurts = append(hurts, Hurt{DamageTypeSweetBerryBush, touchDamage})
			}
		case IsWater(state):
			inWater = true
		default:
			if amount, lit := CampfireDamage(state); lit {
				hurts = append(hurts, Hurt{DamageTypeCampfire, amount})
			}
		}
	}
	if inWater && v.FireTicks > 0 {
		setFireTicks(v, 0)
		// Only a burn the body carried in fizzes, not one a fire block lit
		// this pass.
		// vanilla: Entity.applyEffectsFromBlocks
		if fireBefore > 0 {
			douse.Sound = true
		}
	}
	// Overlapped layered cauldrons take any burn, the fire block's
	// included, and drop a level. The fizz is only for the burn the blocks
	// found; a spectator has no block effects.
	// vanilla: LayeredCauldronBlock.handleEntityOnFireInside, Entity.isAffectedByBlocks
	if v.GameMode != protocol.GameModeSpectator && len(around.Cauldrons) > 0 && v.FireTicks > 0 {
		setFireTicks(v, 0)
		douse.Cauldrons = around.Cauldrons
		if fireBefore > 0 {
			douse.Sound = true
		}
	}

	// Powder snow melts under a burning player (no mob_griefing check for
	// players) and puts the fire out. The block effects skip a spectator.
	// vanilla: PowderSnowBlock.entityInside, InsideBlockEffectType.EXTINGUISH
	if v.GameMode != protocol.GameModeSpectator && len(around.PowderSnows) > 0 && v.FireTicks > 0 {
		setFireTicks(v, 0)
		douse.PowderSnows = around.PowderSnows
		if fireBefore > 0 {
			douse.Sound = true
		}
	}

	// The rain's douse, which vanilla takes up after the blocks: a body
	// the sky is falling on loses its burn the same way the water took
	// it, fizz and all.
	if around.Raining && v.FireTicks > 0 {
		setFireTicks(v, 0)
		if fireBefore > 0 {
			douse.Sound = true
		}
	}

	// End of the pass: a body no longer burning that gained no fire this
	// tick drops into the douse cooldown; one the blocks lit keeps its
	// fire.
	if v.FireTicks <= 0 && v.FireTicks <= fireBefore {
		setFireTicks(v, -DouseCooldown)
	}
	return hurts, douse
}

// tickAir is LivingEntity.baseTick's breath. canDrown (vanilla's
// canDrownInWater) is false for a mode-spared body, which spends no breath
// and takes no bite but still refills. A bubble column skips the air loss,
// so the breath refills there.
// vanilla: LivingEntity.baseTick
func tickAir(v *player.Vitals, around Habitat, rng *rand.Rand, oxygenBonus float64, canDrown bool) (hurts []Hurt) {
	if around.EyesInWater && !around.EyesInBubbleColumn && !around.WaterBreathing && canDrown {
		if oxygenBonus <= 0 || rng == nil || rng.Float64() < 1.0/(oxygenBonus+1.0) {
			v.AirSupply--
		}
		if v.AirSupply <= AirDrownFloor {
			v.AirSupply = 0
			hurts = append(hurts, Hurt{DamageTypeDrown, drownDamage})
		}
	} else if v.AirSupply < MaxAirSupply &&
		(!around.EyesInWater || around.EyesInBubbleColumn || !around.HoldAir) {
		v.AirSupply = min(v.AirSupply+airRefillPerTick, MaxAirSupply)
	}
	return hurts
}

// overlaps reports whether any of the gathered block states answers yes.
func overlaps(states []uint16, match func(uint16) bool) bool {
	return slices.ContainsFunc(states, match)
}

// RainReader is what the rain check reads of the world: whether the sky
// rains on a cell.
type RainReader interface {
	RainingAt(x, y, z int32) bool
}

// RainsOnBody reports whether the sky rains on the cell a body's feet stand
// in or the cell the top of its box, at height top, stands in.
// vanilla: Entity.isInRain
func RainsOnBody(w RainReader, x, y, z, top float64) bool {
	if w.RainingAt(int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))) {
		return true
	}
	return w.RainingAt(int32(math.Floor(x)), int32(math.Floor(top)), int32(math.Floor(z)))
}

// IsCobweb reports whether a block state is a cobweb.
func IsCobweb(state uint16) bool {
	return registry.BlockOf(state) == registry.BlockCobweb
}
