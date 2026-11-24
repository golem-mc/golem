package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestNoAIShulkerReattachesButDoesNotTeleport(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
	// Face 4 is west and nothing is there; the floor can hold it.
	srv.RunLine(`summon shulker 0.5 100 0.5 {AttachFace:4b,NoAI:1b}`)
	id := srv.MobIDsOfKind("minecraft:shulker")[0]
	servertest.RunTicks(t, srv, 2)
	if face, _, _ := srv.ShulkerState(id); face != 0 {
		t.Errorf("face = %d, want down (0)", face)
	}

	srv.RunLine(`summon shulker 0.5 130 0.5 {AttachFace:4b,NoAI:1b}`)
	var far int32
	for _, o := range srv.MobIDsOfKind("minecraft:shulker") {
		if o != id {
			far = o
		}
	}
	servertest.RunTicks(t, srv, 5)
	if _, y, _, _ := srv.MobPos(far); y != 130 {
		t.Errorf("unsupported NoAI shulker moved to y=%v", y)
	}
}
