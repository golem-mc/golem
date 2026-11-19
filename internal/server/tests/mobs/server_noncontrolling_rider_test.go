package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestAPigCarryingASlimeFollowsACarrot(t *testing.T) {
	t.Parallel()
	pigCarryingCubeFollowsCarrot(t, "slime")
}

func TestAPigCarryingAMagmaOrSulfurCubeFollowsACarrot(t *testing.T) {
	t.Parallel()
	for _, rider := range []string{"magma_cube", "sulfur_cube"} {
		t.Run(rider, func(t *testing.T) {
			t.Parallel()
			pigCarryingCubeFollowsCarrot(t, rider)
		})
	}
}

func pigCarryingCubeFollowsCarrot(t *testing.T, rider string) {
	t.Helper()
	const name = "Herder"
	srv, c := servertest.BreedingPlayer(t, "survival", name, "minecraft:carrot", 1, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition(name)
	c.ChatCommand(fmt.Sprintf(`summon minecraft:pig ~6 ~ ~ {Passengers:[{id:"minecraft:%s",Size:0,NoAI:1b}]}`, rider))
	pig := c.ExpectMobSpawn(protocol.EntityTypePig).EntityID
	c.Settle()
	riders := srv.MobIDsOfKind("minecraft:" + rider)
	if len(riders) != 1 || srv.MobVehicleID(riders[0]) != pig {
		t.Fatalf("%s is not riding the pig: riders %v", rider, riders)
	}
	servertest.UntilTicks(t, srv, 200, "the pig carrying a "+rider+" comes within 3 blocks of the carrot", func() bool {
		x, y, z, ok := srv.MobPos(pig)
		return ok && math.Sqrt((x-pos.X)*(x-pos.X)+(y-pos.Y)*(y-pos.Y)+(z-pos.Z)*(z-pos.Z)) < 3
	})
	if srv.MobVehicleID(riders[0]) != pig {
		t.Fatalf("%s left the pig while it followed the carrot", rider)
	}
}
