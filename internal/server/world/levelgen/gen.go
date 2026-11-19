package levelgen

// The golden files under testdata come from the vanilla server's own classes;
// `go generate ./internal/server/world/levelgen` rewrites them (see tools/README.md).

//go:generate go run ../../../../tools/gen overworld-features
//go:generate go run ../../../../tools/gen placed-features
//go:generate go run ../../../../tools/gen sculk-features
//go:generate go run ../../../../tools/gen structure-golden
//go:generate go run ../../../../tools/gen overworld-trees
//go:generate go run ../../../../tools/gen overworld-trees-golden

//go:generate go run ../../../../tools/gen mansion-golden
//go:generate go run ../../../../tools/gen monument-golden
