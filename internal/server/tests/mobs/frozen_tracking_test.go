package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ServerLevel.tick, ChunkMap.tick, ServerEntity.sendChanges
func TestFrozenPigSendsMovementBetweenSteps(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine("tick freeze")
	srv.RunLine("fill -4 179 -1 4 179 1 smooth_stone")
	// A small lift separates the tracking test from how level travel sets OnGround.
	srv.RunLine(`summon pig -0.1 180 0.5 {OnGround:1b,Motion:[0.45d,0.004d,0d]}`)
	pig := c.AwaitSpawn(protocol.EntityTypePig)
	baseX := pig.X
	type traffic struct {
		ids      []int32
		x, vx    float64
		ticks    int32
		grounded bool
	}
	read := func(n int) traffic {
		var got traffic
		c.ReadTicks(srv, n, func(id int32, data []byte) bool {
			r := protocol.NewReader(data)
			if r.ReadVarInt() != pig.EntityID {
				return false
			}
			switch id {
			case protocol.IDPlaySetEntityMotion:
				got.vx, _, _ = r.ReadLpVec3()
			case protocol.IDPlayTeleportEntity:
				sync := servertest.ParseEntityPositionSync(data)
				got.x, got.ticks, got.grounded = sync.X, sync.StepTicks, sync.OnGround
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
				flags := r.ReadVarInt()
				got.grounded = flags&1 != 0
				if flags&2 != 0 {
					got.ticks = r.ReadVarInt()
				}
				got.x = (math.Round(baseX*4096) + float64(r.ReadI16())) / 4096
			default:
				return false
			}
			got.ids = append(got.ids, id)
			return false
		})
		return got
	}

	// Pig tracking starts at gate 0 and repeats every three server ticks.
	// Put each game step on a gate so post-physics tracking would send too early.
	read(3)
	for i, want := range []struct {
		x, vx    float64
		grounded bool
		packet   int32
	}{
		{0.35, 0.24570003, false, protocol.IDPlayTeleportEntity},
		{0.59570003, 0.22358703, true, protocol.IDPlayTeleportEntity},
		{0.81928706, 0.12207853, true, protocol.IDPlayEntityPosition},
	} {
		srv.RunLine("tick step 1")
		if early := read(1).ids; len(early) != 0 {
			t.Errorf("step %d: pig movement packets at the stepping tick: %x", i+1, early)
		}
		x, _, _, ok := srv.MobPos(pig.EntityID)
		grounded := srv.MobOnGround(pig.EntityID)
		if !ok || math.Abs(x-want.x) > 1e-7 || grounded != want.grounded {
			t.Fatalf("step %d: pig x = %.8f, grounded = %v, want %.8f and %v", i+1, x, grounded, want.x, want.grounded)
		}
		if early := read(2).ids; len(early) != 0 {
			t.Errorf("step %d: pig movement packets before the next tracking gate: %x", i+1, early)
		}
		got := read(1)
		if len(got.ids) != 2 || got.ids[0] != protocol.IDPlaySetEntityMotion || got.ids[1] != want.packet {
			t.Errorf("step %d: tracking gate sent %x, want motion followed by %x", i+1, got.ids, want.packet)
		} else if math.Abs(got.x-want.x) > 1.0/4096 || math.Abs(got.vx-want.vx) > 0.0002 || got.ticks != 3 || got.grounded != want.grounded {
			t.Errorf("step %d: x=%.8f vx=%.8f ticks=%d grounded=%v, want x=%.8f vx=%.8f ticks=3 grounded=%v", i+1, got.x, got.vx, got.ticks, got.grounded, want.x, want.vx, want.grounded)
		}
		baseX = want.x
		if i < 2 {
			read(2)
		}
	}
}
