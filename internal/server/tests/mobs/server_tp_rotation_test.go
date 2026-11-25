package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: TeleportCommand.performTeleport, PositionMoveRotation.calculateAbsolute
func TestTeleportWrapsRelativeMobRotationAgainstTarget(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, tpGroundConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon chicken 0.5 145 2.5 {NoAI:1b,NoGravity:1b,Rotation:[170f,-80f]}`)
	chicken := c.AwaitSpawn(protocol.EntityTypeChicken)
	if got := savedField(t, srv, "chicken", "Rotation"); got != "[170.0f, -80.0f]" {
		t.Fatalf("summoned chicken rotation = %s, want [170.0f, -80.0f]", got)
	}
	servertest.RunTicks(t, srv, 1)
	c.Settle()

	srv.RunLine(`execute rotated 0 0 run tp @e[type=chicken,limit=1] 0.5 145 3.5 ~-170 ~120`)
	if got := savedField(t, srv, "chicken", "Rotation"); got != "[190.0f, -90.0f]" {
		t.Errorf("teleported chicken rotation = %s, want [190.0f, -90.0f]", got)
	}
	var sync servertest.EntityPositionSync
	if !c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id != protocol.IDPlayTeleportEntity {
			return false
		}
		sync = servertest.ParseEntityPositionSync(data)
		return sync.ID == chicken.EntityID && sync.Z == 3.5
	}) {
		t.Fatal("no Teleport Entity for the chicken at its destination")
	}
	if sync.Yaw != 190 || sync.Pitch != -90 {
		t.Errorf("chicken's packet rotation = [%v, %v], want [190, -90]", sync.Yaw, sync.Pitch)
	}
}
