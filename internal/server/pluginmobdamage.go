package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"slices"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// mobHurtGate puts a mob's hurt to EntityDamageEvent listeners.
type mobHurtGate struct{ s *Server }

func (g mobHurtGate) Listening() bool {
	return g.s.plugins != nil && g.s.plugins.Has(pluginruntime.EntityDamage)
}

// Ask fires EntityDamageEvent for a hurt the window let through, with the mob
// lock and every tick lock released so a listener may read the mob.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (g mobHurtGate) Ask(p *entity.HurtProbe) {
	s := g.s
	ent := s.mobEntityOf(p.Mob)
	if ent == nil {
		return
	}
	exploder := s.pendingExploder
	s.pendingExploder = nil
	e := &plugin.EntityDamageEvent{
		Entity: pluginLivingHandle(ent.uuid),
		Amount: float32(p.Amount),
		Final:  float32(p.Final),
	}
	// The source is resolved inside: relatedByID takes mobsMu.
	s.askUnlocked(func() {
		e.Source = s.pluginSource(ent.w, p.Type, &damageCause{entityID: p.CauseID, directUUID: p.Direct, exploder: exploder})
		s.plugins.Fire(e)
	})
	if _, live := s.herdOf(ent.w).book[p.Mob]; !live {
		p.Cancel = true
		return
	}
	p.Cancel = e.Cancelled()
	p.Amount = float64(e.Amount)
}

// askUnlocked runs fn on the tick goroutine with the lock a mob hurt holds
// released, and takes it again. On the tick goroutine every lock lockTick
// recorded is released, innermost first; mobsMu is released even when a
// plain Lock took it, as a hurt always holds it. Off the tick goroutine only
// mobsMu is held, and fn is handed to the tick goroutine, which a listener
// reading the mob needs free to run. State read before the call may be stale
// after it: a caller up the stack holding a herd slice sees mobs added or
// removed meanwhile (covered by TestPluginMobDamageHerdChangesDuringAsk, not
// proven for every caller).
func (s *Server) askUnlocked(fn func()) {
	mobsPlain := !s.mobsMu.TryLock()
	if !mobsPlain {
		s.mobsMu.Unlock() // not held after all: nothing to release
	}
	if !s.plugins.OnTick() {
		if mobsPlain {
			s.mobsMu.Unlock()
			defer s.mobsMu.Lock()
		}
		s.plugins.Call(fn)
		return
	}
	held := slices.Clone(s.tickLocks)
	if mobsPlain && !slices.Contains(held, &s.mobsMu) {
		s.mobsMu.Unlock()
		defer s.mobsMu.Lock()
	}
	for _, mu := range slices.Backward(held) {
		mu.Unlock()
	}
	s.tickLocks = s.tickLocks[:0]
	defer func() {
		for _, mu := range held {
			mu.Lock()
		}
		s.tickLocks = append(s.tickLocks, held...)
	}()
	fn()
}

// mobBarterGate puts a piglin's barter to PiglinBarterEvent listeners.
type mobBarterGate struct{ s *Server }

func (g mobBarterGate) Listening() bool {
	return g.s.plugins != nil && g.s.plugins.Has(pluginruntime.PiglinBarter)
}

// Ask fires PiglinBarterEvent with the mob lock and every tick lock released,
// like a hurt's ask; a cancel throws nothing.
// vanilla: PiglinAi.stopHoldingOffHandItem
func (g mobBarterGate) Ask(m *entity.Mob, input protocol.ItemStack, outcome []protocol.ItemStack) []protocol.ItemStack {
	s := g.s
	ent := s.mobEntityOf(m)
	if ent == nil {
		return outcome
	}
	e := &plugin.PiglinBarterEvent{Piglin: pendingMob{id: ent.uuid}, Input: pluginStack(input)}
	for _, st := range outcome {
		e.Outcome = append(e.Outcome, pluginStack(st))
	}
	s.askUnlocked(func() { s.plugins.Fire(e) })
	if e.Cancelled() {
		return nil
	}
	out := make([]protocol.ItemStack, 0, len(e.Outcome))
	for _, st := range e.Outcome {
		if p, ok := pluginProtocolStack(st); ok && !p.Empty() {
			out = append(out, p)
		}
	}
	return out
}

// mobEntityOf returns the mobEntity wrapping m in any dimension, or nil.
func (s *Server) mobEntityOf(m *entity.Mob) *mobEntity {
	for _, d := range s.allDims() {
		if e := d.mobs.book[m]; e != nil {
			return e
		}
	}
	return nil
}
