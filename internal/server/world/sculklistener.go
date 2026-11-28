package world

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The vibration listener's saved form. The live listener is the engine's
// (Physics.listeners); each change is copied into the block entity, whose save
// writes it, and a listener a load brings in replaces the live one on the next
// tick.
// vanilla: VibrationSystem.Data.CODEC, VibrationSelector.CODEC, VibrationInfo.CODEC

// listenerKind reports a block entity kind that keeps a vibration listener.
func listenerKind(kind BlockEntityKind) bool {
	return kind == KindSculkSensor || kind == KindCalibratedSculkSensor || kind == KindSculkShrieker
}

// encode writes the listener: the vibration in flight if any, the selector's
// candidate and its tick (-1 without one), and the travel left.
func (l vibrationListener) encode() nbt.CompoundTag {
	selector := nbt.NewOrderedCompound(2)
	if l.hasCandidate {
		selector.Set("event", l.candidate.encode())
		selector.Set("tick", nbt.Long(l.candidateTick))
	} else {
		selector.Set("tick", nbt.Long(-1))
	}
	out := nbt.NewOrderedCompound(5)
	if l.hasCurrent {
		out.Set("event", l.current.encode())
	}
	out.Set("selector", selector)
	out.Set("event_delay", nbt.Int(l.travel))
	return out
}

// encode writes one vibration with its source UUID.
func (v vibrationInfo) encode() nbt.CompoundTag {
	out := nbt.NewOrderedCompound(5)
	name := v.event.Name()
	if v.unknownEvent != "" {
		name = v.unknownEvent
	}
	out.Set("game_event", nbt.String(name))
	out.Set("distance", nbt.Float(v.dist))
	out.Set("pos", nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(v.x), nbt.Double(v.y), nbt.Double(v.z)}})
	if v.player != uuid.Nil {
		out.Set("source", nbt.UUIDToIntArray(v.player))
	}
	if v.mob != uuid.Nil {
		out.Set("source", nbt.UUIDToIntArray(v.mob))
		// The world has no entity lookup after reload, so keep shrieker eligibility.
		out.Set("source_is_mob", nbt.Byte(1))
	}
	return out
}

// decodeListener reads a saved listener, false for one the codec refuses (no
// selector, a tick that is no number, a negative event_delay), which leaves a
// fresh listener. A vibration that will not read is dropped alone.
func decodeListener(c nbt.Compound) (vibrationListener, bool) {
	var l vibrationListener
	selector, ok := nbt.Get[nbt.Compound](c, "selector")
	if !ok {
		return l, false
	}
	tick, ok := codecLong(selector["tick"])
	if !ok {
		return l, false
	}
	if tag, present := c["event_delay"]; present {
		// vanilla: ExtraCodecs.NON_NEGATIVE_INT
		delay, ok := nbt.IntValue(tag)
		if !ok || delay < 0 {
			return l, false
		}
		l.travel = int(delay)
	}
	if event, ok := nbt.Get[nbt.Compound](selector, "event"); ok {
		if l.candidate, l.hasCandidate = decodeVibrationInfo(event); l.hasCandidate {
			l.candidateTick = tick
		}
	}
	if event, ok := nbt.Get[nbt.Compound](c, "event"); ok {
		l.current, l.hasCurrent = decodeVibrationInfo(event)
	}
	return l, true
}

