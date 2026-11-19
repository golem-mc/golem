package datapack

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// decodeTestTimeline shares the dimension timeline codec.
func decodeTestTimeline(tag nbt.Tag) (registry.EnvTimeline, error) {
	return registry.DecodeEnvTimeline(tag)
}
