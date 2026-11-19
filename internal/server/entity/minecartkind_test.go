package entity

import (
	"math"
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

func TestContainerSlowdownIsNearOneWhenEmpty(t *testing.T) {
	slots := make([]protocol.ItemStack, 27)
	if got := ContainerSlowdown(slots, false); math.Abs(got-0.995) > 1e-6 {
		t.Errorf("empty slowdown = %v, want 0.98 + 15 * 0.001", got)
	}
	if got := ContainerSlowdown(slots, true); math.Abs(got-0.98) > 1e-6 {
		t.Errorf("pending-table slowdown = %v, want 0.98", got)
	}
}
