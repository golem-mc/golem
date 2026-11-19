// Package effect holds a living body's status effects: instances and their
// stacking, the per-tick countdown, effect-tick timing and the saved form.
// What an effect does is package rules'; what the client sees, package server's.
package effect

import (
	"math/bits"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Infinite is the duration of an effect that never runs out.
const Infinite = -1

// Instance is one active effect (vanilla: MobEffectInstance). Hidden is the
// weaker or shorter same-effect instance that takes over when this one ends.
type Instance struct {
	ID        int32 // minecraft:mob_effect ID
	Amplifier int32
	Duration  int32
	Ambient   bool
	Visible   bool // show_particles
	ShowIcon  bool
	Hidden    *Instance
}

// FromSpec is the instance a component's effect entry describes.
func FromSpec(spec registry.EffectSpec) (Instance, bool) {
	id, ok := registry.MobEffectID(spec.Effect)
	if !ok {
		return Instance{}, false
	}
	return New(id, spec.Duration, spec.Amplifier, spec.Ambient, spec.ShowParticles, spec.ShowIcon), true
}

// FromWireSpec is the instance a potion_contents custom effect describes, by
// its raw registry ID, with its hidden twin.
// vanilla: MobEffectInstance.Details
func FromWireSpec(spec protocol.PotionEffectSpec) Instance {
	in := New(spec.ID, spec.Duration, spec.Amplifier, spec.Ambient, spec.ShowParticles, spec.ShowIcon)
	if spec.Hidden != nil {
		hidden := FromWireSpec(*spec.Hidden)
		in.Hidden = &hidden
	}
	return in
}

// WireSpec is the potion_contents custom effect an instance saves as, with
// its hidden chain; the inverse of FromWireSpec.
// vanilla: MobEffectInstance.asDetails
func WireSpec(in Instance) protocol.PotionEffectSpec {
	spec := protocol.PotionEffectSpec{ID: in.ID, Amplifier: in.Amplifier, Duration: in.Duration,
		Ambient: in.Ambient, ShowParticles: in.Visible, ShowIcon: in.ShowIcon}
	if in.Hidden != nil {
		hidden := WireSpec(*in.Hidden)
		spec.Hidden = &hidden
	}
	return spec
}

// Clone copies the instance with its hidden chain, so each body handed it
// counts down alone.
// vanilla: the MobEffectInstance copy constructor
func Clone(in Instance) Instance {
	if in.Hidden == nil {
		return in
	}
	return Instance{ID: in.ID, Duration: in.Duration, Amplifier: in.Amplifier,
		Ambient: in.Ambient, Visible: in.Visible, ShowIcon: in.ShowIcon,
		Hidden: cloneInstance(in.Hidden)}
}

// New builds an instance, clamping the amplifier to 0..255.
// vanilla: the MobEffectInstance constructor
func New(id, duration, amplifier int32, ambient, visible, showIcon bool) Instance {
	return Instance{
		ID: id, Duration: duration, Amplifier: min(max(amplifier, 0), 255),
		Ambient: ambient, Visible: visible, ShowIcon: showIcon,
	}
}

// IsInfinite reports whether the instance never runs out.
func (in *Instance) IsInfinite() bool { return in.Duration == Infinite }

func (in *Instance) shorterThan(other *Instance) bool {
	return !in.IsInfinite() && (in.Duration < other.Duration || other.IsInfinite())
}

// copyDetails is vanilla's setDetailsFrom: everything but the hidden chain.
func (in *Instance) copyDetails(from *Instance) {
	in.Duration, in.Amplifier = from.Duration, from.Amplifier
	in.Ambient, in.Visible, in.ShowIcon = from.Ambient, from.Visible, from.ShowIcon
}

// detached is a copy without the hidden chain (vanilla's copy constructor).
func (in *Instance) detached() *Instance {
	c := &Instance{ID: in.ID}
	c.copyDetails(in)
	return c
}

// update merges a same-effect instance into this one and reports whether the
// client's view changed. A stronger one takes over, hiding this one if it
// outlasts it; a longer weaker one is hidden behind this one.
// vanilla: MobEffectInstance.update
func (in *Instance) update(takeOver *Instance) bool {
	changed := false
	if takeOver.Amplifier > in.Amplifier {
		if takeOver.shorterThan(in) {
			prev := in.Hidden
			in.Hidden = in.detached()
			in.Hidden.Hidden = prev
		}
		in.Amplifier = takeOver.Amplifier
		in.Duration = takeOver.Duration
		changed = true
	} else if in.shorterThan(takeOver) {
		switch {
		case takeOver.Amplifier == in.Amplifier:
			in.Duration = takeOver.Duration
			changed = true
		case in.Hidden == nil:
			in.Hidden = takeOver.detached()
		default:
			in.Hidden.update(takeOver)
		}
	}
	if !takeOver.Ambient && in.Ambient || changed {
		in.Ambient = takeOver.Ambient
		changed = true
	}
	if takeOver.Visible != in.Visible {
		in.Visible = takeOver.Visible
		changed = true
	}
	if takeOver.ShowIcon != in.ShowIcon {
		in.ShowIcon = takeOver.ShowIcon
		changed = true
	}
	return changed
}

func (in *Instance) tickDown() {
	if in.Hidden != nil {
		in.Hidden.tickDown()
	}
	if !in.IsInfinite() && in.Duration != 0 {
		in.Duration--
	}
}

// downgrade lets the hidden instance take over once this one has run out.
func (in *Instance) downgrade() bool {
	if in.Duration == 0 && in.Hidden != nil {
		in.copyDetails(in.Hidden)
		in.Hidden = in.Hidden.Hidden
		return true
	}
	return false
}

// ShouldApply reports whether an effect's own tick falls due, given the
// duration left (the body's age for an infinite one).
// vanilla: MobEffect.shouldApplyEffectTickThisTick and its overrides
func ShouldApply(id, tickCount, amplifier int32) bool {
	var interval int32
	switch id {
	case poison:
		interval = 25
	case wither:
		interval = 40
	case regeneration:
		interval = 50
	case hunger, absorption:
		return true
	default:
		return registry.MobEffectOf(id).Instant && tickCount >= 1
	}
	// Java's int >> masks the distance to its low five bits.
	if interval >>= amplifier & 31; interval > 0 {
		return tickCount%interval == 0
	}
	return true
}

const (
	poison       = registry.MobEffectPoison
	wither       = registry.MobEffectWither
	regeneration = registry.MobEffectRegeneration
	hunger       = registry.MobEffectHunger
	absorption   = registry.MobEffectAbsorption
)

// ChangeKind is what happened to an effect, for the packets it needs.
type ChangeKind uint8

const (
	// Added is a new effect (Update Mob Effect with the blend flag).
	Added ChangeKind = iota
	// Updated is a changed or refreshed one (Update Mob Effect).
	Updated
	// Removed is one that ended or was taken away (Remove Mob Effect).
	Removed
)

// Change is one effect's change, with the instance as it now stands.
type Change struct {
	Kind     ChangeKind
	Instance Instance
}

// Set is the active effects of one body, at most one instance per effect.
// The zero value is empty and ready.
type Set struct {
	active [64]*Instance
	mask   uint64
}

// Add applies an effect, reporting the change if there was one.
// vanilla: LivingEntity.addEffect
func (s *Set) Add(in Instance) (Change, bool) {
	if uint32(in.ID) >= uint32(len(s.active)) {
		return Change{}, false
	}
	cur := s.active[in.ID]
	if cur == nil {
		added := cloneInstance(&in)
		s.active[in.ID] = added
		s.mask |= uint64(1) << in.ID
		return Change{Kind: Added, Instance: *cloneInstance(added)}, true
	}
	if cur.update(&in) {
		return Change{Kind: Updated, Instance: *cloneInstance(cur)}, true
	}
	return Change{}, false
}

// Remove takes one effect away, reporting whether it was there.
func (s *Set) Remove(id int32) (Change, bool) {
	if uint32(id) >= uint32(len(s.active)) || s.active[id] == nil {
		return Change{}, false
	}
	in := s.active[id]
	s.active[id] = nil
	s.mask &^= uint64(1) << id
	return Change{Kind: Removed, Instance: *cloneInstance(in)}, true
}

// Clear takes every effect away (vanilla: LivingEntity.removeAllEffects).
func (s *Set) Clear() []Change {
	var changes []Change
	for mask := s.mask; mask != 0; mask &= mask - 1 {
		id := int32(bits.TrailingZeros64(mask))
		ch, _ := s.Remove(id)
		changes = append(changes, ch)
	}
	return changes
}

// Get returns the active instance of an effect.
func (s *Set) Get(id int32) (Instance, bool) {
	if uint32(id) >= uint32(len(s.active)) || s.active[id] == nil {
		return Instance{}, false
	}
	return *cloneInstance(s.active[id]), true
}

// Amplifier is the amplifier of an active effect, without the copy Get makes.
func (s *Set) Amplifier(id int32) (int32, bool) {
	if uint32(id) >= uint32(len(s.active)) || s.active[id] == nil {
		return 0, false
	}
	return s.active[id].Amplifier, true
}

// Clone deep-copies the set and its hidden chains, for a conversion's new
// body, so the two never share a countdown.
func (s *Set) Clone() Set {
	out := Set{mask: s.mask}
	for mask := s.mask; mask != 0; mask &= mask - 1 {
		id := bits.TrailingZeros64(mask)
		out.active[id] = cloneInstance(s.active[id])
	}
	return out
}

// cloneInstance deep-copies an instance's hidden chain, so a copy handed out
// of the owner's lock shares no countdown with the live one.
func cloneInstance(in *Instance) *Instance {
	copied := *in
	if in.Hidden != nil {
		copied.Hidden = cloneInstance(in.Hidden)
	}
	return &copied
}

// All returns the active instances in effect ID order.
func (s *Set) All() []Instance {
	// Most bodies have no effects; keep this path small enough to inline.
	if s.mask == 0 {
		return []Instance{}
	}
	return s.all()
}

func (s *Set) all() []Instance {
	all := make([]Instance, 0, bits.OnesCount64(s.mask))
	for mask := s.mask; mask != 0; mask &= mask - 1 {
		all = append(all, *cloneInstance(s.active[bits.TrailingZeros64(mask)]))
	}
	return all
}

// Tick runs one game tick of every effect. apply carries out a due effect
// tick and reports whether the effect stays; age is the body's tick count,
// an infinite effect's rhythm.
// vanilla: LivingEntity.tickEffects, MobEffectInstance.tickServer
func (s *Set) Tick(age int32, apply func(in *Instance) bool) []Change {
	var changes []Change
	for mask := s.mask; mask != 0; mask &= mask - 1 {
		id := int32(bits.TrailingZeros64(mask))
		in := s.active[id]
		keep, downgraded := in.tick(age, apply)
		if downgraded {
			changes = append(changes, Change{Kind: Updated, Instance: *cloneInstance(in)})
		}
		if !keep {
			s.active[id] = nil
			s.mask &^= uint64(1) << id
			changes = append(changes, Change{Kind: Removed, Instance: *cloneInstance(in)})
			continue
		}
		if in.Duration%600 == 0 {
			changes = append(changes, Change{Kind: Updated, Instance: *cloneInstance(in)})
		}
	}
	return changes
}

// tick reports whether the instance stays and whether a hidden one took over.
// vanilla: MobEffectInstance.tickServer
func (in *Instance) tick(age int32, apply func(in *Instance) bool) (keep, downgraded bool) {
	if !in.IsInfinite() && in.Duration <= 0 {
		return false, false
	}
	tickCount := in.Duration
	if in.IsInfinite() {
		tickCount = age
	}
	if ShouldApply(in.ID, tickCount, in.Amplifier) && !apply(in) {
		return false, false
	}
	in.tickDown()
	downgraded = in.downgrade()
	return in.IsInfinite() || in.Duration > 0, downgraded
}

// Particle is one of the particles a body with effects gives off: the
// particle type and, for minecraft:entity_effect, its ARGB colour.
type Particle struct {
	Type  string
	Color int32
}

// ambientAlpha is the alpha of an ambient effect's particles,
// Mth.floor(38.25f) in vanilla's MobEffect.
const ambientAlpha = 38

// Particles is the effect particle metadata: one particle per visible effect,
// and whether every visible one is ambient.
// vanilla: LivingEntity.updateSynchronizedMobEffectParticles
func Particles(active []Instance) ([]Particle, bool) {
	var particles []Particle
	ambient := true
	for _, in := range active {
		if !in.Visible {
			continue
		}
		ambient = ambient && in.Ambient
		props := registry.MobEffectOf(in.ID)
		if props.Particle != "" {
			particles = append(particles, Particle{Type: props.Particle})
			continue
		}
		alpha := int32(255)
		if in.Ambient {
			alpha = ambientAlpha
		}
		particles = append(particles, Particle{Type: "minecraft:entity_effect", Color: alpha<<24 | props.Color&0xFFFFFF})
	}
	return particles, ambient
}
