package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestEndHighlandsPlaceGatewayReturn(t *testing.T) {
	t.Parallel()
	const chunkX, chunkZ = int32(-27), int32(67)
	gatewayX, gatewayY, gatewayZ := int32(-426), int32(70), int32(1079)
	c := newChunk(chunkX, chunkZ)
	NewEndGenerator(0).Generate(c)

	for _, cell := range EndGatewayPortalCells(gatewayX, gatewayY, gatewayZ) {
		if got := c.BlockAt(cell.X&15, cell.Y, cell.Z&15); got != cell.State {
			t.Errorf("gateway cell (%d, %d, %d) = %d, want %d", cell.X, cell.Y, cell.Z, got, cell.State)
		}
	}

	entities := c.carriedBlockEntities.Items
	if len(entities) != 1 {
		t.Fatalf("%d generated block entities, want one gateway", len(entities))
	}
	entry, ok := entities[0].(nbt.Compound)
	if !ok {
		t.Fatalf("generated block entitity is %T, want a compound", entities[0])
	}
	if id, _ := nbt.Get[nbt.String](entry, "id"); id != "minecraft:end_gateway" {
		t.Errorf("gateway id = %q, want minecraft:end_gateway", id)
	}
	if x, y, z := carriedPos(entry); x != gatewayX || y != gatewayY || z != gatewayZ {
		t.Errorf("gateway position = (%d, %d, %d), want (%d, %d, %d)", x, y, z, gatewayX, gatewayY, gatewayZ)
	}
	exit, ok := nbt.Get[nbt.IntArray](entry, "exit_portal")
	if !ok || !slices.Equal(exit, nbt.IntArray{100, 50, 0}) {
		t.Errorf("gateway exit = %v, ok=%v, want [100 50 0]", exit, ok)
	}
	if exact, _ := nbt.Get[nbt.Byte](entry, "ExactTeleport"); exact != 1 {
		t.Errorf("ExactTeleport = %d, want 1", exact)
	}
	if state := c.BlockAt(gatewayX&15, gatewayY, gatewayZ&15); registry.BlockOf(state) != registry.BlockEndGateway {
		t.Errorf("gateway state = %d, want an end gateway", state)
	}
}
