package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: BlockDataAccessor.setData, TheEndGatewayBlockEntity.getUpdateTag, isSpawning
func TestEndGatewayDataWriteUpdatesTheClientSpawningAge(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	readClientAge := func(want int64) {
		t.Helper()
		updates := 0
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayBlockEntityData {
				return
			}
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			if [3]int32{x, y, z} != [3]int32{12, 64, 3} {
				return
			}
			if kind := r.ReadVarInt(); kind != protocol.BlockEntityEndGateway {
				t.Errorf("block entity type = %d, want end gateway", kind)
			}
			tag, _, err := nbt.DecodeNetwork(r.Rest())
			if err != nil {
				t.Fatalf("decode gateway update: %v", err)
			}
			compound, ok := nbt.CompoundView(tag)
			if !ok || compound["Age"] != nbt.Long(want) {
				t.Errorf("gateway update = %v, want Age %dL", tag, want)
			}
			updates++
		})
		if updates != 1 {
			t.Errorf("gateway updates = %d, want 1 carrying Age %dL for the client beam", updates, want)
		}
	}
	servertest.Setblock(t, c, 12, 64, 3, "end_gateway{Age:200L}")
	servertest.RunTicks(t, srv, 1)
	readClientAge(201)
	c.ChatCommand("data get block 12 64 3 Age")
	c.ExpectCommandFeedback("commands.data.block.query", "12", "64", "3", "201L")

	c.ChatCommand("data modify block 12 64 3 Age set value 41L")
	c.ExpectCommandFeedback("commands.data.block.modified", "12", "64", "3")
	servertest.RunTicks(t, srv, 1)
	readClientAge(42)
	c.ChatCommand("data get block 12 64 3 Age")
	c.ExpectCommandFeedback("commands.data.block.query", "12", "64", "3", "42L")
}
