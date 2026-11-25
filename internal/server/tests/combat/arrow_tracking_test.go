package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractArrow.onHitBlock, ServerEntity.sendChanges
func TestArrowFlightAndLodgingReachBothWatchers(t *testing.T) {
	t.Parallel()
	srv, first := servertest.Join(t, servertest.BowTestConfig(), "First", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	second := servertest.DialServer(t, srv).Join("Second")
	servertest.HoldTicks(t, srv)
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(14, y, 0, registry.BlockStone.DefaultState()) {
			t.Fatalf("could not place the wall at (14, %d, 0)", y)
		}
	}
	id := srv.ShootArrow(2.5, 66, 0.5, 2, 0, 0)
	servertest.RunTicks(t, srv, 8)
	for _, watcher := range []*servertest.Client{first, second} {
		var spawned, moved, stopped, lodged bool
		watcher.ReadTicks(srv, 0, func(packet int32, data []byte) bool {
			switch packet {
			case protocol.IDPlaySpawnEntity, protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
				protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion, protocol.IDPlayEntityMetadata:
			default:
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return false
			}
			switch packet {
			case protocol.IDPlaySpawnEntity:
				spawned = true
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
				moved = true
			case protocol.IDPlaySetEntityMotion:
				vx, vy, vz := r.ReadLpVec3()
				stopped = vx == 0 && vy == 0 && vz == 0
			case protocol.IDPlayEntityMetadata:
				lodged = servertest.ReadArrowMetadata(watcher, data).InGround
			}
			return false
		})
		if !spawned || !moved || !stopped || !lodged {
			t.Errorf("watcher %d: spawn=%v movement=%v stop=%v lodged=%v", watcher.PlayerEntityID, spawned, moved, stopped, lodged)
		}
	}
}
