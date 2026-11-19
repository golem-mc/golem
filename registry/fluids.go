package registry

import "strings"

// FluidStates marks, by block state ID, the states the chunk section "fluid count"
// field counts: every state of minecraft:water and minecraft:lava, plus every
// state of any block whose waterlogged property is true. The client gates
// fluid physics — buoyancy, drag, swimming — on the count being non-zero, so a
// section that holds water must report it or a player falls through as if the
// water were air.
//
// Built at init from AllStates, the same generated table the state lookup
// uses, so it tracks the block report without a generator of its own. It is
// an array rather than a map because a section's fluid count reads it for
// every block each time the section is sent.
var FluidStates = func() *[1 << 16]bool {
	m := new([1 << 16]bool)
	for name, states := range AllStates {
		fluid := name == "minecraft:water" || name == "minecraft:lava"
		for _, s := range states {
			if fluid || strings.Contains(s.Props, "waterlogged=true") {
				m[s.ID] = true
			}
		}
	}
	return m
}()
