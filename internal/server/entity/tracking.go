package entity

import "github.com/golem-mc/golem/registry"

// Entity tracking: how far away a client is shown an entity and how often a
// moving one is updated, read from a vanilla server's EntityTypes
// initialiser (EntityType.Builder defaults to clientTrackingRange 5 chunks
// and updateInterval 3 ticks).

// tracking is a type's tracking range in chunks and update interval in
// ticks.
type tracking struct {
	rangeChunks    int
	updateInterval int
}

// mobTracking carries the numbers each kind's vanilla type carries.
var mobTracking = [registry.EntityTypeCount]tracking{
	TypeEnderDragon:   {rangeChunks: 10, updateInterval: 3},
	TypeCow:           {rangeChunks: 10, updateInterval: 3},
	TypeMooshroom:     {rangeChunks: 10, updateInterval: 3},
	TypePig:           {rangeChunks: 10, updateInterval: 3},
	TypeSheep:         {rangeChunks: 10, updateInterval: 3},
	TypeChicken:       {rangeChunks: 10, updateInterval: 3},
	TypeTurtle:        {rangeChunks: 10, updateInterval: 3},
	TypeHappyGhast:    {rangeChunks: 10, updateInterval: 3},
	TypeBat:           {rangeChunks: 5, updateInterval: 3},
	TypeHorse:         {rangeChunks: 10, updateInterval: 3},
	TypeDonkey:        {rangeChunks: 10, updateInterval: 3},
	TypeMule:          {rangeChunks: 8, updateInterval: 3},
	TypeLlama:         {rangeChunks: 10, updateInterval: 3},
	TypeTraderLlama:   {rangeChunks: 10, updateInterval: 3},
	TypeSkeletonHorse: {rangeChunks: 10, updateInterval: 3},
	TypeZombieHorse:   {rangeChunks: 10, updateInterval: 3},
	// vanilla: EntityTypes.ZOMBIE, SPIDER, CREEPER, SKELETON and the variants
	TypeZombie:         {rangeChunks: 8, updateInterval: 3},
	TypeSpider:         {rangeChunks: 8, updateInterval: 3},
	TypeCreeper:        {rangeChunks: 8, updateInterval: 3},
	TypeEnderman:       {rangeChunks: 8, updateInterval: 3},
	TypeWitch:          {rangeChunks: 8, updateInterval: 3},
	TypeSkeleton:       {rangeChunks: 8, updateInterval: 3},
	TypeHusk:           {rangeChunks: 8, updateInterval: 3},
	TypeStray:          {rangeChunks: 8, updateInterval: 3},
	TypeBogged:         {rangeChunks: 8, updateInterval: 3},
	TypeParched:        {rangeChunks: 8, updateInterval: 3},
	TypeWitherSkeleton: {rangeChunks: 8, updateInterval: 3},
	TypeCaveSpider:     {rangeChunks: 8, updateInterval: 3},
	TypeDrowned:        {rangeChunks: 8, updateInterval: 3},
	TypeVillager:       {rangeChunks: 10, updateInterval: 3},
	TypeZombieVillager: {rangeChunks: 8, updateInterval: 3},
	TypeSlime:          {rangeChunks: 10, updateInterval: 3},
	TypeMagmaCube:      {rangeChunks: 8, updateInterval: 3},
	TypePhantom:        {rangeChunks: 8, updateInterval: 3},
	TypeEndermite:      {rangeChunks: 8, updateInterval: 3},
	TypeSilverfish:     {rangeChunks: 8, updateInterval: 3},
	TypeRabbit:         {rangeChunks: 8, updateInterval: 3},
	TypeFox:            {rangeChunks: 8, updateInterval: 3},

	// vanilla: EntityTypes.POLAR_BEAR
	TypePolarBear: {rangeChunks: 10, updateInterval: 3},

	// vanilla: EntityTypes.ZOMBIFIED_PIGLIN
	TypeZombifiedPiglin: {rangeChunks: 8, updateInterval: 3},
	TypeCat:             {rangeChunks: 8, updateInterval: 3},
	TypeOcelot:          {rangeChunks: 10, updateInterval: 3},
	// vanilla: EntityTypes.WANDERING_TRADER
	TypeWanderingTrader: {rangeChunks: 10, updateInterval: 3},
	TypeGoat:            {rangeChunks: 10, updateInterval: 3},
	// vanilla: EntityTypes.WOLF, IRON_GOLEM, GLOW_SQUID, SQUID, SNOW_GOLEM
	TypeWolf:      {rangeChunks: 10, updateInterval: 3},
	TypeIronGolem: {rangeChunks: 10, updateInterval: 3},
	TypeGlowSquid: {rangeChunks: 10, updateInterval: 3},
	TypeSquid:     {rangeChunks: 8, updateInterval: 3},
	TypeSnowGolem: {rangeChunks: 8, updateInterval: 3},
}

