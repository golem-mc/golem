// Code generated from the game-event registry report of a vanilla 26.3 server. DO NOT EDIT.

package registry

// GameEvent is an internal game-event ID: the registry ID plus one, reserving zero for none.
type GameEvent uint8

// GameEventNone is the absence of a registered game event.
const GameEventNone GameEvent = 0

// GameEventCount includes the reserved zero entry.
const GameEventCount = 62

// The registered game events.
const (
	GameEventBlockActivate               GameEvent = 1
	GameEventBlockAttach                 GameEvent = 2
	GameEventBlockChange                 GameEvent = 3
	GameEventBlockClose                  GameEvent = 4
	GameEventBlockDeactivate             GameEvent = 5
	GameEventBlockDestroy                GameEvent = 6
	GameEventBlockDetach                 GameEvent = 7
	GameEventBlockOpen                   GameEvent = 8
	GameEventBlockPlace                  GameEvent = 9
	GameEventBounce                      GameEvent = 10
	GameEventContainerClose              GameEvent = 11
	GameEventContainerOpen               GameEvent = 12
	GameEventDrink                       GameEvent = 13
	GameEventEat                         GameEvent = 14
	GameEventElytraGlide                 GameEvent = 15
	GameEventEntityDamage                GameEvent = 16
	GameEventEntityDie                   GameEvent = 17
	GameEventEntityDismount              GameEvent = 18
	GameEventEntityInteract              GameEvent = 19
	GameEventEntityMount                 GameEvent = 20
	GameEventEntityPlace                 GameEvent = 21
	GameEventEntityAction                GameEvent = 22
	GameEventEquip                       GameEvent = 23
	GameEventExplode                     GameEvent = 24
	GameEventFlap                        GameEvent = 25
	GameEventFluidPickup                 GameEvent = 26
	GameEventFluidPlace                  GameEvent = 27
	GameEventHitGround                   GameEvent = 28
	GameEventInstrumentPlay              GameEvent = 29
	GameEventItemInteractFinish          GameEvent = 30
	GameEventItemInteractStart           GameEvent = 31
	GameEventJukeboxPlay                 GameEvent = 32
	GameEventJukeboxStopPlay             GameEvent = 33
	GameEventLightningStrike             GameEvent = 34
	GameEventNoteBlockPlay               GameEvent = 35
	GameEventPrimeFuse                   GameEvent = 36
	GameEventProjectileLand              GameEvent = 37
	GameEventProjectileShoot             GameEvent = 38
	GameEventSculkSensorTendrilsClicking GameEvent = 39
	GameEventShear                       GameEvent = 40
	GameEventShriek                      GameEvent = 41
	GameEventSplash                      GameEvent = 42
	GameEventStep                        GameEvent = 43
	GameEventSwim                        GameEvent = 44
	GameEventTeleport                    GameEvent = 45
	GameEventUnequip                     GameEvent = 46
	GameEventResonate1                   GameEvent = 47
	GameEventResonate2                   GameEvent = 48
	GameEventResonate3                   GameEvent = 49
	GameEventResonate4                   GameEvent = 50
	GameEventResonate5                   GameEvent = 51
	GameEventResonate6                   GameEvent = 52
	GameEventResonate7                   GameEvent = 53
	GameEventResonate8                   GameEvent = 54
	GameEventResonate9                   GameEvent = 55
	GameEventResonate10                  GameEvent = 56
	GameEventResonate11                  GameEvent = 57
	GameEventResonate12                  GameEvent = 58
	GameEventResonate13                  GameEvent = 59
	GameEventResonate14                  GameEvent = 60
	GameEventResonate15                  GameEvent = 61
)

var gameEventNames = [GameEventCount]string{
	"",
	"minecraft:block_activate",
	"minecraft:block_attach",
	"minecraft:block_change",
	"minecraft:block_close",
	"minecraft:block_deactivate",
	"minecraft:block_destroy",
	"minecraft:block_detach",
	"minecraft:block_open",
	"minecraft:block_place",
	"minecraft:bounce",
	"minecraft:container_close",
	"minecraft:container_open",
	"minecraft:drink",
	"minecraft:eat",
	"minecraft:elytra_glide",
	"minecraft:entity_damage",
	"minecraft:entity_die",
	"minecraft:entity_dismount",
	"minecraft:entity_interact",
	"minecraft:entity_mount",
	"minecraft:entity_place",
	"minecraft:entity_action",
	"minecraft:equip",
	"minecraft:explode",
	"minecraft:flap",
	"minecraft:fluid_pickup",
	"minecraft:fluid_place",
	"minecraft:hit_ground",
	"minecraft:instrument_play",
	"minecraft:item_interact_finish",
	"minecraft:item_interact_start",
	"minecraft:jukebox_play",
	"minecraft:jukebox_stop_play",
	"minecraft:lightning_strike",
	"minecraft:note_block_play",
	"minecraft:prime_fuse",
	"minecraft:projectile_land",
	"minecraft:projectile_shoot",
	"minecraft:sculk_sensor_tendrils_clicking",
	"minecraft:shear",
	"minecraft:shriek",
	"minecraft:splash",
	"minecraft:step",
	"minecraft:swim",
	"minecraft:teleport",
	"minecraft:unequip",
	"minecraft:resonate_1",
	"minecraft:resonate_2",
	"minecraft:resonate_3",
	"minecraft:resonate_4",
	"minecraft:resonate_5",
	"minecraft:resonate_6",
	"minecraft:resonate_7",
	"minecraft:resonate_8",
	"minecraft:resonate_9",
	"minecraft:resonate_10",
	"minecraft:resonate_11",
	"minecraft:resonate_12",
	"minecraft:resonate_13",
	"minecraft:resonate_14",
	"minecraft:resonate_15",
}
