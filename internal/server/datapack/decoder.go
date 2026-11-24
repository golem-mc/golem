package datapack

import "github.com/golem-mc/golem/registry"

// ResourceDecoder resolves file codecs against one reload's tags and slot sources.
type ResourceDecoder struct {
	tags        *registry.TagLookup
	slotSources map[string]registry.SlotSource
}

// NewResourceDecoder reads the tag snapshot and the slot sources as they load.
func NewResourceDecoder(tags *registry.TagLookup, slotSources map[string]registry.SlotSource) ResourceDecoder {
	return ResourceDecoder{tags: tags, slotSources: slotSources}
}

func liveResourceDecoder() ResourceDecoder {
	return NewResourceDecoder(registry.CurrentTagLookup(), nil)
}
