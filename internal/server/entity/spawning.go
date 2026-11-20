package entity

import (
	"math"
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// The natural spawner, ported from vanilla's NaturalSpawner (spawnForChunk,
// spawnCategoryForChunk, spawnCategoryForPosition and their checks). Each
// tick every chunk near a player rolls one random block, and a pack is
// spawned there only if every rule agrees. The pass reads the world through
// SpawnWorld and returns spawn requests for the server to create.

// SpawnWorld is the world as the spawn pass reads it: a mob's World plus
// the light and terrain the spawn checks need. Tests can substitute a stub.
type SpawnWorld interface {
	World

	// BlockLightAt is the block light level at the cell, 0 where the chunk
	// is not loaded.
	BlockLightAt(x, y, z int32) uint8

	// IsThundering is Level.isThundering (thunder level past 0.9), which eases
	// the darkness requirement.
	IsThundering() bool

	// Biome is the block's zoomed quart's biome (BiomeManager.getBiome), whose
	// spawn lists apply; the generator's biome where that quart is not loaded.
	Biome(x, y, z int32) registry.Biome
	// NoiseBiome is the block's own 4x4x4 biome cell, which environment
	// attributes that are not full-resolution read; NoBiome where not loaded.
	NoiseBiome(x, y, z int32) registry.Biome

	// SurfaceHeight is the WORLD_SURFACE heightmap's highest block y, or one
	// below the bottom for an empty or unloaded column.
	SurfaceHeight(x, z int32) int32

	// HasCeiling is DimensionType.hasCeiling (the Nether).
	HasCeiling() bool

	// MinY is the dimension's bottom y, the floor of the spawn's height roll.
	MinY() int32

	// Height is the dimension's height in blocks.
	Height() int32

	// ChunkLoadedAt reports whether the block's chunk is loaded, which decides
	// whether a pack that wandered across a chunk border may stand there.
	ChunkLoadedAt(x, z int32) bool

	// SpawnPoint is the world spawn and whether the rule keeping monsters away
	// from it applies.
	SpawnPoint() (x, y, z int32, guarded bool)

	// SkyDarken is Level.getSkyDarken: 0 on a clear day, 11 on a clear night,
	// weather in between.
	SkyDarken() int

	// SeedValue is the world seed (WorldGenLevel.getSeed), which seeds the
	// slime-chunk roll.
	SeedValue() int64

	// MoonPhase is the moon phase 0-7, 0 full and waning one per day; the
	// surface slime rule reads its chance from it.
	MoonPhase() int

	// SeaLevel is Level.getSeaLevel, from which surface water creatures' band
	// and underground ones' depth are measured: 63 in the seeded overworld,
	// one above the floor in superflat (whose waters never reach it).
	SeaLevel() int32

	// DimensionName is the dimension's wire name, which selects its type's
	// monster light rules.
	DimensionName() string
}

// ChunkPos identifies a chunk column by its chunk coordinates.
type ChunkPos struct {
	X, Z int32
}

// SpawnPlayer is a player the spawn pass measures against. Spectators are
// left out of every player check, matching vanilla
// (ChunkMap.playerIsCloseEnoughForSpawning refuses them, and the
// nearest-player lookups use EntitySelector.NO_SPECTATORS).
type SpawnPlayer struct {
	EntityID  int32
	X, Y, Z   float64
	Spectator bool
	// Width and Height are the bounding box of the player's pose and scale.
	Width, Height float64
	// Dead is a player on the death screen.
	Dead bool
}

// SpawnCounts are NaturalSpawner's SpawnState counts for the two mob caps,
// gathered once per pass and kept current by it: the dimension's
// non-persistent monsters for the global cap (mobCategoryCounts), and the
// same per nearby player for the local cap (LocalMobCapCalculator).
type SpawnCounts struct {
	Global int
	// PerPlayer is keyed by entity ID. A monster counts for every close-enough
	// player whose neighbourhood it spawned or stands in. Nil until one does.
	PerPlayer map[int32]int
}

// SpawnRequest is one monster to bring into the world at the rolled feet
// position and facing. Group is the attempt's SpawnGroupData, whose shared
// dice decide the pack's babies, passed to the server's finalize.
type SpawnRequest struct {
	Type       MobType
	X, Y, Z    float64
	Yaw        float32
	Group      *SpawnGroup
	Persistent bool
	// Region is a chunk-generation spawn's WorldGenRegion random, nil
	// elsewhere.
	Region Dice
}

// Pass numbers: MAGIC_NUMBER, the 17x17
// chunks of one player's spawn distance (17^2); the minimum player distance,
// isRightDistanceToPlayerAndSpawnPoint's 24 blocks squared (576.0); the
// despawn distance, 128 for every category but WATER_AMBIENT's 64; and
// getMaxSpawnClusterSize's 4 (fish 8 or 5), the most one pass spawns in a
// chunk.
const (
	spawnMagicNumber        = 17 * 17
	spawnMinDistanceSq      = 576.0
	waterAmbientDespawnDist = 64 // vanilla: MobCategory.WATER_AMBIENT
	monsterDespawnDistanceI = 128
	spawnClusterSize        = 4
)

// SpawnableBelow reports whether the block under a spawn position supports
// type t: the block's own isValidSpawn if it has one, else the default (top
// face sturdy and light emission under 14).
// vanilla: BlockBehaviour.Properties.isValidSpawn
func SpawnableBelow(state uint16, t MobType) bool {
	if _, _, ok := registry.BlockStateName(state); !ok {
		return false
	}
	b := registry.BlockOf(state)
	if b == registry.BlockWater || b == registry.BlockLava {
		return false
	}
	if valid, overridden := validSpawnOverride(b, t); overridden {
		return valid
	}
	if !registry.FaceSturdy(state, 1, registry.SupportFull) {
		return false
	}
	return registry.LightEmission(state) < 14
}

// validSpawnOverride is a block's own isValidSpawn for t; overridden is
// false for blocks on the default rule.
// vanilla: Blocks (isValidSpawn on glass, trapdoors, carved pumpkin, ...)
func validSpawnOverride(b registry.Block, t MobType) (valid, overridden bool) {
	switch b {
	case registry.BlockBedrock, registry.BlockGlass, registry.BlockTintedGlass, registry.BlockBarrier,
		registry.BlockMovingPiston, registry.BlockChorusFlower, registry.BlockScaffolding:
		return false, true
	case registry.BlockSoulSand, registry.BlockCarvedPumpkin, registry.BlockJackOLantern,
		registry.BlockRedstoneLamp, registry.BlockMud:
		return true, true
	case registry.BlockIce, registry.BlockFrostedIce:
		return t == registry.EntityTypePolarBear, true
	case registry.BlockMagmaBlock:
		return kindOf(t).fireImmune, true
	}
	switch {
	case leavesNamedBlocks[b]:
		return t == TypeOcelot || t == TypeParrot, true
	case spawnRefusedBlocks[b]:
		return false, true
	}
	return false, false
}

// leavesNamedBlocks marks the blocks whose name ends in _leaves.
var leavesNamedBlocks = registry.BlocksInGroup(registry.GroupLeaves)

// spawnRefusedBlocks marks the stained glass, trapdoors and copper grates,
// whose isValidSpawn refuses every mob.
var spawnRefusedBlocks = registry.BlocksOf(func(b registry.Block) bool {
	return b.InGroup(registry.GroupStainedGlass) || b.InGroup(registry.GroupTrapdoor) ||
		b.InGroup(registry.GroupGrateVariant) || b == registry.BlockCopperGrate
})

// batsSpawnableOn marks the blocks of the bats_spawnable_on tag.
var batsSpawnableOn = registry.BlockTagOf("minecraft:bats_spawnable_on")

// emptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock: not a full
// collision block, not a signal source, no fluid (a waterlogged block holds
// water), not in minecraft:prevent_mob_spawning_inside (the rails), and not
// dangerous for this type (EntityType.isBlockDangerous).
func emptySpawnBlock(t MobType, state uint16) bool {
	return emptySpawnCell(t, state, true)
}

// emptySpawnCell optionally checks the fluid occupying the cell.
func emptySpawnCell(t MobType, state uint16, fluid bool) bool {
	if int(state) >= registry.StateCount {
		return true
	}
	if registry.CollisionShapeFull(state) || registry.IsRedstoneSignalSourceState(state) ||
		fluid && blocklogic.FluidOf(state).Kind != blocklogic.FluidNone {
		return false
	}
	return !registry.BlockOf(state).IsPreventMobSpawningInside() && !spawnBlockDangerous(t, state)
}

// vanilla: EntityTypes builders' immuneTo tags, EntityType.isBlockDangerous
var defaultSpawnImmuneTag = registry.BlockTagOf("minecraft:default_immune_to")

var spawnImmuneTags = func() [registry.EntityTypeCount]registry.BlockTagID {
	var tags [registry.EntityTypeCount]registry.BlockTagID
	for i := range tags {
		tags[i] = defaultSpawnImmuneTag
	}
	tags[TypeFox] = registry.BlockTagOf("minecraft:fox_immune_to")
	tags[TypePolarBear] = registry.BlockTagOf("minecraft:polar_bear_immune_to")
	tags[TypeSnowGolem] = registry.BlockTagOf("minecraft:snow_golem_immune_to")
	tags[TypeStray] = registry.BlockTagOf("minecraft:stray_immune_to")
	tags[TypeWitherSkeleton] = registry.BlockTagOf("minecraft:wither_skeleton_immune_to")
	return tags
}()

func spawnBlockDangerous(t MobType, state uint16) bool {
	b := registry.BlockOf(state)
	burning := registry.IsBurningBlock(state)
	switch b {
	case registry.BlockWitherRose, registry.BlockSweetBerryBush, registry.BlockCactus, registry.BlockPowderSnow:
	default:
		if !burning {
			return false
		}
	}
	tag := defaultSpawnImmuneTag
	if uint32(t) < uint32(len(spawnImmuneTags)) {
		tag = spawnImmuneTags[t]
	}
	if b.InTag(tag) {
		return false
	}
	return !burning || !kindOf(t).fireImmune
}

// originalSpawnPositionOk is the placement a first-generation spawn checks:
// IN_LAVA for the strider, ON_GROUND for everything else.
// vanilla: SpawnPlacements
func originalSpawnPositionOk(w SpawnWorld, t MobType, x, y, z int32) bool {
	if t == TypeStrider {
		return rules.IsLava(w.BlockAt(x, y, z))
	}
	return unrestrictedPlacement(t) || spawnPositionOk(w, t, x, y, z)
}

// unrestrictedPlacement reports the kinds registered with NO_RESTRICTIONS, the
// only ones a biome spawn list names: no check of the cell or the block below.
// vanilla: SpawnPlacements (FOX, PANDA), SpawnPlacementTypes.NO_RESTRICTIONS
func unrestrictedPlacement(t MobType) bool {
	return t == TypeFox || t == TypePanda
}

// spawnPositionOk is SpawnPlacementTypes.ON_GROUND's isSpawnPositionOk (the
// zombie's placement, over MOTION_BLOCKING_NO_LEAVES): the block below
// supports the mob, and the block and the one above are empty spawn blocks.
func spawnPositionOk(w SpawnWorld, t MobType, x, y, z int32) bool {
	if !w.InBorder(x, z) {
		return false
	}
	if !SpawnableBelow(w.BlockAt(x, y-1, z), t) {
		return false
	}
	return emptySpawnBlock(t, w.BlockAt(x, y, z)) && emptySpawnBlock(t, w.BlockAt(x, y+1, z))
}

// RaidSpawnSpotOk reports whether a raid wave can spawn on these blocks.
// Snow below air bypasses the normal placement checks, including the head cell.
// vanilla: Raid.findRandomSpawnPos
func RaidSpawnSpotOk(below, at, above uint16) bool {
	return SpawnableBelow(below, TypeRavager) && emptySpawnBlock(TypeRavager, at) && emptySpawnBlock(TypeRavager, above) ||
		registry.BlockOf(below) == registry.BlockSnow && world.IsAirState(at)
}

// skyDarkenForSpawn is the darkening the spawn brightness uses: getSkyDarken
// when clear, isDarkEnoughToSpawn's fixed 10 during a thunderstorm.
func skyDarkenForSpawn(w SpawnWorld) int {
	if w.IsThundering() {
		return 10
	}
	return w.SkyDarken()
}

// isDarkEnoughToSpawn is Monster.isDarkEnoughToSpawn for a natural spawn
// (via checkMonsterSpawnRules): the raw sky light must lose to a 32-sided
// die first; block light must not exceed the dimension's
// monster_spawn_block_light_limit (0 in the overworld, so any block light
// refuses); and the brightest light must not exceed
// monster_spawn_light_level (the overworld's a uniform 0 to 7).
func isDarkEnoughToSpawn(w SpawnWorld, x, y, z int32, rng Dice) bool {
	if int32(w.SkyLightAt(x, y, z)) > rng.NextIntN(32) {
		return false
	}
	blockLimit, lightLevel := monsterSpawnLight(w)
	if blockLimit < 15 && int(w.BlockLightAt(x, y, z)) > blockLimit {
		return false
	}
	bright := int(w.SkyLightAt(x, y, z)) - skyDarkenForSpawn(w)
	if b := int(w.BlockLightAt(x, y, z)); b > bright {
		bright = b
	}
	if lightLevel < 0 {
		// minecraft:uniform over 0..7, both ends included.
		return int32(bright) <= rng.NextIntN(8)
	}
	return bright <= lightLevel
}

// monsterSpawnLight is the dimension type's monster_spawn_block_light_limit
// and fixed monster_spawn_light_level, -1 for the overworld's uniform 0..7.
func monsterSpawnLight(w SpawnWorld) (blockLimit, lightLevel int) {
	switch w.DimensionName() {
	case world.NetherName:
		return 15, 7
	case world.EndName:
		return 0, 15
	}
	return 0, -1
}

// slimeChunkSalt is the salt WorldgenRandom.seedSlimeChunk folds the world's
// seed and the chunk's coordinates with.
const slimeChunkSalt = 987234911

// surfaceSlimeBiomes are the biomes the minecraft:allows_surface_slime_spawns
// tag holds, the only places the slime's surface branch runs.
var surfaceSlimeBiomes = [registry.BiomeCount]bool{
	registry.BiomeSwamp:         true,
	registry.BiomeMangroveSwamp: true,
}

func isSurfaceSlimeBiome(b registry.Biome) bool {
	return b >= 0 && b < registry.BiomeCount && surfaceSlimeBiomes[b]
}

// surfaceSlimeChance is minecraft:gameplay/surface_slime_spawn_chance by
// moon phase: 0.5 at full moon down to 0 at new moon.
var surfaceSlimeChance = [8]float32{0.5, 0.375, 0.25, 0.125, 0, 0.125, 0.25, 0.375}

// legacyRandom is RandomSource.createThreadLocalInstance's java.util.Random
// arithmetic.
type legacyRandom struct{ seed int64 }

func newLegacyRandom(seed int64) *legacyRandom {
	return &legacyRandom{seed: (seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *legacyRandom) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(uint64(r.seed) >> (48 - bits))
}

// nextFloat is BitRandomSource.nextFloat.
func (r *legacyRandom) nextFloat() float32 { return float32(r.next(24)) / (1 << 24) }

// nextInt is RandomSource.nextInt for positive bounds: the top 31 bits
// modulo n.
func (r *legacyRandom) nextInt(n int32) int32 { return r.next(31) % n }

// seedSlimeChunk is WorldgenRandom.seedSlimeChunk: the seed with the chunk
// coordinates mixed in by its constants and salt. The squares and products
// are 32-bit and only then widened to long, as Java's i2l does.
func seedSlimeChunk(cx, cz int32, seed int64) *legacyRandom {
	stirred := seed +
		int64(cx*cx*4987142) +
		int64(cx*5947611) +
		int64(cz*cz)*4392871 +
		int64(cz*389711)
	return newLegacyRandom(stirred ^ slimeChunkSalt)
}

// slimeSpawnRules is Slime.checkSlimeSpawnRules, used instead of the
// darkness rule. The surface branch runs only in
// allows_surface_slime_spawns biomes between y 50 and 70: a die against the
// moon phase's chance, then the raw light must lose to an 8-sided die.
// Otherwise the slime-chunk branch: the chunk's own 1-in-10, the world
// dice's 1-in-10, and y under 40. Both end in Mob.checkMobSpawnRules (the
// block below supports it; no light check). Dice order follows the
// bytecode: the surface light die only if its chance die opened; the
// chunk's dice before the world's, whose roll is thrown regardless.
func slimeSpawnRules(w SpawnWorld, rng Dice, x, y, z int32) bool {
	if isSurfaceSlimeBiome(w.Biome(x, y, z)) && y > 50 && y < 70 {
		if rng.NextFloat() < surfaceSlimeChance[w.MoonPhase()] &&
			int32(w.MaxLocalRawLightAt(x, y, z)) <= rng.NextIntN(8) {
			return SpawnableBelow(w.BlockAt(x, y-1, z), registry.EntityTypeSlime)
		}
	}
	isSlimeChunk := seedSlimeChunk(x>>4, z>>4, w.SeedValue()).nextInt(10) == 0
	return rng.NextIntN(10) == 0 && isSlimeChunk && y < 40 && SpawnableBelow(w.BlockAt(x, y-1, z), registry.EntityTypeSlime)
}

// boxCollidesBlocks is ServerLevel.noCollision's block half (inverted) for
// the type's spawn box, EntityType.getSpawnAABB scaled by the builder's
// spawnDimensionsScale and centred on the position.
func boxCollidesBlocks(w SpawnWorld, t MobType, x, y, z float64) bool {
	k := kindOf(t)
	width, height := k.width, k.height
	if k.spawnScale > 0 {
		width, height = width*k.spawnScale, height*k.spawnScale
	}
	minX := int32(math.Floor(x - width/2))
	maxX := int32(math.Floor(x + width/2))
	minY := int32(math.Floor(y))
	maxY := int32(math.Floor(y + height))
	minZ := int32(math.Floor(z - width/2))
	maxZ := int32(math.Floor(z + width/2))
	for cx := minX; cx <= maxX; cx++ {
		for cy := minY; cy <= maxY; cy++ {
			for cz := minZ; cz <= maxZ; cz++ {
				if blockExtent(w.BlockAt(cx, cy, cz)) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// boxInLiquid is LevelReader.containsAnyLiquid as Mob.checkSpawnObstruction
// reads it: ON_GROUND monsters do not spawn in water.
func boxInLiquid(w SpawnWorld, t MobType, x, y, z float64) bool {
	k := kindOf(t)
	minX := int32(math.Floor(x - k.width/2))
	maxX := int32(math.Floor(x + k.width/2))
	minY := int32(math.Floor(y))
	maxY := int32(math.Floor(y + k.height))
	minZ := int32(math.Floor(z - k.width/2))
	maxZ := int32(math.Floor(z + k.width/2))
	for cx := minX; cx <= maxX; cx++ {
		for cy := minY; cy <= maxY; cy++ {
			for cz := minZ; cz <= maxZ; cz++ {
				if state := w.BlockAt(cx, cy, cz); world.HoldsFluid(state) {
					return true
				}
			}
		}
	}
	return false
}

// SpawnBody is the Entity.getBoundingBox of a blocksBuilding body (player,
// mob, primed TNT, falling block or boat) that a new mob's obstruction test
// checks against; the box is all EntityGetter.isUnobstructed reads.
type SpawnBody struct {
	MinX, MinY, MinZ float64
	MaxX, MaxY, MaxZ float64
}

// boxOverlapsBody is Mob.checkSpawnObstruction's entity half: the spawn box
// must not intersect any blocksBuilding body (EntityGetter.isUnobstructed).
func boxOverlapsBody(bodies []SpawnBody, box SpawnBody) bool {
	for i := range bodies {
		b := &bodies[i]
		if b.MinX < box.MaxX && b.MaxX > box.MinX &&
			b.MinY < box.MaxY && b.MaxY > box.MinY &&
			b.MinZ < box.MaxZ && b.MaxZ > box.MinZ {
			return true
		}
	}
	return false
}

// spawnBox is the box a type-t body at the position would fill.
func spawnBox(t MobType, x, y, z float64) SpawnBody {
	k := kindOf(t)
	return SpawnBody{
		MinX: x - k.width/2, MinY: y, MinZ: z - k.width/2,
		MaxX: x + k.width/2, MaxY: y + k.height, MaxZ: z + k.width/2,
	}
}

var axolotlGround = registry.BlockTagOf("minecraft:axolotls_spawnable_on")

// SpawnCategory is vanilla's MobCategory, naming the biome lists and spawn
// rules a pass uses.
type SpawnCategory = registry.MobCategory

const (
	CategoryMonster          = registry.MobCategoryMonster
	CategoryCreature         = registry.MobCategoryCreature
	CategoryWaterCreature    = registry.MobCategoryWaterCreature
	CategoryUndergroundWater = registry.MobCategoryUndergroundWaterCreature
	CategoryAmbient          = registry.MobCategoryAmbient
	CategoryWaterAmbient     = registry.MobCategoryWaterAmbient
	CategoryAxolotls         = registry.MobCategoryAxolotls

	// SpawnCategoryCount is the number of categories, for arrays indexed by one.
	SpawnCategoryCount = int(registry.MobCategoryCount)
)

// spawnListsOf are the biome spawn lists a category is rolled from, or nil for
// categories that have no natural spawn list (MobCategoryMisc).
func spawnListsOf(c SpawnCategory) *[registry.BiomeCount][]spawnData {
	switch c {
	case CategoryMonster:
		return &biomeMonsterSpawns
	case CategoryCreature:
		return &biomeCreatureSpawns
	case CategoryWaterCreature:
		return &biomeWaterSpawns
	case CategoryUndergroundWater:
		return &biomeUndergroundWaterSpawns
	case CategoryAmbient:
		return &biomeAmbientSpawns
	case CategoryWaterAmbient:
		return &biomeWaterAmbientSpawns
	case CategoryAxolotls:
		return &biomeAxolotlSpawns
	}
	return nil
}

// spawnData is MobSpawnSettings.SpawnerData: type, weight and pack size
// span.
type spawnData struct {
	mob                MobType
	weight             int
	minCount, maxCount int
}

// spawnCost is a biome's MobSpawnSettings.MobSpawnCost for one mob type.
type spawnCost struct {
	mob    MobType
	charge float64
	budget float64
}

// spawnCostAt is the biome's spawn cost for the type.
func spawnCostAt(biome registry.Biome, t MobType) (spawnCost, bool) {
	return currentBiomeSpawns().costAt(biome, t)
}

// HasSpawnCost reports whether any biome gives the type a spawn cost.
func HasSpawnCost(t MobType) bool {
	return t >= 0 && t < registry.EntityTypeCount && currentBiomeSpawns().spawnCostTypes[t]
}

// SpawnChargeOf is the charge a mob of the type adds in the biome it stands in.
func SpawnChargeOf(biome registry.Biome, t MobType) (float64, bool) {
	c, ok := spawnCostAt(biome, t)
	return c.charge, ok
}

// SpawnPotential is vanilla's PotentialCalculator: the charges of the mobs
// standing in costed biomes, which limit how closely another may spawn.
type SpawnPotential []spawnCharge

type spawnCharge struct {
	x, y, z int32
	charge  float64
}

// Add records a charge at the block; a zero charge changes nothing.
func (p *SpawnPotential) Add(x, y, z int32, charge float64) {
	if charge != 0 {
		*p = append(*p, spawnCharge{x, y, z, charge})
	}
}

// EnergyChange is the potential a new charge at the block adds: charge times
// each stored charge over the distance between block corners, +Inf at 0.
// vanilla: PotentialCalculator.getPotentialEnergyChange
func (p SpawnPotential) EnergyChange(x, y, z int32, charge float64) float64 {
	if charge == 0 {
		return 0
	}
	var sum float64
	for _, c := range p {
		dx, dy, dz := float64(x-c.x), float64(y-c.y), float64(z-c.z)
		d := dx*dx + dy*dy + dz*dz
		if d == 0 {
			return math.Inf(1)
		}
		sum += c.charge / math.Sqrt(d)
	}
	return charge * sum
}

// pickSpawnData draws from the position's list (NaturalSpawner.getRandomSpawnMobAt).
func pickSpawnData(w SpawnWorld, rng *rand.Rand, x, y, z int32, category SpawnCategory) *spawnData {
	biome := w.Biome(x, y, z)
	// The rivers hold back 98 in 100 water-ambient rolls.
	// vanilla: NaturalSpawner.getRandomSpawnMobAt, BiomeTags.REDUCED_WATER_AMBIENT_SPAWNS
	if category == CategoryWaterAmbient && biome.InTag(registry.BiomeTagIDReduceWaterAmbientSpawns) && rng.Float32() < 0.98 {
		return nil
	}
	return pickFromList(spawnListAt(w, x, y, z, category, biome), GoDice{rng})
}

// fortressEnemies is the monster list of a nether fortress.
// vanilla: NetherFortressStructure.FORTRESS_ENEMIES
var fortressEnemies = []spawnData{
	{TypeBlaze, 10, 2, 3},
	{TypeZombifiedPiglin, 5, 4, 4},
	{TypeWitherSkeleton, 8, 5, 5},
	{TypeSkeleton, 2, 5, 5},
	{TypeMagmaCube, 3, 4, 4},
}

// fortressWorld is a world that knows where its nether fortresses stand.
type fortressWorld interface {
	FortressAt(x, y, z int32) (inBounds, inPiece bool)
}

// spawnListAt applies fortress and structure overrides before the biome list.
// vanilla: NaturalSpawner.isInNetherFortressBounds, ChunkGenerator.getMobsAt
func spawnListAt(w SpawnWorld, x, y, z int32, category SpawnCategory, biome registry.Biome) []spawnData {
	c := naturalSpawnContext{w: w}
	return c.listAt(x, y, z, category, biome).entries
}

// spawnListFor is the category's biome list, nil for NoBiome.
func spawnListFor(category SpawnCategory, biome registry.Biome) []spawnData {
	if biome < 0 || biome >= registry.BiomeCount {
		return nil
	}
	return currentBiomeSpawns().lists[category][biome].entries
}

// canSpawnMobAt tests value membership (NaturalSpawner.canSpawnMobAt).
func canSpawnMobAt(w SpawnWorld, x, y, z int32, category SpawnCategory, data *spawnData) bool {
	return slices.Contains(spawnListAt(w, x, y, z, category, w.Biome(x, y, z)), *data)
}

// pickFromList is the weighted roll: one draw against the total weight.
func pickFromList(list []spawnData, rng Dice) *spawnData {
	total := 0
	for i := range list {
		total += list[i].weight
	}
	if total <= 0 {
		return nil
	}
	roll := int(rng.NextIntN(int32(total)))
	for i := range list {
		roll -= list[i].weight
		if roll < 0 {
			return &list[i]
		}
	}
	return nil
}

// MonsterKind reports whether a mob type is a monster-category (hostile) kind.
func MonsterKind(t MobType) bool { return kindOf(t).hostile }

// babyDropsLoot reports whether a baby of the kind still rolls its loot: the
// monster-category kinds that extend Monster, not the Animal-like ones.
// vanilla: LivingEntity.shouldDropLoot, Monster.shouldDropLoot
func babyDropsLoot(t MobType) bool {
	switch t {
	case TypeHoglin, TypeZombieHorse, TypeZombieNautilus, TypeSulfurCube, TypeCamelHusk:
		return false
	}
	return MonsterKind(t)
}

// Enemy reports vanilla's Enemy kinds: the monster-category kinds but the
// zombie horse, a plain AbstractHorse.
func Enemy(t MobType) bool { return kindOf(t).enemy(t) }

// enemy is Enemy for t, whose kinds entry k is.
func (k *kind) enemy(t MobType) bool {
	return k.hostile && t != TypeZombieHorse && t != TypeZombieNautilus && t != TypeSulfurCube && t != TypeCamelHusk
}

// GoneInPeaceful reports kinds peaceful refuses and removes: monsters,
// except the few without notInPeaceful.
// vanilla: EntityType.canSpawn, Mob.checkDespawn
func GoneInPeaceful(t MobType) bool { return kindOf(t).goneInPeaceful() }

// goneInPeaceful is GoneInPeaceful for k.
func (k *kind) goneInPeaceful() bool { return k.hostile && !k.livesInPeaceful }

// MonsterClassKind reports types extending Monster: the enemies minus those
// that implement Enemy on another base class.
// vanilla: Slime, MagmaCube, Phantom, Ghast (Mob), Shulker (AbstractGolem),
// Hoglin (Animal)
func MonsterClassKind(t MobType) bool { return kindOf(t).monsterClass(t) }

// monsterClass is MonsterClassKind for t, whose kinds entry k is.
func (k *kind) monsterClass(t MobType) bool {
	switch t {
	case TypeSlime, TypeMagmaCube, TypePhantom, TypeGhast, TypeShulker, TypeHoglin:
		return false
	}
	return k.enemy(t)
}

// CreatureKind reports MobCategory.CREATURE kinds, which the server fills
// every 400 ticks. Golems and villagers are MISC and count toward no cap;
// water kinds have their own caps.
func CreatureKind(t MobType) bool {
	k := lookupKind(t)
	return k != nil && !k.hostile && !k.water && !k.ambient && !golemKind(t) && t != TypeVillager && t != TypeNautilus && !AxolotlKind(t)
}

// WaterCreatureKind reports MobCategory.WATER_CREATURE (the squid, listed
// by ocean and river biomes).
func WaterCreatureKind(t MobType) bool {
	return t == TypeSquid || t == TypeNautilus || t == TypeDolphin
}

// AxolotlKind reports MobCategory.AXOLOTLS.
func AxolotlKind(t MobType) bool { return t == TypeAxolotl }

// UndergroundWaterKind reports MobCategory.UNDERGROUND_WATER_CREATURE (the
// glow squid, listed by every overworld biome, spawning in dark water).
func UndergroundWaterKind(t MobType) bool { return t == TypeGlowSquid }

// WaterAmbientKind reports MobCategory.WATER_AMBIENT (the fish of oceans and
// rivers).
func WaterAmbientKind(t MobType) bool { return FishKind(t) }

// AmbientKind reports a mob type of MobCategory.AMBIENT, the bat.
func AmbientKind(t MobType) bool { return kindOf(t).ambient }

// SpawnCategoryOf is the category whose cap a mob type counts toward, false
// for a type in none (MISC).
func SpawnCategoryOf(t MobType) (SpawnCategory, bool) {
	c := t.Category()
	return c, c != registry.MobCategoryMisc
}

// KnownMobKind reports whether this package simulates the type, as /summon
// checks.
func KnownMobKind(t MobType) bool {
	return lookupKind(t) != nil
}

// SpawnableKind reports whether a spawn egg or /summon may create the kind.
func SpawnableKind(t MobType) bool {
	return lookupKind(t) != nil
}

// nearestSpawnPlayer is Level.getNearestPlayer for the distance rules: the
// nearest non-spectator, unbounded, alive or not.
func nearestSpawnPlayer(players []SpawnPlayer, x, y, z float64) *SpawnPlayer {
	var nearest *SpawnPlayer
	best := math.MaxFloat64
	for i := range players {
		p := &players[i]
		if p.Spectator {
			continue
		}
		if d := Dist2(x, y, z, p.X, p.Y, p.Z); d < best {
			best = d
			nearest = p
		}
	}
	return nearest
}

// CloseForSpawning reports whether p is a non-spectator within 128 blocks
// horizontally of the chunk's centre.
// vanilla: ChunkMap.playerIsCloseEnoughForSpawning
func (p *SpawnPlayer) CloseForSpawning(pos ChunkPos) bool {
	if p.Spectator {
		return false
	}
	dx, dz := float64(pos.X*16+8)-p.X, float64(pos.Z*16+8)-p.Z
	return dx*dx+dz*dz < 128*128
}

// creatureSpawnProbability is the biome data's creature_spawn_probability
// for original mobs: 0.1, except 0.07 in snowy plains and ice spikes, 0.03 in
// badlands and eroded badlands and 0.04 in wooded badlands.
// vanilla: MobSpawnSettings.DEFAULT_CREATURE_SPAWN_PROBABILITY
const creatureSpawnProbability float32 = 0.1

// biomeCreatureProbabilities are the biomes overriding the default creature
// probability.
// vanilla: OverworldBiomes.plains, OverworldBiomes.badlands, BiomeData
var biomeCreatureProbabilities = [registry.BiomeCount]float32{
	registry.BiomeBadlands:       0.03,
	registry.BiomeErodedBadlands: 0.03,
	registry.BiomeWoodedBadlands: 0.04,
	registry.BiomeSnowyPlains:    0.07,
	registry.BiomeIceSpikes:      0.07,
}

// SpawnOriginalMobs spawns a freshly generated chunk's original mobs: while
// the biome's creature probability roll succeeds, one weighted roll of its
// CREATURE list picks a kind and size, and the group walks onto ground the
// animal rules accept, as one pack for the finalize's babies. No player, cap
// or distance rule applies. spawn drives the gate, pick, count, walk and yaw;
// region is the WorldGenRegion's random the spawn rules and the packs'
// finalizes roll, handed on in SpawnRequest.Region.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration, reached by
// NoiseBasedChunkGenerator.spawnOriginalMobs
func SpawnOriginalMobs(w SpawnWorld, spawn, region Dice, pos ChunkPos, sink func(SpawnRequest)) {
	w = undarkenedWorld{w}
	xo, zo := pos.X*16, pos.Z*16
	// One biome for the chunk's whole herd, at the corner's top block.
	// vanilla: NoiseBasedChunkGenerator.spawnOriginalMobs's sourcePos
	// vanilla: EnvironmentAttributeSystem.addBiomeLayerForAttribute (the
	// probability is not full-resolution, so it reads the corner's own cell)
	table := currentBiomeSpawns()
	top := w.MinY() + w.Height() - 1
	biome := w.Biome(xo, top, zo)
	chance := creatureSpawnProbability
	if noise := w.NoiseBiome(xo, top, zo); noise >= 0 && noise < registry.BiomeCount {
		chance = table.creatureProbabilities[noise]
	}
	var list []spawnData
	if biome >= 0 && biome < registry.BiomeCount {
		list = table.lists[CategoryCreature][biome].entries
	}
	if len(list) == 0 {
		// No creatures in this biome (ocean, the End): vanilla returns before
		// the gate roll.
		return
	}
	for spawn.NextFloat() < chance {
		data := pickFromList(list, spawn)
		count := data.minCount
		if data.maxCount != data.minCount {
			// vanilla: UniformInt.sample; an equal span is a ConstantInt, no roll
			count += int(spawn.NextIntN(int32(data.maxCount - data.minCount + 1)))
		}
		// An unsimulated kind still spends its group's rolls and places nothing.
		known := KnownMobKind(data.mob)
		group := NewSpawnGroup(region)
		x0 := xo + spawn.NextIntN(16)
		z0 := zo + spawn.NextIntN(16)
		x, z := x0, z0
		for range count {
			placed := false
			for attempts := 0; !placed && attempts < 4; attempts++ {
				// The placement reads the rolled column, the collision box and
				// the spawn rules (in that order) the clamped stand.
				if y, ok := groundStandY(w, data.mob, x, z); known && ok && originalSpawnPositionOk(w, data.mob, x, y, z) {
					fx, fz := clampedStand(data.mob, x, z, pos)
					if boxCollidesBlocks(w, data.mob, fx, float64(y), fz) ||
						!spawnRulesAt(w, region, data.mob, fx, float64(y), fz, CategoryCreature, false) {
						// A colliding box or a refused spawn rule spends the
						// attempt without walking: the next one retries this
						// column. vanilla: the continue in
						// NaturalSpawner.spawnMobsForChunkGeneration
						continue
					}
					// Rolled before the obstruction check: a mob it refuses
					// has still spent its yaw. vanilla: Entity.snapTo
					yaw := spawn.NextFloat() * 360
					// No bodies: a WorldGenRegion's getEntities is always empty,
					// so a member may stand inside an earlier one.
					// vanilla: WorldGenRegion.getEntities
					if validPositionAt(w, data.mob, fx, float64(y), fz, 0, nil) {
						sink(SpawnRequest{
							Type:   data.mob,
							X:      fx,
							Y:      float64(y),
							Z:      fz,
							Yaw:    yaw,
							Group:  group,
							Region: region,
						})
						placed = true
					}
				}
				x += spawn.NextIntN(5) - spawn.NextIntN(5) //nolint:staticcheck // SA4000: two rolls of one die, vanilla's own walk
				z += spawn.NextIntN(5) - spawn.NextIntN(5) //nolint:staticcheck // SA4000: two rolls of one die, vanilla's own walk
				for x < xo || x >= xo+16 || z < zo || z >= zo+16 {
					x = x0 + spawn.NextIntN(5) - spawn.NextIntN(5) //nolint:staticcheck // SA4000: two rolls of one die, vanilla's own walk
					z = z0 + spawn.NextIntN(5) - spawn.NextIntN(5) //nolint:staticcheck // SA4000: two rolls of one die, vanilla's own walk
				}
			}
		}
	}
}

// OriginalMobs is a freshly generated chunk's first-generation animals
// (SpawnOriginalMobs), finalized, for the caller to register. The dice are
// seeded from the level seed and chunk position; nextID hands out entity IDs
// and difficulty is the local difficulty at a mob's column.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration
func OriginalMobs(w SpawnWorld, seed int64, pos ChunkPos, nextID func() int32, difficulty func(x, z float64) rules.LocalDifficulty) []*Mob {
	var herd []*Mob
	cs := ChunkSeed(seed, pos)
	spawn, region := originalMobDice(seed, pos, w.HasCeiling())
	SpawnOriginalMobs(w, spawn, region, pos, func(r SpawnRequest) {
		m := NewMob(r.Type, nextID(), r.X, r.Y, r.Z, cs^int64(len(herd)+1))
		if m.Type == TypeStrider {
			m.MonsterJockeysDisabled = !w.BoolRule(world.RuleSpawnMobs) || !w.BoolRule(world.RuleSpawnMonsters)
		}
		// vanilla: Entity.snapTo
		m.Yaw = r.Yaw
		// Each animal's biome is read at its own block, so one across a biome
		// edge takes its side; sheep colour and variants read it.
		// vanilla: SpawnContext.create
		m.FinalizeSpawn(r.Region, w.Biome(rules.BlockOf(r.X), rules.BlockOf(r.Y), rules.BlockOf(r.Z)), r.Group, difficulty(r.X, r.Z))
		herd = append(herd, m)
	})
	return herd
}

// originalMobDice are a chunk's two first-generation streams: spawn, a
// legacy random on the chunk's decoration seed, and region, the
// WorldGenRegion's random at the chunk's corner. legacy is set for the
// nether, whose noise settings use the legacy random source.
// vanilla: NoiseBasedChunkGenerator.spawnOriginalMobs, WorldGenRegion
func originalMobDice(seed int64, pos ChunkPos, legacy bool) (spawn *levelgen.LegacyRandomSource, region levelgen.RandomSource) {
	spawn = levelgen.NewLegacyRandom(0)
	spawn.SetDecorationSeed(seed, pos.X*16, pos.Z*16)
	named := levelgen.NewXoroshiroNamedRandom
	if legacy {
		named = levelgen.NewLegacyNamedRandom
	}
	region = named(seed, "minecraft:worldgen_region_random").At(pos.X*16, 0, pos.Z*16)
	return spawn, region
}

// ChunkSeed folds a chunk position into the level seed with vanilla's two
// chunk-seed multipliers.
func ChunkSeed(seed int64, pos ChunkPos) int64 {
	return seed + int64(pos.X)*341873128712 + int64(pos.Z)*132897987541
}

// groundStandY is the first free y above the MOTION_BLOCKING_NO_LEAVES
// height, so a leaf canopy does not hide the grass; a parrot or an ocelot
// reads MOTION_BLOCKING and stands on top of the leaves. A dimension with a
// ceiling stands on the first non-air block under the topmost air pocket
// instead. false for a column with nothing to stand on.
// vanilla: SpawnPlacements.getHeightmapType, NaturalSpawner.getTopNonCollidingPos
func groundStandY(w SpawnWorld, t MobType, x, z int32) (int32, bool) {
	var y int32
	if t == TypeParrot || t == TypeOcelot {
		y = w.MotionBlockingHeight(x, z) + 1
	} else {
		y = w.NoLeavesHeight(x, z) + 1
	}
	if w.HasCeiling() {
		for {
			y--
			if rules.IsAir(w.BlockAt(x, y, z)) {
				break
			}
		}
		for {
			y--
			if !rules.IsAir(w.BlockAt(x, y, z)) || y <= w.MinY() {
				break
			}
		}
		return y, true
	}
	if y <= w.MinY() {
		return 0, false
	}
	return y, true
}

// clampedStand clamps a member's block coordinate to keep its half-width in
// the chunk, so it stands on the block corner rather than the centre.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration
func clampedStand(t MobType, x, z int32, pos ChunkPos) (fx, fz float64) {
	// EntityType.getWidth is a float: 0.9 clamps to 0.8999999761581421.
	width := float64(float32(kinds[t].width))
	xo, zo := float64(pos.X*16), float64(pos.Z*16)
	return min(max(float64(x), xo+width), xo+16-width),
		min(max(float64(z), zo+width), zo+16-width)
}

// SpawnPass is one category's part of a tick's natural spawning.
type SpawnPass struct {
	Counts   *SpawnCounts
	max      int
	category SpawnCategory
}

// NewSpawnPass is the pass of a category, capped at limit when it is not
// negative, else at the vanilla cap (MobCategory.max). The category selects
// the biome list and spawn rules; the server runs a persistent category
// (creature) every 400 ticks, the others every tick.
func NewSpawnPass(c SpawnCategory, counts *SpawnCounts, limit int) SpawnPass {
	if limit < 0 {
		limit = c.Max()
	}
	return SpawnPass{counts, limit, c}
}

// SpawnNaturally shuffles chunks once, trying open passes in order with shared bodies/potential.
// vanilla: ServerChunkCache.tickChunks, NaturalSpawner.spawnForChunk; global cap scales by chunks/289.
func SpawnNaturally(w SpawnWorld, rng *rand.Rand, chunks []ChunkPos, players []SpawnPlayer,
	bodies []SpawnBody, potential *SpawnPotential, passes []SpawnPass, spawnableChunkCount int, sink func(SpawnRequest)) {
	open := make([]SpawnPass, 0, len(passes))
	for _, p := range passes {
		if p.Counts.Global < spawnableChunkCount*p.max/spawnMagicNumber {
			open = append(open, p)
		}
	}
	if len(open) == 0 {
		return
	}
	// The spawn point cannot change during this pass.
	spawn := readSpawnPoint(w)
	rng.Shuffle(len(chunks), func(i, j int) { chunks[i], chunks[j] = chunks[j], chunks[i] })
	c := naturalSpawnContext{w: w}
	concrete, _ := w.(*world.World)
	var held world.SpawnChunk
	for _, pos := range chunks {
		c.playersPrepared, c.query = false, false
		c.referencesRead = false
		acquired := false
		for _, p := range open {
			if c.localCapOpen(players, pos, p.Counts, p.max) {
				if concrete != nil && !acquired {
					acquired = true
					var ok bool
					held, ok = concrete.HoldSpawnChunk(world.ChunkPos{X: pos.X, Z: pos.Z})
					if ok {
						c.chunk = &held
						c.w = c.chunk
					}
				}
				c.spawnCategoryForChunk(spawn, rng, pos, players, &bodies, potential, p.Counts, p.category, sink)
			}
		}
		if c.chunk != nil {
			c.chunk.Close()
			c.chunk = nil
		}
		c.w = w
	}
}

// localCapOpen is SpawnState.canSpawnForCategoryLocal: some nearby player
// still has room under the cap. A chunk with no nearby player is closed.
func localCapOpen(players []SpawnPlayer, pos ChunkPos, counts *SpawnCounts, categoryMax int) bool {
	for i := range players {
		if p := &players[i]; p.CloseForSpawning(pos) && counts.PerPlayer[p.EntityID] < categoryMax {
			return true
		}
	}
	return false
}

// spawnCategoryForChunk grows packs from one random position (NaturalSpawner.spawnCategoryForChunk).
func spawnCategoryForChunk(w SpawnWorld, spawn spawnPoint, rng *rand.Rand, pos ChunkPos,
	players []SpawnPlayer, bodies *[]SpawnBody, potential *SpawnPotential, counts *SpawnCounts, category SpawnCategory, sink func(SpawnRequest)) {
	c := naturalSpawnContext{w: w}
	c.spawnCategoryForChunk(spawn, rng, pos, players, bodies, potential, counts, category, sink)
}

func (c *naturalSpawnContext) spawnCategoryForChunk(spawn spawnPoint, rng *rand.Rand, pos ChunkPos,
	players []SpawnPlayer, bodies *[]SpawnBody, potential *SpawnPotential, counts *SpawnCounts, category SpawnCategory, sink func(SpawnRequest)) {
	w := c.w
	// vanilla: NaturalSpawner.getRandomPosWithin; inclusive upper bound is the first free y.
	bx := pos.X*16 + (rng.Int31() & 15)
	bz := pos.Z*16 + (rng.Int31() & 15)
	var minY, surface int32
	if c.chunk != nil && c.chunk.InOrigin(bx, bz) {
		minY = c.chunk.MinY()
		surface = c.chunk.OriginSurfaceHeight(bx&15, bz&15)
	} else {
		minY = w.MinY()
		surface = w.SurfaceHeight(bx, bz)
	}
	span := surface + 2 - minY
	if span <= 0 {
		return
	}
	y := minY + rng.Int31n(span)
	if y < minY+1 {
		return
	}
	// vanilla: NaturalSpawner.spawnCategoryForPosition isRedstoneConductor
	if c.chunk != nil {
		if c.chunk.InitialSpawnBlocked(bx, y, bz) {
			return
		}
	} else if conductorAt(w, bx, y, bz) {
		return
	}
	if c.chunk != nil && !c.playersPrepared {
		c.playersPrepared = true
		c.preparePlayers(pos, players)
	}
	c.spawnCategoryForPosition(spawn, rng, pos, int32(y), bx, bz, players, bodies, potential, counts, category, sink)
}

// spawnCategoryForPosition walks three packs (NaturalSpawner.spawnCategoryForPosition).
func spawnCategoryForPosition(w SpawnWorld, spawn spawnPoint, rng *rand.Rand, pos ChunkPos, y int32, bx, bz int32,
	players []SpawnPlayer, bodies *[]SpawnBody, potential *SpawnPotential, counts *SpawnCounts, category SpawnCategory, sink func(SpawnRequest)) {
	c := naturalSpawnContext{w: w}
	c.spawnCategoryForPosition(spawn, rng, pos, y, bx, bz, players, bodies, potential, counts, category, sink)
}

func (c *naturalSpawnContext) spawnCategoryForPosition(spawn spawnPoint, rng *rand.Rand, pos ChunkPos, y int32, bx, bz int32,
	players []SpawnPlayer, bodies *[]SpawnBody, potential *SpawnPotential, counts *SpawnCounts, category SpawnCategory, sink func(SpawnRequest)) {
	w := c.w
	spawned := 0
	for range 3 {
		// SpawnGroupData threads through each pack's synchronous finalization.
		var group *SpawnGroup
		x0, z0 := bx, bz
		var data *spawnData
		var selected *spawnList
		var kind selectedSpawnKind
		discarded := false
		steps := int(math.Ceil(float64(rng.Float32()) * 4.0))
		for step := 0; step < steps; step++ {
			x0 += spawnWalkDie(rng) - spawnWalkDie(rng) //nolint:staticcheck // SA4000: two rolls of one die, as vanilla's is
			z0 += spawnWalkDie(rng) - spawnWalkDie(rng) //nolint:staticcheck // SA4000: two rolls of one die, as vanilla's is
			if discarded {
				continue
			}
			if c.chunk != nil {
				c.chunk.ResetSpawnStep()
			}
			dist, exists := c.playerDistance(players, float64(x0)+0.5, float64(y), float64(z0)+0.5)
			if !exists {
				continue
			}
			if !rightDistanceToPlayerAndSpawnPoint(w, spawn, x0, y, z0, dist) {
				continue
			}
			initial := data == nil
			var biome registry.Biome
			if initial {
				biome = c.spawnBiome(category, x0, y, z0)
				// vanilla: NaturalSpawner.getRandomSpawnMobAt; suppression precedes list resolution.
				if category == CategoryWaterAmbient && biome.InTag(registry.BiomeTagIDReduceWaterAmbientSpawns) && rng.Float32() < 0.98 {
					break
				}
				selected = c.listAt(x0, y, z0, category, biome)
				data = selected.pick(rng)
				if data == nil {
					break
				}
				// Even a fixed pack size consumes Intn(1).
				steps = data.minCount + rng.Intn(1+data.maxCount-data.minCount)
				kind = selectedKind(data.mob)
				if c.query && (!kind.known || !kind.far && dist > kind.distanceSq && c.packOutOfRange(players, float64(x0)+0.5, float64(y), float64(z0)+0.5, steps-step-1, kind.distanceSq)) {
					discarded = true
					continue
				}
			}
			if !kind.known {
				// Unsupported kinds keep their share of the weighted list.
				continue
			}
			if !kind.far && dist > kind.distanceSq {
				continue
			}
			if !spawnTerrainPossible(w, data.mob, x0, y, z0, category) {
				continue
			}
			if !initial {
				biome = c.spawnBiome(category, x0, y, z0)
				list := c.listAt(x0, y, z0, category, biome)
				if list != selected && !slices.Contains(list.entries, *data) {
					continue
				}
			}
			if !spawnPlacementClear(w, GoDice{rng}, data.mob, float64(x0)+0.5, float64(y), float64(z0)+0.5, category) {
				continue
			}
			// vanilla: SpawnState.canSpawn
			cost, costed := c.spawns().costAt(biome, data.mob)
			if costed && potential.EnergyChange(x0, y, z0, cost.charge) > cost.budget {
				continue
			}
			if !validPositionWithDespawn(w, data.mob, float64(x0)+0.5, float64(y), float64(z0)+0.5, dist, kind.distanceSq, *bodies) {
				continue
			}
			if group == nil {
				group = NewNaturalSpawnGroup(GoDice{rng})
			}
			sink(SpawnRequest{
				Type:  data.mob,
				X:     float64(x0) + 0.5,
				Y:     float64(y),
				Z:     float64(z0) + 0.5,
				Yaw:   rng.Float32() * 360,
				Group: group,
			})
			if c.chunk != nil {
				c.chunk.ResetSpawnStep()
			}
			kind = selectedKind(data.mob)
			if c.query && !kind.known {
				discarded = true
			}
			if costed {
				potential.Add(x0, y, z0, cost.charge)
			}
			// Finalization and body publication precede the next roll.
			*bodies = append(*bodies, spawnBox(data.mob, float64(x0)+0.5, float64(y), float64(z0)+0.5))
			counts.Global++
			if counts.PerPlayer == nil {
				counts.PerPlayer = make(map[int32]int)
			}
			landed := ChunkPos{X: x0 >> 4, Z: z0 >> 4}
			if c.query && landed == c.origin {
				for _, i := range c.closePlayers {
					counts.PerPlayer[players[i].EntityID]++
				}
			} else {
				for i := range players {
					if p := &players[i]; p.CloseForSpawning(landed) {
						counts.PerPlayer[p.EntityID]++
					}
				}
			}
			spawned++
			// The cluster size caps one position's spawns across all its packs;
			// a solitary tropical fish ends only its own pack.
			// vanilla: TropicalFish.isMaxGroupSizeReached
			if spawned >= maxSpawnClusterSize(data.mob) {
				return
			}
			if data.mob == TypeTropicalFish && group.tropicalSolitary {
				break
			}
		}
	}
}

type spawnPoint struct {
	x, y, z int32
	guarded bool
}

func readSpawnPoint(w SpawnWorld) spawnPoint {
	x, y, z, guarded := w.SpawnPoint()
	return spawnPoint{x, y, z, guarded}
}

// rightDistanceToPlayerAndSpawnPoint keeps 24 blocks clear and requires a loaded landing chunk.
// vanilla: NaturalSpawner.isRightDistanceToPlayerAndSpawnPoint
func rightDistanceToPlayerAndSpawnPoint(w SpawnWorld, spawn spawnPoint, x, y, z int32, distToPlayerSq float64) bool {
	if distToPlayerSq <= spawnMinDistanceSq {
		return false
	}
	if spawn.guarded {
		// BlockPos.closerToCenterThan uses the spawn centre but the uncentred y.
		dx := float64(spawn.x) + 0.5 - (float64(x) + 0.5)
		dy := float64(spawn.y) + 0.5 - float64(y)
		dz := float64(spawn.z) + 0.5 - (float64(z) + 0.5)
		if dx*dx+dy*dy+dz*dz < spawnMinDistanceSq {
			return false
		}
	}
	if !w.ChunkLoadedAt(x, z) {
		return false
	}
	if s, ok := w.(*world.SpawnChunk); ok {
		return s.ChunkInBorder(world.ChunkPos{X: x >> 4, Z: z >> 4})
	}
	return chunkInBorder(w, x>>4, z>>4)
}

// chunkInBorder reports whether the chunk lies wholly inside the border.
// vanilla: WorldBorder.isWithinBounds(ChunkPos)
func chunkInBorder(w World, cx, cz int32) bool {
	return w.InBorder(cx*16, cz*16) && w.InBorder(cx*16+15, cz*16+15)
}

// ChunkInBorder reports whether a chunk may spawn entities as far as the
// border goes.
func ChunkInBorder(w World, pos ChunkPos) bool { return chunkInBorder(w, pos.X, pos.Z) }

// validSpawnPositionForType is NaturalSpawner.isValidSpawnPostitionForType
// at a block position's centre, as the tick's pass calls it.
func validSpawnPositionForType(w SpawnWorld, rng *rand.Rand, t MobType, x, y, z int32, distToPlayerSq float64, category SpawnCategory) bool {
	return validSpawnPositionAt(w, GoDice{rng}, t, float64(x)+0.5, float64(y), float64(z)+0.5, distToPlayerSq, category)
}

// validSpawnPositionAt is NaturalSpawner.isValidSpawnPostitionForType at any
// feet centre: within the despawn band of the nearest player unless the type
// may spawn far from players, the type's
// placement (SpawnPlacements.isSpawnPositionOk), its spawn rule
// (SpawnPlacements.checkSpawnRules), and the spawn box clear of blocks.
// Block reads use the block the centre is in, box reads the centre itself.
func validSpawnPositionAt(w SpawnWorld, rng Dice, t MobType, x, y, z float64, distToPlayerSq float64, category SpawnCategory) bool {
	if far := despawnDistanceOf(t); !canSpawnFarFromPlayer(t) && distToPlayerSq > far*far {
		return false
	}
	return spawnPlacementClear(w, rng, t, x, y, z, category)
}

func spawnPlacementClear(w SpawnWorld, rng Dice, t MobType, x, y, z float64, category SpawnCategory) bool {
	return spawnRulesAt(w, rng, t, x, y, z, category, true) && !boxCollidesBlocks(w, t, x, y, z)
}

// spawnRulesAt is SpawnPlacements.checkSpawnRules at the block holding a
// feet centre, after the ON_GROUND placement when onGround is set; chunk
// generation clears it, having checked the placement at the unclamped column.
// Monsters use Monster.checkMonsterSpawnRules (the peaceful refusal is here
// for direct test calls; the driver uses spawnEnemies), creatures
// Animal.checkAnimalSpawnRules, water categories the squids' rules.
func spawnRulesAt(w SpawnWorld, rng Dice, t MobType, x, y, z float64, category SpawnCategory, onGround bool) bool {
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	if t == TypeGuardian {
		// vanilla: SpawnPlacementTypes.IN_WATER, Guardian.checkGuardianSpawnRules
		if !rules.IsWater(w.BlockAt(bx, by, bz)) || conductorAt(w, bx, by+1, bz) {
			return false
		}
		return (rng.NextIntN(20) == 0 || !seesSkyBelowWater(w, bx, by, bz)) &&
			w.Difficulty() != rules.DifficultyPeaceful && rules.IsWater(w.BlockAt(bx, by-1, bz))
	}
	if t == TypeDrowned {
		return drownedSpawnOk(w, rng, bx, by, bz)
	}
	if t == TypeDolphin {
		return dolphinSpawnOk(w, bx, by, bz)
	}
	if IsNautilus(t) {
		return nautilusSpawnOk(w, bx, by, bz)
	}
	if t == TypeStrider {
		return striderSpawnOk(w, bx, by, bz)
	}
	if t == TypeOcelot {
		// The ocelot's rule applies whichever list names it (bamboo jungle lists
		// it among monsters).
		return (!onGround || spawnPositionOk(w, t, bx, by, bz)) && ocelotSpawnOk(w, rng, bx, by, bz)
	}
	switch category {
	case CategoryCreature:
		if onGround && !unrestrictedPlacement(t) && !spawnPositionOk(w, t, bx, by, bz) {
			return false
		}
		if t == TypeTurtle {
			if !turtleSpawnOk(w, bx, by, bz) {
				return false
			}
		} else if !creatureGroundSpawnOk(w, t, bx, by, bz) {
			return false
		}
	case CategoryAxolotls:
		// vanilla: SpawnPlacementTypes.IN_WATER, Axolotl.checkAxolotlSpawnRules
		if !rules.IsWater(w.BlockAt(bx, by, bz)) ||
			conductorAt(w, bx, by+1, bz) ||
			!registry.BlockOf(w.BlockAt(bx, by-1, bz)).InTag(axolotlGround) {
			return false
		}
	case CategoryWaterCreature, CategoryUndergroundWater, CategoryWaterAmbient:
		// vanilla: WaterAnimal.checkSurfaceWaterAnimalSpawnRules
		if t == TypeTropicalFish {
			if !tropicalFishSpawnPositionOK(w, bx, by, bz) {
				return false
			}
		} else if !squidSpawnPositionOk(w, category, bx, by, bz) {
			return false
		}
	case CategoryAmbient:
		if !spawnPositionOk(w, t, bx, by, bz) || !batSpawnOk(w, rng, bx, by, bz) {
			return false
		}
	default:
		if !spawnPositionOk(w, t, bx, by, bz) {
			return false
		}
		if w.Difficulty() == rules.DifficultyPeaceful && t != TypePiglin && t != TypeHoglin {
			return false
		}
		if t == TypePiglin || t == TypeHoglin {
			// vanilla: Piglin.checkPiglinSpawnRules, Hoglin.checkHoglinSpawnRules; no light check
			if registry.BlockOf(w.BlockAt(bx, by-1, bz)) == registry.BlockNetherWartBlock {
				return false
			}
		} else if t == registry.EntityTypeSlime {
			if !slimeSpawnRules(w, rng, bx, by, bz) {
				return false
			}
		} else if t == TypeGhast {
			// vanilla: Ghast.checkGhastSpawnRules, no light check
			if rng.NextIntN(20) != 0 {
				return false
			}
		} else if t == TypePillager {
			// vanilla: PatrollingMonster.checkPatrollingMonsterSpawnRules
			if w.BlockLightAt(bx, by, bz) > 8 {
				return false
			}
		} else if t == TypeBlaze {
			// vanilla: Monster.checkAnyLightMonsterSpawnRules
		} else if t == TypeZombifiedPiglin {
			// vanilla: ZombifiedPiglin.checkZombifiedPiglinSpawnRules, no light check
			if registry.BlockOf(w.BlockAt(bx, by-1, bz)) == registry.BlockNetherWartBlock {
				return false
			}
		} else if t != TypeMagmaCube && t != TypeSilverfish && t != TypeSulfurCube && !isDarkEnoughToSpawn(w, bx, by, bz, rng) {
			// vanilla: MagmaCube.checkMagmaCubeSpawnRules and
			// Silverfish.checkSilverfishSpawnRules ask for no darkness. The
			// silverfish's player-within-5 test is implied by the pass's 24-block
			// minimum.
			return false
		}
		if !seesSkyForSpawn(w, t, bx, by, bz) {
			return false
		}
	}
	return true
}

// creatureGroundSpawnOk checks the creature predicates' ground tags and brightness.
// vanilla: Animal.checkAnimalSpawnRules and the per-type ground predicates
func creatureGroundSpawnOk(w SpawnWorld, t MobType, x, y, z int32) bool {
	ground := registry.BlockGrassBlock
	if t == TypeMooshroom {
		ground = registry.BlockMycelium
	}
	if t == TypePolarBear && w.Biome(x, y, z).InTag(registry.BiomeTagIDPolarBearsSpawnOnAlternateBlocks) {
		// vanilla: PolarBear.checkPolarBearSpawnRules; the
		// polar_bears_spawnable_on_alternate tag is just ice, and the
		// brightness test below still applies
		if registry.BlockOf(w.BlockAt(x, y-1, z)) != registry.BlockIce {
			return false
		}
	} else if t == TypeGoat {
		// vanilla: Goat.checkGoatSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(goatGround) {
			return false
		}
	} else if t == TypeRabbit {
		// vanilla: Rabbit.checkRabbitSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(rabbitGround) {
			return false
		}
	} else if t == TypeFox {
		// vanilla: Fox.checkFoxSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(foxGround) {
			return false
		}
	} else if t == TypeArmadillo {
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(registry.TagIDArmadilloSpawnableOn) {
			return false
		}
	} else if t == TypeCamel {
		// vanilla: Camel.checkCamelSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(registry.TagIDCamelsSpawnableOn) {
			return false
		}
	} else if t == TypeWolf {
		// vanilla: Wolf.checkWolfSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(wolfGround) {
			return false
		}
	} else if t == TypeFrog {
		// vanilla: Frog.checkFrogSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(frogGround) {
			return false
		}
	} else if t == TypeParrot {
		// vanilla: Parrot.checkParrotSpawnRules
		if !registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(parrotGround) {
			return false
		}
	} else if registry.BlockOf(w.BlockAt(x, y-1, z)) != ground {
		return false
	}
	return rawBrightness(w, x, y, z) > 8
}

