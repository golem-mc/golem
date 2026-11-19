package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnEndermanTeleportSendsItsParticleEvent: a hurt with no living
// attacker teleports the enderman, and level event 2018 at the cell it left
// packs the clamped offset to the cell it reached; a silent one sends none.
// vanilla: EnderMan.teleport, EnderMan.hurtServer
func TestAnEndermanTeleportSendsItsParticleEvent(t *testing.T) {
	t.Parallel()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	at := srv.PlayerPosition("Watcher")
	x, z := math.Floor(at.X)+4.5, math.Floor(at.Z)+0.5

	trails := func(silent bool, seed int64) (from, to [3]int32, got [][4]int32) {
		id := srv.SpawnMobSeeded("minecraft:enderman", x, at.Y, z, seed)
		srv.SetMobNoAI(id)
		srv.SetMobSilent(id, silent)
		// One tick hands the enderman its world, which a hurt teleport needs.
		srv.TickMobs(1)
		c.Settle()
		x0, y0, z0, _ := srv.MobPos(id)
		for range 20 {
			srv.HurtMobByWorld(id, 1, "minecraft:generic")
			if mx, my, mz, _ := srv.MobPos(id); mx != x0 || my != y0 || mz != z0 {
				from = [3]int32{int32(math.Floor(x0)), int32(math.Floor(y0)), int32(math.Floor(z0))}
				to = [3]int32{int32(math.Floor(mx)), int32(math.Floor(my)), int32(math.Floor(mz))}
				break
			}
		}
		if from == to {
			t.Fatal("twenty hurts never teleported the enderman")
		}
		// The hurt's teleport is told with the next tick.
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlayWorldEvent {
				return
			}
			if event, ex, ey, ez, d := servertest.ReadWorldEvent(t, data); event == protocol.WorldEventEndermanTeleport {
				got = append(got, [4]int32{ex, ey, ez, d})
			}
		})
		return from, to, got
	}

	from, to, got := trails(false, 1)
	if len(got) != 1 {
		t.Fatalf("got %d enderman teleport events, want 1", len(got))
	}
	if [3]int32{got[0][0], got[0][1], got[0][2]} != from {
		t.Errorf("the event is at %v, want the cell left %v", got[0][:3], from)
	}
	if dest := servertest.UnpackPosDiff(from[0], from[1], from[2], got[0][3], 127, 127, 127); dest != to {
		t.Errorf("the event points at %v, want the cell reached %v", dest, to)
	}

	if _, _, got := trails(true, 2); len(got) != 0 {
		t.Errorf("a silent enderman sent %d teleport events, want none", len(got))
	}
}
