package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// speleothemState looks up a dry speleothem state by column direction and
// thickness.
func speleothemState(t *testing.T, name, direction, thickness string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, map[string]string{
		"vertical_direction": direction, "thickness": thickness, "waterlogged": "false",
	})
	if !ok {
		t.Fatalf("no %s %s %s", name, direction, thickness)
	}
	return s
}

func TestSulfurSpikeTipMergeRepairedByPostProcessing(t *testing.T) {
	c := newChunk(-68, 31)
	NewOverworldGenerator(0).Generate(c)
	s := c.BlockAt(-1088&15, 59, 503&15)
	if registry.BlockOf(s) != registry.BlockSulfurSpike {
		t.Fatalf("block = %v, want sulfur_spike", registry.BlockOf(s))
	}
	if got := registry.StateValue(s, registry.PropThickness); got != "tip" {
		t.Errorf("thickness = %s, want tip", got)
	}
	if got := registry.StateValue(s, registry.PropVerticalDirection); got != "up" {
		t.Errorf("direction = %s, want up", got)
	}
	if !registry.StateBool(s, registry.PropWaterlogged) {
		t.Error("not waterlogged")
	}
}
