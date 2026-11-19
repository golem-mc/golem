// Package entity simulates the server's own entities: item entities and
// mobs. It works on plain values: the world is read through the small
// interface defined here, no state is kept beyond the entities the caller
// passes, nothing is sent and no lock is taken. Spawning for clients,
// movement updates, pickups and dimensions are the server's.
package entity

import "github.com/golem-mc/golem/internal/server/world"

// World is the world as an entity simulation reads it: blocks and the few
// environment facts a mob needs. Tests can substitute a stub.
type World interface {
	BlockAt(x, y, z int32) uint16
	// BlocksIn appends to buf[:0] the BlockAt of every cell in the inclusive
	// box, x outermost, then y, then z.
	BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16
	// MovingPistonBoxes is a moving piston's cell-local collision, which the
	// state alone cannot give.
	MovingPistonBoxes(x, y, z int32) []world.CollisionBox
	// ShulkerBoxBoxes is a shulker box's cell-local collision, which grows
	// with its lid.
	ShulkerBoxBoxes(state uint16, x, y, z int32) []world.CollisionBox
	// PlacementSurvives is BlockState.canSurvive for the state at the cell.
	PlacementSurvives(state uint16, x, y, z int32) bool
	// DimensionName is the dimension's wire name, as GlobalPos holds it.
	DimensionName() string
	// MinY is Level.getMinY, the floor of the below-world check.
	MinY() int32
	// InBorder is WorldBorder.isWithinBounds for a block column: whether a
	// mob may be put there.
	InBorder(x, z int32) bool
	// InBorderBox is WorldBorder.isWithinBounds(AABB), including its far-edge epsilon.
	InBorderBox(minX, minZ, maxX, maxZ float64) bool
	// BorderEdges is the world border's current edges; ok is false when the
	// world has none.
	BorderEdges() (minX, minZ, maxX, maxZ float64, ok bool)
	// MaxY is one past the dimension's top block, Level.getMaxY.
	MaxY() int32
	// SkyLightAt is the sky light level at the cell, 0 where the chunk is
	// not loaded.
	SkyLightAt(x, y, z int32) uint8
	// MaxLocalRawLightAt is LevelReader.getMaxLocalRawBrightness: the brighter
	// of block light and sky light less the hour's darkening; 0 where not
	// loaded.
	MaxLocalRawLightAt(x, y, z int32) uint8
	// AmbientLight is the dimension's minecraft:ambient_light, which the
	// light-dependent magic value lerps from.
	AmbientLight() float32
	// IsRaining is Level.isRaining (rain level past 0.2), which puts out a
	// burning mob.
	IsRaining() bool
	// BoolRule is the value of the boolean game rule with the id given.
	BoolRule(id world.GameRuleIndex) bool
	// RainingAt is Level.isRainingAt: raining, open sky above, and a raining
	// biome.
	RainingAt(x, y, z int32) bool
	// PrecipitatesAt is Level.precipitationAt != NONE: RainingAt for rain or
	// snow.
	PrecipitatesAt(x, y, z int32) bool
	// InClouds is Entity.isInClouds for a box of the height given at the
	// position.
	InClouds(x, y, z, height float64) bool
	// BrightOutside is Level.isBrightOutside: a running clock and sky darkening
	// under four. The sun goals gate on it before reading the light; the burn's
	// daylight is a different attribute and the two need not agree.
	BrightOutside() bool
	// DarkOutside is Level.isDarkOutside: a running clock and not bright.
	DarkOutside() bool
	// IsThundering is Level.isThundering, which wakes foxes.
	IsThundering() bool
	// ClockTime, EnvironmentFloat and LootReads are what a loot roll's
	// level reads (rules.LevelAt).
	ClockTime(clock string) (worldAge, total int64, ok bool)
	EnvironmentFloat(attribute string, x, y, z float64) float32
	LootReads() world.LootReads
	// MonstersBurn reports whether monsters burn in this environment.
	MonstersBurn() bool
	// PiglinsZombify is the gameplay/piglins_zombify environment attribute.
	PiglinsZombify() bool
	// SnowGolemMelts is the gameplay/snow_golem_melts attribute at the cell (the
	// nether and hot biomes).
	SnowGolemMelts(x, y, z int32) bool
	// SnowLayerSurvives is SnowLayerBlock.canSurvive for one layer at the cell.
	SnowLayerSurvives(x, y, z int32) bool
	// IsAirAt is Block.isAir at the cell: air, cave_air or void_air.
	IsAirAt(x, y, z int32) bool
	// MobGriefing is the mob_griefing game rule.
	MobGriefing() bool
	// TurtleEggsIn reports whether a loaded section overlapping the inclusive
	// box holds a turtle egg.
	TurtleEggsIn(x0, y0, z0, x1, y1, z1 int32) bool
	// Survives is the state's canSurvive at the cell, true without a rule.
	Survives(x, y, z int32, state uint16) bool
	// MaxEntityCramming is the max_entity_cramming game rule, 0 for off.
	MaxEntityCramming() int32
	// Difficulty is the Change Difficulty packet's value: 0 peaceful, 1 easy, 2
	// normal, 3 hard.
	Difficulty() uint8
	// FastLava is minecraft:gameplay/fast_lava: the nether's stronger lava
	// current.
	FastLava() bool
	// SeaLevel is Level.getSeaLevel, which a phantom's anchor stays above.
	SeaLevel() int32
	// NoLeavesHeight is the highest motion-blocking block excluding leaves.
	NoLeavesHeight(x, z int32) int32
	// MotionBlockingHeight is the MOTION_BLOCKING heightmap's highest block y,
	// or one below the floor when empty or not loaded.
	MotionBlockingHeight(x, z int32) int32
	// BeesStayInHive is gameplay/bees_stay_in_hive (overworld night and rain).
	BeesStayInHive() bool
	// CatGiftChance is gameplay/cat_waking_up_gift_chance: the chance a cat
	// that slept beside its owner leaves a gift.
	CatGiftChance() float32
	// BeehiveOccupants is the bee count of the hive or nest at the cell; false
	// when there is none or it is not loaded.
	BeehiveOccupants(x, y, z int32) (n int, ok bool)
	// BeehiveCells lists the loaded beehives and bee nests within r blocks
	// of the cell.
	BeehiveCells(x, y, z, r int32) [][3]int32
}

// BodyWorld is World plus collidable bodies, the entity colliders
// Entity.collide gathers beside block shapes, and the pushable bodies an
// unridden boat stops at. Without it
// the sweeps see blocks alone.
type BodyWorld interface {
	World
	// BodyBoxes is EntityGetter.getEntityCollisions: the collidable boxes
	// whose insides meet box (touching does not count).
	BodyBoxes(box AABB) []AABB
}
