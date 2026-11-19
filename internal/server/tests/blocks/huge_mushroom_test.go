package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestHugeMushroomBlocksHideFacesJoinedToTheirOwnKind checks that a huge
// mushroom block is placed with a face off toward a block of its own kind,
// that the neighbour drops the face toward the new block, and that another
// kind of mushroom block does not count.
func TestHugeMushroomBlocksHideFacesJoinedToTheirOwnKind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()

	const brown, red = "minecraft:brown_mushroom_block", "minecraft:red_mushroom_block"
	servertest.SetFarmBlock(t, c, srv, 1, 63, 0, brown, nil)

	state := func(name string, off ...string) uint16 {
		t.Helper()
		props := map[string]string{"north": "true", "south": "true", "east": "true", "west": "true", "up": "true", "down": "true"}
		for _, f := range off {
			props[f] = "false"
		}
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state for %s %v", name, props)
		}
		return s
	}
	place := func(item string, x, y, z, face int32) {
		t.Helper()
		id, _ := registry.ItemID(item)
		srv.GiveSlot("Camper", player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 5})
		c.PlaceBlock(x, y, z, face)
		c.SettleKeeping()
		srv.FlushBlockAcks()
	}
	check := func(x, y, z int32, want uint16) {
		t.Helper()
		if got := srv.BlockAt(x, y, z); got != want {
			t.Errorf("block at (%d, %d, %d) = %d, want %d", x, y, z, got, want)
		}
	}

	// Above the first: its down face is off, and the first loses its up face.
	place(brown, 1, 63, 0, protocol.FaceTop)
	check(1, 64, 0, state(brown, "down"))
	check(1, 63, 0, state(brown, "up"))

	// Beside the second: west is off on the new block, east on the second.
	place(brown, 1, 64, 0, protocol.FaceEast)
	check(2, 64, 0, state(brown, "west"))
	check(1, 64, 0, state(brown, "down", "east"))

	// A red block on the second keeps every face, and so does the second.
	place(red, 1, 64, 0, protocol.FaceTop)
	check(1, 65, 0, state(red))
	check(1, 64, 0, state(brown, "down", "east"))

	// The stem is a huge mushroom block too.
	const stem = "minecraft:mushroom_stem"
	place(stem, 1, 65, 0, protocol.FaceEast)
	check(2, 65, 0, state(stem))
	place(stem, 2, 65, 0, protocol.FaceTop)
	check(2, 66, 0, state(stem, "down"))
	check(2, 65, 0, state(stem, "up"))
}
