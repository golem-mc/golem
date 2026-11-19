package blockchanges

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A platform built in a newly arrived, interior chunk needs authoritative
// skylight even when the game is frozen: block updates alone leave it dark.
// vanilla: ChunkHolder.broadcastChanges, ClientPacketListener.applyLightData
func TestFrozenFreshArenaReceivesItsSkyLight(t *testing.T) {
	t.Parallel()
	for _, freezeFirst := range []bool{false, true} {
		name := "freeze after fill"
		if freezeFirst {
			name = "load while frozen"
		}
		t.Run(name, func(t *testing.T) { frozenArena(t, freezeFirst) })
	}
}

func frozenArena(t *testing.T, freezeFirst bool) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Builder", servertest.OpsListing(t, "Builder"), func(s *server.Server) {
		s.SetWorldSpawn(world.SpawnPos{X: 160, Y: 64, Z: 160})
	})
	if got := srv.SkyLightAt(0, 200, 0); got != 0 {
		t.Fatal("arena was already loaded before teleporting")
	}
	if freezeFirst {
		c.ChatCommand("tick freeze")
	}
	c.ChatCommand("tp @s 0.5 203 -14.5")
	arrived := make(map[world.ChunkPos]bool)
	c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		if id == protocol.IDPlayChunkData {
			r := protocol.NewReader(data)
			pos := world.ChunkPos{X: r.ReadI32(), Z: r.ReadI32()}
			if pos.X >= -2 && pos.X <= 1 && pos.Z >= -2 && pos.Z <= 1 {
				arrived[pos] = true
			}
		}
		// All eight neighbours of each platform chunk have arrived, so a
		// border-only broadcast cannot accidentally satisfy the assertion.
		return len(arrived) == 16
	})
	if len(arrived) != 16 {
		t.Fatalf("only %d of the arena's sixteen chunks arrived", len(arrived))
	}
	for pos := range arrived {
		if pos.X < -1 || pos.X > 0 || pos.Z < -1 || pos.Z > 0 {
			delete(arrived, pos)
		}
	}
	c.ChatCommand("time set noon")
	c.ChatCommand("weather clear")
	c.ChatCommand("fill -12 199 -12 12 199 12 stone")
	if !freezeFirst {
		c.ChatCommand("tick freeze")
	}
	const section = (200+64)/16 + 1
	lit := make(map[world.ChunkPos]bool)
	see := func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateLight {
			return false
		}
		r := protocol.NewReader(data)
		pos := world.ChunkPos{X: r.ReadVarInt(), Z: r.ReadVarInt()}
		sky := r.ReadBitSet()
		r.ReadBitSet() // block
		r.ReadBitSet() // empty sky
		r.ReadBitSet() // empty block
		n := r.ReadVarInt()
		for i := range 26 {
			if sky&(1<<i) == 0 {
				continue
			}
			n--
			arr := r.ReadBytes(2048)
			if i != section || !arrived[pos] {
				continue
			}
			if len(arr) != 2048 {
				t.Fatal("invalid sky light array")
			}
			for z := max(-12, pos.Z*16); z <= min(12, pos.Z*16+15); z++ {
				for x := max(-12, pos.X*16); x <= min(12, pos.X*16+15); x++ {
					index := (200&15)*256 + (z&15)*16 + (x & 15)
					if got := arr[index/2] >> ((index & 1) * 4) & 15; got != 15 {
						t.Errorf("sky light above platform at (%d, 200, %d) = %d, want 15", x, z, got)
					}
				}
			}
			lit[pos] = true
		}
		if n != 0 || r.Err() != nil {
			t.Fatalf("invalid sky light arrays: count remaining %d, error %v", n, r.Err())
		}
		return false
	}
	// Drain the commands without discarding light packets that beat the barrier,
	// then flush the completed relight on a held loop with the game still frozen.
	c.ReadTicks(srv, 0, see)
	srv.WaitRelights()
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 1, see)
	for pos := range arrived {
		if !lit[pos] {
			t.Errorf("no Update Light with sky section %d for platform chunk %+v", section, pos)
		}
	}
}
