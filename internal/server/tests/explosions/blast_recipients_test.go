package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ServerLevel.explode
func TestExplosionPacketExcludesThe64BlockBoundary(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Watcher")

	srv.BlastAt(pos.X+63.999, pos.Y, pos.Z, 4)
	boom := servertest.ReadExplosion(c, c.ExpectWithinTicks(srv, 1, protocol.IDPlayExplosion))
	if boom.X != pos.X+63.999 || boom.Y != pos.Y || boom.Z != pos.Z {
		t.Fatalf("explosion center = (%v, %v, %v), want (%v, %v, %v)",
			boom.X, boom.Y, boom.Z, pos.X+63.999, pos.Y, pos.Z)
	}

	srv.BlastAt(pos.X+64, pos.Y, pos.Z, 4)
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayExplosion))
}
