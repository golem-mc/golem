package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestATinyBlastReachesTheTorchAcrossTheFloatRoundedBoundary(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(1, 99, 0, registry.BlockObsidian.DefaultState())
	srv.SetBlock(1, 100, 0, registry.BlockTorch.DefaultState())

	// At this centre, only a float-rounded direction reaches x=1 on the
	// first step. Power 0.34 permits no second step; repeat to let that
	// ray roll enough power to overcome the torch's resistance.
	removed := false
	for range 10 {
		c.ChatCommand("summon tnt 0.7013244978332901 100.43875 0.5 {fuse:0,NoGravity:1b,explosion_power:0.34f}")
		c.AwaitSpawn(protocol.EntityTypeTnt)
		if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlayBlockUpdate:
				r := protocol.NewReader(data)
				x, y, z := r.ReadPosition()
				if x == 1 && y == 100 && z == 0 && r.ReadVarInt() == 0 {
					removed = true
				}
			case protocol.IDPlaySectionBlocks:
				pos, entries := servertest.ReadSectionBlocks(t, data)
				if pos == 6 { // section (0, 6, 0)
					for _, entry := range entries {
						if entry == 1<<8|4 { // air at local (1, 4, 0)
							removed = true
						}
					}
				}
			case protocol.IDPlayExplosion:
				return true
			}
			return false
		}) {
			t.Fatal("the tiny TNT never exploded")
		}
		if removed {
			break
		}
	}
	if !removed || srv.BlockAt(1, 100, 0) != registry.BlockAir.DefaultState() {
		t.Fatal("the client still sees the torch across the float-rounded ray boundary")
	}
}
