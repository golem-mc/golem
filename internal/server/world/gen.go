package world

// The golden files under testdata come from the vanilla server's own classes;
// `go generate ./internal/server/world` rewrites them (see tools/README.md).

//go:generate go run ../../../tools/gen overworld-feature-golden
//go:generate go run ../../../tools/gen monument-exterior-golden
//go:generate go run ../../../tools/gen monument-rooms-golden

//go:generate go run ../../../tools/gen monument-starts-golden

//go:generate go run ../../../tools/gen jungletemple-golden
