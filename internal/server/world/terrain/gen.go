package terrain

// The golden files under testdata come from the 26.3 server's own classes;
// `go generate ./internal/server/world/terrain` rewrites them (see tools/README.md).

//go:generate go run ../../../../tools/gen sampler-golden
//go:generate go run ../../../../tools/gen material-golden
//go:generate go run ../../../../tools/gen terrain-golden
//go:generate go run ../../../../tools/gen overworld-biome-parameters
//go:generate go run ../../../../tools/gen overworld-biome-golden
//go:generate go run ../../../../tools/gen column-golden
//go:generate go run ../../../../tools/gen beardifier-golden

//go:generate go run ../../../../tools/gen overworld-terrain-golden

//go:generate go run ../../../../tools/gen overworld-surface-golden