// batSpawnOk is Bat.checkBatSpawnRules beyond the placement: below the
// surface, a coin flip, light no brighter than a roll of four, on a
// bats_spawnable_on block.
func batSpawnOk(w SpawnWorld, rng Dice, x, y, z int32) bool {
	// SurfaceHeight is the highest block; WORLD_SURFACE's value is one over.
	if y >= w.SurfaceHeight(x, z)+1 {
		return false
	}
	if rng.NextIntN(2) == 0 {
		return false
	}
	if int32(w.MaxLocalRawLightAt(x, y, z)) > rng.NextIntN(4) {
		return false
	}
	return registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(batsSpawnableOn)
}

// seesSkyForSpawn is the sky half of the surface monsters' rules (canSeeSky
// as full sky light): Monster.checkSurfaceMonstersSpawnRules for husk and
// parched, Stray.checkStraySpawnRules for the stray (looking from the top of
// any powder snow above). Other kinds pass.
func seesSkyForSpawn(w SpawnWorld, t MobType, x, y, z int32) bool {
	switch t {
	case TypeHusk, TypeParched, TypeCamelHusk:
		return w.SkyLightAt(x, y, z) >= 15
	case TypeStray:
		for registry.BlockOf(w.BlockAt(x, y+1, z)) == registry.BlockPowderSnow {
			y++
		}
		return w.SkyLightAt(x, y, z) >= 15
	}
	return true
}

