package world

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// SignResolver resolves a sign's components against its level's command state.
type SignResolver interface {
	ResolveSignLine(x, y, z int32, line nbt.Tag) nbt.Tag
}

// SetSignResolver sets the component resolver before the world is shared.
func (w *World) SetSignResolver(resolver SignResolver) { w.signResolver = resolver }

// ResolveSignTag resolves both sides of an op-enabled sign before loading its
// data. No world or entity lock may be held: components can read either.
// vanilla: SignBlockEntity.loadAdditional, resolveLines
func (w *World) ResolveSignTag(x, y, z int32, tag nbt.Compound) nbt.Compound {
	if w.signResolver == nil || !nbt.BoolField(tag, "allow_op_features") {
		return tag
	}
	out := maps.Clone(tag)
	for _, side := range []string{"front_text", "back_text"} {
		data, ok := nbt.Get[nbt.Compound](tag, side)
		if !ok {
			continue
		}
		text := decodeSignText(data)
		// The mutable copy starts both lists from the raw messages.
		// vanilla: SignText.Mutable constructor
		text.filtered = nil
		for i, raw := range text.raw {
			if raw != nil {
				text.raw[i] = w.signResolver.ResolveSignLine(x, y, z, raw)
				text.Messages[i], _ = signLiteral(text.raw[i])
			}
		}
		out[side] = text.encode()
	}
	return out
}
