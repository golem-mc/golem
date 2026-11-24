package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Shulker.tick, canStayAt, teleportSomewhere
func TestNoAIShulkerReattachesButDoesNotTeleport(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
	// Face 4 is west and nothing is there; the floor can hold it.
	srv.RunLine(`summon shulker 0.5 100 0.5 {Tags:[floor],AttachFace:4b,NoAI:1b}`)
	// Lily pad above does not fill its face; the stone to the east does.
	srv.RunLine(`setblock 7 99 5 minecraft:air`)
	srv.RunLine(`setblock 7 101 5 minecraft:lily_pad`)
	srv.RunLine(`setblock 8 100 5 minecraft:stone`)
	srv.RunLine(`summon shulker 7.5 100 5.5 {Tags:[lily],AttachFace:4b,NoAI:1b}`)
	// Stone above holds it, but its shell would open down into the scaffolding
	// at the shulker's own height; the stone to the east is the face.
	srv.RunLine(`setblock -7 98 5 minecraft:stone`)
	srv.RunLine(`setblock -7 99 5 minecraft:scaffolding`)
	srv.RunLine(`setblock -7 101 5 minecraft:stone`)
	srv.RunLine(`setblock -6 100 5 minecraft:stone`)
	srv.RunLine(`summon shulker -6.5 100 5.5 {AttachFace:4b,NoAI:1b}`)
	ids := srv.MobIDsOfKind("minecraft:shulker")
	floor, lily, scaffold := ids[0], ids[1], ids[2]
	servertest.RunTicks(t, srv, 1)
	if face, _, _ := srv.ShulkerState(floor); face != 0 {
		t.Errorf("floor face = %d, want down (0)", face)
	}
	if face, _, _ := srv.ShulkerState(lily); face != 5 {
		t.Errorf("lily-pad face = %d, want east (5)", face)
	}

	if face, _, _ := srv.ShulkerState(scaffold); face != 5 {
		t.Errorf("scaffold face = %d, want east (5)", face)
	}

	// Floor y=99 supports only cells at y=100; a shulker at y=101 teleports
	// within a few ticks when it has AI and never when NoAI.
	srv.RunLine(`summon shulker -4.5 101 -4.5 {Tags:[ai],AttachFace:1b}`)
	srv.RunLine(`summon shulker 4.5 101 -4.5 {Tags:[still],AttachFace:1b,NoAI:1b}`)
	var ai, still int32
	for _, o := range srv.MobIDsOfKind("minecraft:shulker") {
		if o != floor && o != lily && o != scaffold {
			if x, _, _, _ := srv.MobPos(o); x < 0 {
				ai = o
			} else {
				still = o
			}
		}
	}
	servertest.UntilTicks(t, srv, 400, "AI shulker teleports", func() bool {
		x, y, z, _ := srv.MobPos(ai)
		return x != -4.5 || y != 101 || z != -4.5
	})
	x, y, z, _ := srv.MobPos(still)
	if x != 4.5 || y != 101 || z != -4.5 {
		t.Errorf("NoAI shulker moved to %v,%v,%v", x, y, z)
	}
}