// validPositionForMob is NaturalSpawner.isValidPositionForMob at a block
// position's centre, as the tick's pass calls it.
func validPositionForMob(w SpawnWorld, t MobType, x, y, z int32, distToPlayerSq float64, bodies []SpawnBody) bool {
	return validPositionAt(w, t, float64(x)+0.5, float64(y), float64(z)+0.5, distToPlayerSq, bodies)
}

// undarkenedWorld reads light as a WorldGenRegion does: no sky darkening, so
// the raw light is the brighter of sky and block light.
// vanilla: WorldGenRegion.getSkyDarken
type undarkenedWorld struct{ SpawnWorld }

func (u undarkenedWorld) MaxLocalRawLightAt(x, y, z int32) uint8 {
	return max(u.SkyLightAt(x, y, z), u.BlockLightAt(x, y, z))
}

// validPositionAt is NaturalSpawner.isValidPositionForMob at any feet
// centre: a walk-target value of at least 0 (PathfinderMob.checkSpawnRules),
// within the despawn band for a body removeWhenFarAway would remove, no fluid
// in its box, and no blocksBuilding body in the way.
func validPositionAt(w SpawnWorld, t MobType, x, y, z float64, distToPlayerSq float64, bodies []SpawnBody) bool {
	far := despawnDistanceOf(t)
	return validPositionWithDespawn(w, t, x, y, z, distToPlayerSq, far*far, bodies)
}

