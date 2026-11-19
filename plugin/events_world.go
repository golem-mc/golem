package plugin

// WorldLoadEvent, WorldUnloadEvent and WorldSaveEvent fire as a world is
// loaded, before it is unloaded, and before it is saved. Bukkit:
// WorldLoadEvent, WorldUnloadEvent, WorldSaveEvent
type WorldLoadEvent struct {
	Base
	World World
}

type WorldUnloadEvent struct {
	Cancellable
	World World
}

type WorldSaveEvent struct {
	Base
	World World
}

// ChunkLoadEvent fires when a chunk is loaded into the world, New when it
// was just generated. Bukkit: ChunkLoadEvent
type ChunkLoadEvent struct {
	Base
	Chunk Chunk
	New   bool
}

// ChunkUnloadEvent fires on the tick after a chunk left memory; its handle
// only answers what it held then. Bukkit:
// ChunkUnloadEvent
type ChunkUnloadEvent struct {
	Base
	Chunk Chunk
}

// ChunkPopulateEvent fires once a new chunk's features are placed. Bukkit:
// ChunkPopulateEvent
type ChunkPopulateEvent struct {
	Base
	Chunk Chunk
}

// PortalCreateEvent fires before a nether portal is lit or created by a
// trip, or an end platform built; Blocks may be trimmed. Bukkit:
// PortalCreateEvent
type PortalCreateEvent struct {
	Cancellable
	World  World
	Blocks []BlockChange
	Entity Entity // what made it, if anything
	Reason PortalReason
}

// PortalReason is why a portal was made.
type PortalReason uint8

const (
	PortalFire PortalReason = iota
	PortalNetherPair
	PortalEndPlatform
)

// SpawnChangeEvent fires after a world's spawn moves. Bukkit:
// SpawnChangeEvent
type SpawnChangeEvent struct {
	Base
	World    World
	Previous Location
}

// TimeSkipEvent fires when the time jumps: everyone slept, or /time.
// Skip may be changed. Bukkit: TimeSkipEvent
type TimeSkipEvent struct {
	Cancellable
	World  World
	Skip   int64
	Reason TimeSkipReason
}

// TimeSkipReason is why time jumped.
type TimeSkipReason uint8

const (
	SkipNight TimeSkipReason = iota
	SkipCommand
	SkipPlugin
)

// WeatherChangeEvent fires before a world's raining flag flips; cancelling
// keeps the current flag. New is the weather the change heads for, thunder
// included. Bukkit: WeatherChangeEvent
type WeatherChangeEvent struct {
	Cancellable
	World   World
	New     Weather
	Raining bool
}

// ThunderChangeEvent fires before a world's thundering flag flips, apart
// from the rain flag's WeatherChangeEvent; cancelling keeps the current
// flag. Bukkit: ThunderChangeEvent
type ThunderChangeEvent struct {
	Cancellable
	World      World
	Thundering bool
}

// LightningStrikeEvent fires before lightning strikes. Bukkit:
// LightningStrikeEvent
type LightningStrikeEvent struct {
	Cancellable
	World     World
	Lightning Entity
	Cause     LightningCause
}

// LightningCause is what called lightning.
type LightningCause uint8

const (
	LightningWeather LightningCause = iota
	LightningTrident
	LightningCommand
	LightningSkeletonTrap
	LightningPlugin
)
