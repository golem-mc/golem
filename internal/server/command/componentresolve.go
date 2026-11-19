package command

import (
	"maps"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ResolveText resolves dynamic contents with src as the source, preserving
// the original text if resolution fails or the result would not fit the wire.
// vanilla: ComponentUtils.resolve
func ResolveText(src Source, t protocol.Text) protocol.Text {
	tag, ok := resolveComponent(src, src.SelfEntity(), protocol.TextTag(t))
	if !ok {
		return t
	}
	resolved, ok := protocol.RawTextOf(tag)
	if !ok {
		return t
	}
	return resolved
}

// resolveComponent resolves dynamic contents and folds a top-level list into
// its first element with siblings for stored book and bossbar components.
// Failure returns the original tag and false.
// vanilla: ComponentUtils.resolve, ComponentSerialization (list form)
func resolveComponent(src Source, scoreEntity Entity, tag nbt.Tag) (nbt.Tag, bool) {
	var key string
	r := resolver{view: src, scoreEntity: scoreEntity, refusal: &key}
	resolved := r.resolve(tag, 0)
	if key != "" || !protocol.RawTextFits(resolved) {
		return tag, false
	}
	resolved = foldComponentList(resolved)
	if !protocol.RawTextFits(resolved) {
		return tag, false
	}
	return resolved, true
}

func foldComponentList(tag nbt.Tag) nbt.Tag {
	list, ok := tag.(nbt.List)
	if !ok {
		return tag
	}
	base := nbt.Compound{"text": nbt.String("")}
	var rest []nbt.Tag
	for i, item := range list.Items {
		part := componentCompound(foldComponentList(nbt.ListElement(item)))
		if i == 0 {
			base = part
		} else {
			rest = append(rest, part)
		}
	}
	appendExtra(base, rest)
	return base
}

// componentCompound is the compound form of a resolved component; a string
// component becomes the text it is.
func componentCompound(tag nbt.Tag) nbt.Compound {
	if compound, ok := tag.(*nbt.OrderedCompound); ok {
		return maps.Clone(compound.Entries())
	}
	switch v := tag.(type) {
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		return maps.Clone(compoundFields)
	case nbt.String:
		return nbt.Compound{"text": v}
	}
	return nbt.Compound{"text": nbt.String("")}
}

// appendExtra adds siblings to a component's own, copying the list.
func appendExtra(c nbt.Compound, more []nbt.Tag) {
	var items []nbt.Tag
	if list, ok := c["extra"].(nbt.List); ok {
		items = append(items, list.Items...)
	}
	items = append(items, more...)
	if len(items) > 0 {
		c["extra"] = nbt.NewList(items)
	}
}
