package explosions

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestSameTickTNTBlastsPushSurvivorsInOrder checks that when multiple primed
// TNT explode on the same tick (building the blast spatial index), surviving
// TNT across a cell boundary receive knockback from every blast in list order.
// vanilla: ServerExplosion.hurtEntities
func TestSameTickTNTBlastsPushSurvivorsInOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)

	// Place survivors in reverse cell order across the x=8 cell boundary
	// (surv1 in cell 1, surv2 in cell 0) so cell order differs from list order.
	surv1 := srv.SpawnTNT(9.5, 64, 0.5, 80)
	surv2 := srv.SpawnTNT(6.5, 64, 0.5, 80)
	exp1 := srv.SpawnTNT(8.0, 64, 0.5, 1)
	exp2 := srv.SpawnTNT(8.0, 64, 1.5, 1)
	for _, id := range []int32{surv1, surv2, exp1, exp2} {
		if id == 0 {
			t.Fatal("failed to spawn primed TNT")
		}
		c.AwaitSpawn(protocol.EntityTypeTnt)
	}

	type motion struct {
		id         int32
		vx, vy, vz float64
	}
	var got []motion
	explosions := 0
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			eid := r.ReadVarInt()
			vx, vy, vz := r.ReadLpVec3()
			if err := r.Err(); err != nil {
				t.Fatalf("set entity motion: %v", err)
			}
			if eid == surv1 || eid == surv2 {
				got = append(got, motion{id: eid, vx: vx, vy: vy, vz: vz})
			}
		case protocol.IDPlayExplosion:
			explosions++
		}
		return false
	})

	if explosions != 2 {
		t.Fatalf("explosions = %d, want 2", explosions)
	}

	var ids []int32
	for _, m := range got {
		ids = append(ids, m.id)
	}
	wantIDs := []int32{surv1, surv2, surv1, surv2}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("motion order = %v, want %v (motions=%+v)", ids, wantIDs, got)
	}

	// Blast 1 at (8.0, 64, 0.5) pushes surv1 (+X) and surv2 (-X) symmetrically.
	if got[0].vx <= 0 || got[1].vx >= 0 {
		t.Fatalf("blast 1 horizontal push = (%v, %v), want (+, -)", got[0].vx, got[1].vx)
	}
	if math.Abs(got[0].vx+got[1].vx) > 0.01 {
		t.Fatalf("blast 1 push asymmetric: surv1=%+v surv2=%+v", got[0], got[1])
	}
	// Blast 2 at (8.0, 64, 1.5) adds further +/-X and -Z knockback to both.
	if got[2].vx <= got[0].vx || got[2].vz >= 0 {
		t.Fatalf("blast 2 surv1 motion = %+v, want vx > %v and vz < 0", got[2], got[0].vx)
	}
	if got[3].vx >= got[1].vx || got[3].vz >= 0 {
		t.Fatalf("blast 2 surv2 motion = %+v, want vx < %v and vz < 0", got[3], got[1].vx)
	}

	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 2 {
		t.Fatalf("surviving TNT = %d, want 2", n)
	}
}
