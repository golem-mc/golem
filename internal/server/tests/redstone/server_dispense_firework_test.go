package redstone

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FireworkRocketItem.getEntityJustOutsideOfBlockPos, FireworkRocketEntity.tick
func TestNorthFacingDispenserLaunchesFireworkAtFaceHeight(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	const x, y, z = 4, 110, 2
	if !srv.PlaceContainer(x, y, z, "minecraft:dispenser") {
		t.Fatal("could not place the dispenser")
	}
	srv.SetBlock(x, y, z, servertest.MustState(t, "minecraft:dispenser", map[string]string{"facing": "north"}))
	if !srv.SetInventoryAt(x, y, z, 0, servertest.FireworkStack(1, 0)) {
		t.Fatal("could not load the dispenser")
	}
	srv.SetLevelSeed(2)
	srv.SetBlockAndTell(x, y, z+1, servertest.BlockState(t, "minecraft:redstone_block"))

	var rocket servertest.ThrownSpawn
	var sound [3]int32
	var heard bool
	c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if spawn := servertest.ReadThrownSpawn(c, data); spawn.EntityType == protocol.EntityTypeFireworkRocket {
				rocket = spawn
			}
		case protocol.IDPlaySoundEffect:
			if name, _, sx, sy, sz, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.firework_rocket.launch" {
				sound, heard = [3]int32{sx, sy, sz}, true
			}
		}
		return false
	})
	if rocket.EntityID == 0 || rocket.Y != y+0.5 || rocket.Vy >= 0 || rocket.Vy < -0.009 {
		t.Fatalf("spawn = %+v, want a rocket at y=%v with slightly negative vertical motion", rocket, y+0.5)
	}
	bodies := srv.SelectedEntities("minecraft:firework_rocket")
	if len(bodies) != 1 {
		t.Fatalf("got %d rockets after launch, want one", len(bodies))
	}
	pos := bodies[0].Position()
	// LpVec3 quantizes the spawn velocity before the client sees it.
	if wantY := rocket.Y + rocket.Vy; math.Abs(pos.Y-wantY) > 0.0002 {
		t.Errorf("first-tick rocket y = %.6f, want %.6f", pos.Y, wantY)
	}
	if wantZ := rocket.Z + rocket.Vz; math.Abs(pos.Z-wantZ) > 0.0002 {
		t.Errorf("first-tick rocket z = %.6f, want %.6f", pos.Z, wantZ)
	}
	// Vanilla plays the sound after moving, with the packet's coordinates in eighths.
	wantSound := [3]int32{int32((rocket.X + rocket.Vx) * 8), 883, int32((rocket.Z + rocket.Vz) * 8)}
	if !heard || sound != wantSound {
		t.Errorf("launch sound position in eighths = %v (heard %v), want %v", sound, heard, wantSound)
	}
}
