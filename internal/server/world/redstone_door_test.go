package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDoorLikeCoversBlockGroups(t *testing.T) {
	for block := range registry.Block(registry.BlockCount) {
		want := block.InGroup(registry.GroupDoor) || block.InGroup(registry.GroupTrapdoor) || block.InGroup(registry.GroupFenceGate)
		if isDoorLike[block] != want {
			t.Errorf("isDoorLike[%s] = %t, want %t", block.Name(), isDoorLike[block], want)
		}
	}
}