// The builder defaults, used for an unknown kind rather than a zero
// interval.
const (
	defaultTrackingRange    = 5
	defaultUpdateInterval   = 3
	itemTrackingRangeChunks = 6
	itemUpdateIntervalTicks = 20
)

// EntityTypes.TNT: clientTrackingRange(10), updateInterval(10).
const (
	TNTTrackingRange  = 10
	TNTUpdateInterval = 10
)

// EntityTypes.FIREWORK_ROCKET: clientTrackingRange(4), updateInterval(10).
const (
	FireworkTrackingRange  = 4
	FireworkUpdateInterval = 10
)

// MobTrackingRange is the type's clientTrackingRange in chunks, compared
// against a watcher's horizontal distance; 5 when unlisted.
func MobTrackingRange(t MobType) int {
	if uint32(t) < uint32(len(mobTracking)) && mobTracking[t].updateInterval != 0 {
		k := mobTracking[t]
		return k.rangeChunks
	}
	return defaultTrackingRange
}

// MobUpdateInterval is the type's updateInterval; 3 when unlisted.
func MobUpdateInterval(t MobType) int {
	if uint32(t) < uint32(len(mobTracking)) && mobTracking[t].updateInterval != 0 {
		k := mobTracking[t]
		return k.updateInterval
	}
	return defaultUpdateInterval
}

// SpectatableType reports whether clients are ever shown the type.
// minecraft:marker is the only type with clientTrackingRange 0, so the only
// one /spectate refuses.
// vanilla: EntityTypes.MARKER
func SpectatableType(name string) bool {
	return name != "minecraft:marker"
}

// ItemTrackingRange is minecraft:item's clientTrackingRange, six chunks.
const ItemTrackingRange = itemTrackingRangeChunks

// ItemUpdateInterval is the minecraft:item type's updateInterval, twenty.
const ItemUpdateInterval = itemUpdateIntervalTicks

// EntityTypes.PLAYER: clientTrackingRange(32), updateInterval(2).
const (
	PlayerTrackingRange  = 32
	PlayerUpdateInterval = 2
)

// EntityTypes.FALLING_BLOCK: clientTrackingRange(10), updateInterval(20).
const (
	FallingBlockTrackingRange  = 10
	FallingBlockUpdateInterval = 20
)

// EntityTypes.PAINTING: clientTrackingRange(10), updateInterval
// Integer.MAX_VALUE (it never moves).
const PaintingTrackingRange = 10

// EntityTypes.ARMOR_STAND: clientTrackingRange(10), the builder's default
// updateInterval of 3.
const (
	ArmorStandTrackingRange = 10
	ArmorStandInterval      = 3
)

// EntityTypes.ITEM_FRAME and GLOW_ITEM_FRAME: the painting's
// clientTrackingRange(10) and updateInterval Integer.MAX_VALUE.
const ItemFrameTrackingRange = 10
