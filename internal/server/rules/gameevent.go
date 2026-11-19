package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// GameEvent is an internal game-event ID.
type GameEvent = registry.GameEvent

// The game events the server posts.
const (
	GameEventBlockDestroy GameEvent = registry.GameEventBlockDestroy
	GameEventBlockChange  GameEvent = registry.GameEventBlockChange
	GameEventBlockPlace   GameEvent = registry.GameEventBlockPlace
	GameEventHitGround    GameEvent = registry.GameEventHitGround

	GameEventBlockClose                  GameEvent = world.GameEventBlockClose
	GameEventBlockOpen                   GameEvent = world.GameEventBlockOpen
	GameEventContainerClose              GameEvent = registry.GameEventContainerClose
	GameEventContainerOpen               GameEvent = registry.GameEventContainerOpen
	GameEventLightningStrike             GameEvent = registry.GameEventLightningStrike
	GameEventInstrumentPlay              GameEvent = registry.GameEventInstrumentPlay
	GameEventItemInteractFinish          GameEvent = registry.GameEventItemInteractFinish
	GameEventItemInteractStart           GameEvent = registry.GameEventItemInteractStart
	GameEventEntityMount                 GameEvent = registry.GameEventEntityMount
	GameEventEntityDismount              GameEvent = registry.GameEventEntityDismount
	GameEventEntityPlace                 GameEvent = registry.GameEventEntityPlace
	GameEventEntityInteract              GameEvent = registry.GameEventEntityInteract
	GameEventEntityDamage                GameEvent = registry.GameEventEntityDamage
	GameEventEntityAction                GameEvent = registry.GameEventEntityAction
	GameEventElytraGlide                 GameEvent = registry.GameEventElytraGlide
	GameEventEntityDie                   GameEvent = registry.GameEventEntityDie
	GameEventDrink                       GameEvent = registry.GameEventDrink
	GameEventEat                         GameEvent = registry.GameEventEat
	GameEventEquip                       GameEvent = registry.GameEventEquip
	GameEventUnequip                     GameEvent = registry.GameEventUnequip
	GameEventFluidPickup                 GameEvent = registry.GameEventFluidPickup
	GameEventFluidPlace                  GameEvent = registry.GameEventFluidPlace
	GameEventExplode                     GameEvent = registry.GameEventExplode
	GameEventPrimeFuse                   GameEvent = registry.GameEventPrimeFuse
	GameEventProjectileLand              GameEvent = registry.GameEventProjectileLand
	GameEventProjectileShoot             GameEvent = registry.GameEventProjectileShoot
	GameEventSculkSensorTendrilsClicking GameEvent = registry.GameEventSculkSensorTendrilsClicking
	GameEventBounce                      GameEvent = registry.GameEventBounce
	GameEventFlap                        GameEvent = registry.GameEventFlap
	GameEventSplash                      GameEvent = registry.GameEventSplash
	GameEventShear                       GameEvent = registry.GameEventShear
	GameEventShriek                      GameEvent = registry.GameEventShriek
	GameEventStep                        GameEvent = registry.GameEventStep
	GameEventSwim                        GameEvent = registry.GameEventSwim
	GameEventTeleport                    GameEvent = registry.GameEventTeleport
)

// ignoredWhenSneaking is the minecraft:ignore_vibrations_sneaking tag.
var ignoredWhenSneaking = [registry.GameEventCount]bool{
	registry.GameEventHitGround: true, registry.GameEventProjectileShoot: true, registry.GameEventStep: true,
	registry.GameEventSwim: true, registry.GameEventItemInteractStart: true, registry.GameEventItemInteractFinish: true,
}

// HiddenBySneaking reports whether a player's sneaking hides the event from
// vibration listeners. vanilla: VibrationSystem.User.isValidVibration
func HiddenBySneaking(ev GameEvent, ctx GameEventContext) bool {
	return ctx.Sneaking && !ctx.Spectator && world.VibrationFrequency(ev) != 0 && ignoredWhenSneaking[ev]
}

// GameEventContext is what a posted event says about its cause.
// vanilla: GameEvent.Context
type GameEventContext struct {
	// Spectator and Sneaking describe the source entity; both false for none.
	Spectator, Sneaking bool
	// Dampens is set when the source entity dampens vibrations: an item entity
	// holding a dampens_vibrations item. vanilla: Entity.dampensVibrations
	Dampens bool
	// Affected is the block state the event touched, 0 (air) for none.
	Affected uint16
	// Player is the UUID of the player behind the event, uuid.Nil for none.
	Player uuid.UUID
	// Mob is the UUID of the mob behind the event, uuid.Nil for none.
	Mob uuid.UUID
	// ProjectileOwner is the UUID behind a projectile event; uuid.Nil for none.
	ProjectileOwner uuid.UUID
}

// VibrationOf turns a game event at a position into the vibration the sculk
// sensors hear, false when it makes none: a non-vibrating event, a spectator
// source, a sneaking source for the events sneaking hides, and an event on a
// block that dampens vibrations (wool, carpet).
// vanilla: VibrationSystem.User.isValidVibration
func VibrationOf(ev GameEvent, x, y, z float64, ctx GameEventContext) (world.Vibration, bool) {
	freq := world.VibrationFrequency(ev)
	if freq == 0 || ctx.Spectator || ctx.Dampens || (ctx.Sneaking && ignoredWhenSneaking[ev]) {
		return world.Vibration{}, false
	}
	if registry.BlockOf(ctx.Affected).InTag(registry.TagIDDampensVibrations) {
		return world.Vibration{}, false
	}
	return world.Vibration{
		X: x, Y: y, Z: z, Event: ev, Frequency: freq,
		PlaceOrDestroy: ev == GameEventBlockPlace || ev == GameEventBlockDestroy,
		Player:         ctx.Player,
		Mob:            ctx.Mob,
	}, true
}
