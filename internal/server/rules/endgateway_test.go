package rules

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestEndGatewayExitReadsAListOfInts(t *testing.T) {
	tag := nbt.Compound{"exit_portal": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(30), nbt.Int(100), nbt.Int(-30)}}}
	x, y, z, _, ok := EndGatewayExit(tag)
	if !ok || x != 30 || y != 100 || z != -30 {
		t.Errorf("exit = %d %d %d ok=%v, want 30 100 -30", x, y, z, ok)
	}
}

// cellsReader is a BlockReader over a map; every other cell is air.
type cellsReader map[[3]int32]uint16

func (c cellsReader) BlockAt(x, y, z int32) uint16 { return c[[3]int32{x, y, z}] }

func TestEndGatewayTentativeStepsBackPastFilledChunksThenForwardPastEmptyOnes(t *testing.T) {
	// Chunks from x=1000 on are filled: from 1024 the search steps back by 16
	// to the empty 992, then forward to the first filled chunk, 1008.
	x, z := EndGatewayTentative(5, 0, func(x, _ float64) bool { return x < 1000 })
	if x != 1008 || z != 0 {
		t.Errorf("tentative = (%v, %v), want (1008, 0)", x, z)
	}
	// An origin gateway has no direction and stays at the origin.
	if x, z := EndGatewayTentative(0, 0, func(_, _ float64) bool { return true }); x != 0 || z != 0 {
		t.Errorf("tentative from the origin = (%v, %v), want (0, 0)", x, z)
	}
}

func TestEndGatewayTallestCountsBedrockAndTheCentreOnlyWhenAllowed(t *testing.T) {
	bedrock := registry.MustState("minecraft:bedrock", nil)
	stone := registry.MustState("minecraft:stone", nil)
	w := cellsReader{{0, 50, 0}: bedrock, {3, 40, 0}: stone}
	if x, y, z := EndGatewayTallest(w, 0, 10, 0, 5, false, 0, 255); x != 3 || y != 40 || z != 0 {
		t.Errorf("without bedrock = (%d, %d, %d), want (3, 40, 0)", x, y, z)
	}
	if x, y, z := EndGatewayTallest(w, 0, 10, 0, 5, true, 0, 255); x != 0 || y != 50 || z != 0 {
		t.Errorf("with bedrock = (%d, %d, %d), want (0, 50, 0)", x, y, z)
	}
	if x, y, z := EndGatewayTallest(cellsReader{}, 7, 10, 8, 5, true, 0, 255); x != 7 || y != 10 || z != 8 {
		t.Errorf("over nothing = (%d, %d, %d), want the centre (7, 10, 8)", x, y, z)
	}
}

func TestEndGatewayValidSpawnTakesTheEndStoneNearestTheOrigin(t *testing.T) {
	endStone := registry.MustState("minecraft:end_stone", nil)
	stone := registry.MustState("minecraft:stone", nil)
	w := cellsReader{
		{5, 60, 5}: endStone,
		{2, 60, 2}: endStone,
		{1, 60, 1}: endStone, {1, 61, 1}: stone, // roofed: no room to stand
		{3, 20, 3}: endStone, // below y 30
	}
	x, y, z, ok := EndGatewayValidSpawn(w, 0, 0, 100)
	if !ok || x != 2 || y != 60 || z != 2 {
		t.Errorf("spawn = (%d, %d, %d) ok=%v, want (2, 60, 2)", x, y, z, ok)
	}
	if _, _, _, ok := EndGatewayValidSpawn(cellsReader{}, 0, 0, 100); ok {
		t.Error("an empty chunk gave a spawn")
	}
}

func TestEndChunkTopIsTheTopOfTheHighestFilledSection(t *testing.T) {
	stone := registry.MustState("minecraft:stone", nil)
	if top, filled := EndChunkTop(cellsReader{{3, 70, 4}: stone}, 0, 0, 0, 255); !filled || top != 79 {
		t.Errorf("top = %d filled=%v, want 79 true", top, filled)
	}
	if _, filled := EndChunkTop(cellsReader{{20, 70, 4}: stone}, 0, 0, 0, 255); filled {
		t.Error("a block in the next chunk filled this one")
	}
}
