package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestEndPortalInTheNetherLeadsToTheEnd stands a player in an end portal
// block in the nether: it takes them to the end's arrival platform, as it
// does from the overworld.
// vanilla: EndPortalBlock.entityInside, EndPortalBlock.getPortalDestination
func TestEndPortalInTheNetherLeadsToTheEnd(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Strider")
	srv.MovePlayerToDimension("Strider", world.NetherName, 8, 40, 8)
	c.ExpectWithinTicks(srv, crossingTicks, protocol.IDPlayRespawn)
	servertest.AwaitChunksSettled(t, srv)
	servertest.UntilTicks(t, srv, crossingTicks, "nether arrival chunk loaded", func() bool {
		return srv.ChunkLoadedInDimension(world.NetherName, 8, 8)
	})

	const y = 40
	for dy := range int32(3) {
		// False when the cell is air already.
		srv.SetBlockInDimension(world.NetherName, 8, y+dy, 8, 0)
	}
	if !srv.SetBlockInDimension(world.NetherName, 8, y, 8, srv.EndPortalState()) {
		t.Fatal("placing the end portal block in the nether failed")
	}
	srv.MovePlayerToDimension("Strider", world.NetherName, 8.5, y, 8.5)

	data := c.ExpectWithinTicks(srv, crossingTicks, protocol.IDPlayRespawn)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.EndType) {
		t.Errorf("respawn dimension type = %d, want the end's", got)
	}
	if got := r.ReadString(32767); got != world.EndName {
		t.Errorf("respawn dimension = %q, want %q", got, world.EndName)
	}
	servertest.UntilTicks(t, srv, arrivalTicks, "player arrived in the end", func() bool {
		return srv.PlayerDimension("Strider") == world.EndName
	})
	arrival := world.EndArrival()
	pos := srv.PlayerPosition("Strider")
	if pos.X != float64(arrival.X)+0.5 || pos.Y != float64(arrival.Y) || pos.Z != float64(arrival.Z)+0.5 {
		t.Errorf("end arrival = (%v, %v, %v), want the arrival platform (%v, %v, %v)",
			pos.X, pos.Y, pos.Z, float64(arrival.X)+0.5, arrival.Y, float64(arrival.Z)+0.5)
	}
}
