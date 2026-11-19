package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// swingState is vanilla's LivingEntity.SwingState reduced to what gates a new
// swing. Locked: handlers start swings while the tick advances them.
type swingState struct {
	mu       sync.Mutex
	active   bool
	ticks    int
	duration int
}

// startIfAble begins a swing unless one is past its start and not yet half
// over, and reports whether it began.
// vanilla: LivingEntity.SwingState.startIfAble
func (s *swingState) startIfAble(duration int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active && s.ticks > 0 && s.ticks <= s.duration/2 {
		return false
	}
	s.active, s.ticks, s.duration = true, 0, duration
	return true
}

// tick advances the swing. vanilla: LivingEntity.SwingState.tick
func (s *swingState) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	if s.ticks++; s.ticks-1 > s.duration {
		s.active = false
	}
}

// swingDuration is a swing's length in ticks, shortened by haste and
// lengthened by mining fatigue.
// vanilla: LivingEntity.getModifiedSwingDuration
func (c *client) swingDuration(anim registry.SwingAnimation) int {
	haste, fatigue := rules.DigEffects(c.player.Effects())
	switch {
	case haste > 0:
		return int(anim.Duration) - haste
	case fatigue > 0:
		return int(anim.Duration) + fatigue*2
	}
	return int(anim.Duration)
}

type useSwing uint8

const (
	swingNone useSwing = iota
	swingPredicted
	swingServer
)

// swingForUse relays a successful use; only SUCCESS_SERVER also animates the actor.
// Deliberately relay block uses once: vanilla's handleUseItemOn calls
// swingAndResetAttackStrength twice at tick 0, sending identical swing packets.
// The duplicate only restarts the same animation in the same tick.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn, handleUseItem, handleInteract
func (c *client) swingForUse(hand int32) {
	if c.useSwing == swingNone {
		return
	}
	if !c.swing.startIfAble(c.swingDuration(registry.DefaultSwingAnimation)) {
		return
	}
	c.attackStrengthTicks.Store(0)
	payload := swingArm(c.player, hand)
	c.srv.broadcastExcept(c, protocol.IDPlaySwingAnimation, payload)
	if c.useSwing == swingServer {
		c.send(protocol.IDPlaySwingAnimation, payload)
	}
}
