package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestATeleportOfARiddenBoatIsToldToItsDriver checks that the driver, whose
// own client runs the boat and is sent none of its movements, is sent the
// Teleport Entity of a teleport, like every other watcher, also one back onto
// the spot it was last told of.
// vanilla: ServerEntity.sendChanges
func TestATeleportOfARiddenBoatIsToldToItsDriver(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Skipper", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 20, func(int32, []byte) bool { return false })

	boats := srv.SelectedEntities("minecraft:oak_boat")
	if len(boats) != 1 {
		t.Fatalf("selected %d boats, want 1", len(boats))
	}
	at := boats[0].Position()
	for j := range 3 {
		boats[0].Teleport(command.Move{Pos: player.Position{X: at.X + float64(j+1), Y: at.Y, Z: at.Z}},
			world.DimensionName, "test")
	}
	teleports := func(x float64) int {
		told := 0
		c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
			if id != protocol.IDPlayTeleportEntity {
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != boatID {
				return false
			}
			told++
			r.ReadVarInt() // position path
			if got := r.ReadF64(); got != x {
				t.Errorf("the driver was told the boat at x %v, want %v", got, x)
			}
			return false
		})
		return told
	}
	if told := teleports(at.X + 3); told != 1 {
		t.Errorf("the driver was sent %d boat Teleport Entity packets, want 1", told)
	}

	// The driver steers the boat away; none of that is told to it, so its
	// told position is still the last teleport's when the boat is put back there.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(at.X + 4.5)
		b.PutF64(at.Y)
		b.PutF64(at.Z)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	// The barrier's answer comes after the report was handled; held ticks
	// would otherwise all run before the connection goroutine reads it.
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	servertest.UntilTicks(t, srv, 200, "the driver's report taken", func() bool {
		x, _, _, _, _, ok := srv.BoatState(boatID)
		return ok && x == at.X+4.5
	})
	boats[0].Teleport(command.Move{Pos: player.Position{X: at.X + 3, Y: at.Y, Z: at.Z}}, world.DimensionName, "test")
	if told := teleports(at.X + 3); told != 1 {
		t.Errorf("the driver was sent %d Teleport Entity packets for a teleport back, want 1", told)
	}
}
