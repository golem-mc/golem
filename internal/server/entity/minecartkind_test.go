package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestContainerSignalFollowsFullness(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	slots := make([]protocol.ItemStack, 27)
	if got := ContainerSignal(slots); got != 0 {
		t.Errorf("empty container signal = %d, want 0", got)
	}
	slots[0] = protocol.ItemStack{ItemID: stone, Count: 1}
	if got := ContainerSignal(slots); got != 1 {
		t.Errorf("one item signal = %d, want 1", got)
	}
	for i := range slots {
		slots[i] = protocol.ItemStack{ItemID: stone, Count: 64}
	}
	if got := ContainerSignal(slots); got != 15 {
		t.Errorf("full container signal = %d, want 15", got)
	}
}
