package rules

import "math"

// MaxFlyingTicks is how many consecutive hovering ticks a body's gravity permits.
// vanilla: ServerGamePacketListenerImpl.getMaximumFlyingTicks
func MaxFlyingTicks(gravity float64) int {
	if gravity < float64(float32(1e-5)) {
		return math.MaxInt
	}
	return int(math.Ceil(80 * math.Max(0.08/gravity, 1)))
}

// FloatingStep is one reported step handed to FlightCheck.Moved.
type FloatingStep struct {
	DY             float64 // the step's vertical distance, blocks
	Spectator      bool
	AllowFlight    bool // the allow-flight setting
	MayFly         bool // the body's flight ability
	Levitation     bool
	FallFlying     bool
	SpinAttack     bool
	NoBlocksAround bool // only air within a sixteenth of the body and 0.55 below it
}

// FlightCheck counts the ticks a body has hovered above the ground.
// vanilla: ServerGamePacketListenerImpl.clientIsFloating, aboveGroundTickCount
type FlightCheck struct {
	floating bool
	ticks    int
}

// Candidate reports whether the step could leave the body floating, before
// the block scan NoBlocksAround needs.
func (s FloatingStep) Candidate() bool {
	return s.DY >= -0.03125 && !s.Spectator && !s.AllowFlight &&
		!s.MayFly && !s.Levitation && !s.FallFlying && !s.SpinAttack
}

// Moved records whether the latest step leaves the body floating.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (f *FlightCheck) Moved(s FloatingStep) {
	f.floating = s.Candidate() && s.NoBlocksAround
}

// Tick counts one tick and reports whether the body has hovered too long.
// A sleeper, a passenger and a dead body never count.
// vanilla: ServerGamePacketListenerImpl.tickPlayer
func (f *FlightCheck) Tick(exempt bool, maxTicks int) (kick bool) {
	if f.floating && !exempt {
		f.ticks++
		return f.ticks > maxTicks
	}
	f.floating = false
	f.ticks = 0
	return false
}

// Reset clears the count after a teleport.
// vanilla: ServerGamePacketListenerImpl.resetFlyingTicks
func (f *FlightCheck) Reset() { f.ticks = 0 }
