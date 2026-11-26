package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: AbstractArrow.onHitEntity, ClientboundDamageEventPacket
func TestAnOwnerlessArrowIsBothCauseAndDirectOfThePlayersDamageEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Target", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.RunTicks(t, srv, 1)
	srv.ShootArrow(0.5, 64.5, -1.5, 0, 0, 1)
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("arrows = %v, want the one shot", arrows)
	}
	want := arrows[0].ID + 1
	var cause, direct int32
	found := c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlayDamageEvent {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // victim
		r.ReadVarInt() // type
		cause, direct = r.ReadVarInt(), r.ReadVarInt()
		return true
	})
	if !found {
		t.Fatal("the arrow never hurt the player")
	}
	if cause != want || direct != want {
		t.Errorf("damage event cause, direct = %d, %d; want the arrow's %d for both", cause, direct, want)
	}
}
