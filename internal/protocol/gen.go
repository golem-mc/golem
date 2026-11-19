package protocol

// language_gen.go is generated from the vanilla client jar; `go generate
// ./protocol` rewrites it (see tools/README.md). internal/protocol/entitytypes_gen.go
// comes from the entity type generator that registry/gen.go runs.

//go:generate go run ../../tools/gen language
//go:generate go run ../../tools/gen particleids
//go:generate go run ../../tools/gen entitytypeids
//go:generate go run ../../tools/gen packets
