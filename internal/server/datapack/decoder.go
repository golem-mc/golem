package datapack

import "github.com/golem-mc/golem/registry"

// ResourceDecoder resolves file codecs against one reload's registry layers.
type ResourceDecoder struct {
	tags   *registry.TagLookup
	layers *registry.PackLayers
}

// NewResourceDecoder captures the layers' tag snapshot for decoding.
func NewResourceDecoder(layers *registry.PackLayers) ResourceDecoder {
	return ResourceDecoder{tags: layers.TagLookup(), layers: layers}
}

func liveResourceDecoder() ResourceDecoder {
	return NewResourceDecoder(nil)
}
