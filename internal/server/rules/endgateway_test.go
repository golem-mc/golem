package rules

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
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
