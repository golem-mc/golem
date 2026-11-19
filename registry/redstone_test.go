package registry

import "testing"

func TestIsRedstoneConductorStateOverrides(t *testing.T) {
	for _, name := range []string{"minecraft:soul_sand", "minecraft:mud", "minecraft:stone"} {
		if !IsRedstoneConductorState(BlockStates[name]) {
			t.Errorf("%s is not a conductor", name)
		}
	}
	if IsRedstoneConductorState(BlockStates["minecraft:glass"]) {
		t.Error("glass is a conductor")
	}
}
