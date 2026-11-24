package server

import (
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackContextProviders prepares a layer to publish after decoding both registries.
func loadPackContextProviders(dirs []string, tags *registry.PackTagLayer, skip func(file string, err error)) (func(), error) {
	ints, err := datapack.LoadResources(dirs, "context_int_provider", skip)
	if err != nil {
		return nil, err
	}
	floats, err := datapack.LoadResources(dirs, "context_float_provider", skip)
	if err != nil {
		return nil, err
	}
	if err := datapack.ValidatePackContextProviders(ints, floats, tags); err != nil {
		return nil, err
	}
	return func() { registry.ApplyPackContextProviders(ints, floats) }, nil
}
