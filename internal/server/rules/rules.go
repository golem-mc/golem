// Package rules owns the game's numbers and decisions as plain functions:
// placement states, connections, fire and portal rules, and survival (hits,
// knockback, armour, regeneration, starvation, the void, falls, wear,
// meals). Each is one vanilla rule. They read blocks through this package's
// interfaces (BlockReader, FireWorld), keep no state, send nothing, start
// nothing and take no locks; the server applies their decisions.
//
// Block, item and face data come from the generated registry and the
// protocol; nothing is pinned by hand.
package rules

// BlockReader is the slice of the world the block rules read: block lookup
// by world coordinates. The world satisfies this; tests can substitute a
// stub.
type BlockReader interface {
	BlockAt(x, y, z int32) uint16
}

// FireWorld is the blocks around a spot plus the dimension's void line,
// below which nothing stands long enough to burn.
type FireWorld interface {
	BlockAt(x, y, z int32) uint16
	VoidY() int32
}
