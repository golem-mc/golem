package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: AbstractCubeMob.defineSynchedData, AbstractCubeMob.readAdditionalSaveData
func TestExpectMobSpawnCubeSize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind int32
		size int
	}{
		{"sulfur_cube", protocol.EntityTypeSulfurCube, 1},
		{"sulfur_cube", protocol.EntityTypeSulfurCube, 0},
		{"slime", protocol.EntityTypeSlime, 3},
		{"magma_cube", protocol.EntityTypeMagmaCube, 3},
	} {
		t.Run(fmt.Sprintf("%s/Size=%d", tc.name, tc.size), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 4.5 64 7.5 {Age:0,Size:%d}`, tc.name, tc.size))
			spawn := c.ExpectMobSpawn(tc.kind)
			if want := int32(tc.size + 1); spawn.HasCubeSize != (want != 1) || spawn.HasCubeSize && spawn.CubeSize != want {
				t.Fatalf("cube size = %d (present %v), want %d (present %v)", spawn.CubeSize, spawn.HasCubeSize, want, want != 1)
			}
		})
	}
}