func validPositionWithDespawn(w SpawnWorld, t MobType, x, y, z, distToPlayerSq, farSq float64, bodies []SpawnBody) bool {
	if pathfinderMobSpawn(t) && (&Mob{Type: t}).walkTargetValue(w, Waypoint{int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))}) < 0 {
		return false
	}
	if distToPlayerSq > farSq && freshRemovesWhenFarAway(t) {
		return false
	}
	// vanilla: Drowned.checkSpawnObstruction and
	// AgeableWaterCreature.checkSpawnObstruction leave the liquid out
	if !leavesLiquidOut(t) && boxInLiquid(w, t, x, y, z) {
		return false
	}
	if boxOverlapsBody(bodies, spawnBox(t, x, y, z)) {
		return false
	}
	return true
}

// pathfinderMobSpawn reports kinds that are a PathfinderMob in vanilla, whose
// checkSpawnRules asks getWalkTargetValue.
func pathfinderMobSpawn(t MobType) bool {
	switch t {
	case TypeSlime, TypeMagmaCube, TypePhantom, TypeGhast, TypeBat:
		return false
	}
	return true
}

// leavesLiquidOut reports kinds whose checkSpawnObstruction override drops
// the liquid check: water is where they belong.
// vanilla: Guardian, Drowned, AgeableWaterCreature, Mob.checkSpawnObstruction
func leavesLiquidOut(t MobType) bool {
	return t == TypeZombieNautilus || t == TypeGuardian || t == TypeElderGuardian || t == TypeDrowned || t == TypeStrider || AxolotlKind(t) || WaterCreatureKind(t) || UndergroundWaterKind(t) || WaterAmbientKind(t)
}

