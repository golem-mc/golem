package server

import "github.com/golem-mc/golem/internal/server/datapack"

// loadPackContextProviders reads the enabled packs' context int and float
// providers; they are decoded where /compute names them.
// vanilla: ContextIntProviders, ContextFloatProviders
func loadPackContextProviders(dirs []string, skip func(file string, err error)) (ints, floats map[string][]byte, err error) {
	if ints, err = datapack.LoadResources(dirs, "context_int_provider", skip); err != nil {
		return nil, nil, err
	}
	floats, err = datapack.LoadResources(dirs, "context_float_provider", skip)
	return ints, floats, err
}