// decodeVibrationInfo reads one vibration, false for one the codec refuses.
// The player is the projectile's owner when one is named, else the source.
// Deliberately unlike vanilla, a game event outside the vibration table is
// read with frequency 0 rather than checked against the registry.
// vanilla: VibrationInfo.CODEC, SculkShriekerBlockEntity.tryGetPlayer
func decodeVibrationInfo(c nbt.Compound) (vibrationInfo, bool) {
	name, ok := nbt.Get[nbt.String](c, "game_event")
	if !ok || name == "" || !protocol.ValidIdentifier(string(name)) {
		return vibrationInfo{}, false
	}
	// vanilla: Codec.floatRange(0, Float.MAX_VALUE)
	dist, ok := nbt.FloatField(c, "distance")
	if !ok || !(dist >= 0 && dist <= math.MaxFloat32) {
		return vibrationInfo{}, false
	}
	// vanilla: Vec3.CODEC, three doubles
	pos, ok := nbt.Get[nbt.List](c, "pos")
	if !ok || len(pos.Items) != 3 {
		return vibrationInfo{}, false
	}
	var xyz [3]float64
	for i, item := range pos.Items {
		if xyz[i], ok = doubleOf(item); !ok {
			return vibrationInfo{}, false
		}
	}
	event := registry.GameEventByName(registry.Qualify(string(name)))
	v := vibrationInfo{x: xyz[0], y: xyz[1], z: xyz[2], dist: dist, event: event, freq: VibrationFrequency(event)}
	if event == registry.GameEventNone {
		v.unknownEvent = registry.Qualify(string(name))
	}
	if owner, ok := nbt.UUIDField(c, "projectile_owner"); ok {
		v.player = owner
	} else if source, ok := nbt.UUIDField(c, "source"); ok {
		if nbt.BoolField(c, "source_is_mob") {
			v.mob = source
		} else {
			v.player = source
		}
	}
	return v, true
}

// doubleOf reads a number as Codec.DOUBLE does, false for a tag that is no
// number.
func doubleOf(tag nbt.Tag) (float64, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return float64(v), true
	case nbt.Short:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Double:
		return float64(v), true
	}
	return 0, false
}

// codecLong reads a number as Codec.LONG does, through Number.longValue: a
// fraction truncates toward zero.
func codecLong(tag nbt.Tag) (int64, bool) {
	if d, ok := tag.(nbt.Double); ok {
		tag = nbt.Double(math.Trunc(float64(d)))
	}
	return nbt.LongValue(tag)
}

// adoptListeners takes up the listeners loaded into block entities since the
// last tick, each replacing the live one at its cell; a vibration in flight
// sends its particle again.
// vanilla: SculkSensorBlockEntity.loadAdditional, SculkShriekerBlockEntity.loadAdditional
func (p *Physics) adoptListeners() {
	for _, be := range p.w.loadedListeners() {
		var key blockKey
		var l vibrationListener
		be.Use(func(b *BlockEntity) { key, l = blockKey{b.X, b.Y, b.Z}, b.listener })
		if !l.hasCandidate && !l.hasCurrent {
			delete(p.listeners, key)
			continue
		}
		l.reloadParticle = l.hasCurrent
		p.listeners[key] = &l
	}
}

// saveListener copies the cell's live listener, or a fresh one when it has
// none, into its block entity, which the chunk save reads.
func (p *Physics) saveListener(k blockKey) {
	kind := kindOfID(registry.BlockOf(p.w.BlockAt(k.x, k.y, k.z)).Name())
	if !listenerKind(kind) {
		return
	}
	var l vibrationListener
	if live := p.listeners[k]; live != nil {
		l = *live
		l.reloadParticle = false
	}
	be := p.w.BlockEntity(k.x, k.y, k.z)
	if be == nil {
		if l == (vibrationListener{}) {
			return
		}
		if be = p.w.EnsureBlockEntity(k.x, k.y, k.z, kind); be == nil {
			return
		}
	}
	be.Use(func(b *BlockEntity) { b.listener = l })
	p.w.MarkDirty(ChunkPos{X: k.x >> 4, Z: k.z >> 4})
}

// loadedListeners returns the listener-keeping block entities a load has
// filled since the last call, in insertion order.
func (w *World) loadedListeners() []*BlockEntity {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []*BlockEntity
	for _, be := range w.sculkBlockEntities.bes {
		if be != nil && listenerKind(be.Kind) && be.listenerLoaded.Swap(false) {
			out = append(out, be)
		}
	}
	return out
}
