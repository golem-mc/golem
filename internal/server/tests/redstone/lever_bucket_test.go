package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: LeverBlock.<init>, useWithoutItem, pull; ServerPlayerGameMode.useItemOn
func TestSurvivalBucketLeverPreservesFacingOnTheWire(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
		s.SetWorldSpawn(world.SpawnPos{X: 168, Y: 200, Z: 152, Pitch: 60})
		grass := servertest.BlockState(t, "minecraft:grass_block")
		for x := int32(165); x <= 171; x++ {
			for z := int32(149); z <= 155; z++ {
				s.SetLoadedBlock(x, 199, z, grass)
			}
		}
	})
	servertest.HoldTicks(t, srv)
	c.FacePlayer(168.5, 200, 152.5, 0, 60)
	// Omitting facing must keep LeverBlock's north default, independent of yaw.
	servertest.Setblock(t, c, 168, 200, 153, "minecraft:lever[face=floor,powered=false]")
	off := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=false")
	on := servertest.BlockStateWith(t, "minecraft:lever", "face=floor,facing=north,powered=true")
	if got := srv.BlockAt(168, 200, 153); got != off {
		t.Fatalf("initial lever state = %d, want floor/north/unpowered %d", got, off)
	}
	bucket := servertest.ItemStack(t, "minecraft:axolotl_bucket", 1)
	srv.GiveSlot("Tester", player.HotbarSlot, bucket)
	// Flush the command's block update before recording the interaction.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })

	const seq = 7
	c.UseBlockSeq(168, 200, 153, protocol.FaceNorth, seq)
	var last uint16
	var seen, acked bool
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		for _, ch := range servertest.BlockChangesIn(t, id, data) {
			if ch.X == 168 && ch.Y == 200 && ch.Z == 153 {
				last, seen = ch.State, true
			}
		}
		if id == protocol.IDPlayAckBlockChange {
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != seq {
				t.Fatalf("ack sequence = %d, want %d", got, seq)
			}
			acked = true
			// The client resolves its prediction at the ack using the last update.
			if !seen || last != on {
				t.Errorf("lever at ack = %d (seen %t), want floor/north/powered %d", last, seen, on)
			}
		}
		return false
	})
	if !acked || !seen || last != on {
		t.Errorf("final wire state = %d (seen %t, acked %t), want %d", last, seen, acked, on)
	}
	if got := srv.BlockAt(168, 200, 153); got != on {
		t.Errorf("server lever state = %d, want %d", got, on)
	}
	if got := srv.InventorySlot("Tester", player.HotbarSlot); got.ItemID != bucket.ItemID || got.Count != bucket.Count {
		t.Errorf("held bucket = %+v, want %+v", got, bucket)
	}
	c.ChatCommand("execute if block 168 200 153 minecraft:lever[powered=true] run say powered")
	c.ExpectSayLineAs(t, servertest.PlayerDisplayComponent("Tester"), "powered")
}
