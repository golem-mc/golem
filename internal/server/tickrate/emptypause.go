package tickrate

import "sync/atomic"

// EmptyPause counts the ticks the server has stood empty and says when the
// world stops ticking for it. Management may change the configured seconds;
// only the main loop touches the counter.
// vanilla: MinecraftServer.tickServer (emptyTicks)
type EmptyPause struct {
	seconds atomic.Int32
	empty   int
}

// NewEmptyPause pauses after the given seconds at 20 ticks a second; zero or
// less never pauses.
func NewEmptyPause(seconds int) *EmptyPause {
	p := &EmptyPause{}
	p.SetSeconds(seconds)
	return p
}

// Seconds is the configured pause-when-empty-seconds.
func (p *EmptyPause) Seconds() int { return int(p.seconds.Load()) }

// SetSeconds changes the pause threshold without resetting the empty counter.
func (p *EmptyPause) SetSeconds(seconds int) { p.seconds.Store(int32(seconds)) }

// Tick counts one loop turn and reports whether the world is paused and
// whether this is the first paused turn. A sprint or a player resets it.
func (p *EmptyPause) Tick(players int, sprinting bool) (paused, first bool) {
	threshold := p.Seconds() * 20
	if threshold <= 0 {
		return false, false
	}
	if players == 0 && !sprinting {
		p.empty++
	} else {
		p.empty = 0
	}
	return p.empty >= threshold, p.empty == threshold
}
