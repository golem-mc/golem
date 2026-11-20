package registry

// The tables below are generated from the vanilla server's data; `go generate
// ./registry` rewrites them (see tools/README.md), and `-run particles` picks
// one. registry/registries_gen.go is the exception: it comes from a probe of a
// running server (`DUMP=registries.json mise run gen:registries`). Each
// generator needs the reference data, so run `mise run reference:setup` and
// `mise run reference:reports` first.

//go:generate go run ../tools/gen particles
//go:generate go run ../tools/gen mobeffects
//go:generate go run ../tools/gen stats
//go:generate go run ../tools/gen gameevents
//go:generate go run ../tools/gen soundevents
//go:generate go run ../tools/gen entitytypes
//go:generate go run ../tools/gen entitycategories
//go:generate go run ../tools/gen items
//go:generate go run ../tools/gen ids
//go:generate go run ../tools/gen blocks
//go:generate go run ../tools/gen blockstates
//go:generate go run ../tools/gen tagdefs
//go:generate go run ../tools/gen placekeys
//go:generate go run ../tools/gen biomeids
//go:generate go run ../tools/gen paintings
//go:generate go run ../tools/gen damagetypes
//go:generate go run ../tools/gen sulfurarchetypes
//go:generate go run ../tools/gen equippable
//go:generate go run ../tools/gen itemdyes
//go:generate go run ../tools/gen bannerpatternitems
//go:generate go run ../tools/gen itemprops
//go:generate go run ../tools/gen enchantments
//go:generate go run ../tools/gen enchantmentproviders
//go:generate go run ../tools/gen recipes
//go:generate go run ../tools/gen recipebook
//go:generate go run ../tools/gen advancements
//go:generate go run ../tools/gen loot
//go:generate go run ../tools/gen blockresistance
//go:generate go run ../tools/gen noteblockinstruments
//go:generate go run ../tools/gen mapdecorations
//go:generate go run ../tools/gen mapcolors
//go:generate go run ../tools/gen blockuse
//go:generate go run ../tools/gen toolconversions
//go:generate go run ../tools/gen craftingremainders
//go:generate go run ../tools/gen flammability
//go:generate go run ../tools/gen ignitedbylava
//go:generate go run ../tools/gen blockitems
//go:generate go run ../tools/gen blockdrops
//go:generate go run ../tools/gen potions
//go:generate go run ../tools/gen blockshapes
//go:generate go run ../tools/gen blocksupport
//go:generate go run ../tools/gen lightdata
//go:generate go run ../tools/gen registrytables
//go:generate go run ../tools/gen blocksounds
//go:generate go run ../tools/gen blocklight
//go:generate go run ../tools/gen fuel
//go:generate go run ../tools/gen contextproviders
//go:generate go run ../tools/gen villagertrades
//go:generate go run ../tools/gen variantspawns