// maxSpawnClusterSize is Mob.getMaxSpawnClusterSize: the most of a type one
// position's three packs may spawn.
// vanilla: AbstractFish.getMaxSpawnClusterSize, AbstractSchoolingFish.getMaxSpawnClusterSize
func maxSpawnClusterSize(t MobType) int {
	if IsEquine(t) {
		// vanilla: AbstractHorse.getMaxSpawnClusterSize
		return 6
	}
	switch t {
	case TypeGhast:
		// vanilla: Ghast.getMaxSpawnClusterSize
		return 1
	case TypePillager:
		// vanilla: Pillager.getMaxSpawnClusterSize
		return 1
	case TypeWolf:
		// vanilla: Wolf.getMaxSpawnClusterSize
		return 8
	case TypeCod, TypeTropicalFish, TypePufferfish:
		return 8
	case TypeSalmon:
		return 5
	}
	return spawnClusterSize
}

// canSpawnFarFromPlayer is EntityType.canSpawnFarFromPlayer: the creature
// and misc categories (a kind SpawnCategoryOf places nowhere), plus the
// pillager and the shulker.
// vanilla: EntityType.Builder, EntityTypes
func canSpawnFarFromPlayer(t MobType) bool {
	if t == TypePillager || t == TypeShulker {
		return true
	}
	c, ok := SpawnCategoryOf(t)
	return !ok || c == CategoryCreature
}

// freshRemovesWhenFarAway is Mob.removeWhenFarAway for a mob of the type
// just created: false for animals, golems and a cat or ocelot at age 0.
func freshRemovesWhenFarAway(t MobType) bool {
	return (&Mob{Type: t}).removeWhenFarAway()
}

// despawnDistanceOf is the type's MobCategory despawn distance; positions
// beyond it from the nearest player are refused.
// vanilla: MobCategory.getDespawnDistance
func despawnDistanceOf(t MobType) float64 {
	if WaterAmbientKind(t) {
		return waterAmbientDespawnDist
	}
	return monsterDespawnDistanceI
}
