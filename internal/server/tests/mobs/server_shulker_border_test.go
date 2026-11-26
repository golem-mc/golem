package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A face whose opened shell would cross the world border is refused.
// vanilla: Shulker.canStayAt, CollisionGetter.noBorderCollision
func TestNoAIShulkerRefusesFaceOpeningAcrossBorder(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`worldborder center 0 0`)
	srv.RunLine(`worldborder set 19`)
	// West face holds both; the shell opens east, over x 9..11 for the first; the border centre is a block centre, so size 19 puts the edge at x=10.
	srv.RunLine(`setblock 8 100 0 minecraft:stone`)
	srv.RunLine(`setblock -2 100 0 minecraft:stone`)
	srv.RunLine(`summon shulker 9.5 100 0.5 {Tags:[edge],AttachFace:1b,NoAI:1b}`)
	srv.RunLine(`summon shulker -0.5 100 0.5 {Tags:[inside],AttachFace:1b,NoAI:1b}`)
	var edge, inside int32
	for _, id := range srv.MobIDsOfKind("minecraft:shulker") {
		if x, _, _, _ := srv.MobPos(id); x > 0 {
			edge = id
		} else {
			inside = id
		}
	}
	servertest.RunTicks(t, srv, 1)
	if face, _, _ := srv.ShulkerState(inside); face != 4 {
		t.Errorf("inside face = %d, want west (4)", face)
	}
	if face, _, _ := srv.ShulkerState(edge); face != 1 {
		t.Errorf("edge face = %d, want it kept up (1): the shell would cross the border", face)
	}
}
