package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A /tp sets the ground flag, so a client sees a cube mob land on the target.
// vanilla: TeleportCommand.performTeleport
func TestTeleportCommandSetsMobOnGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon magma_cube 0.5 100 0.5 {Tags:[c],NoAI:1b,Size:1}`)
	id := srv.MobIDsOfKind("minecraft:magma_cube")[0]
	servertest.RunTicks(t, srv, 5)
	if srv.MobOnGround(id) {
		t.Fatal("a NoAI cube in the air starts on the ground")
	}
	c.Settle()

	srv.RunLine(`tp @e[tag=c,limit=1] 0.5 105 0.5`)
	servertest.RunTicks(t, srv, 5)
	told, ground := false, false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayTeleportEntity {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		r.ReadVarInt()
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		r.ReadF32()
		r.ReadF32()
		told, ground = true, r.ReadBool()
	})
	if !told {
		t.Fatal("no Teleport Entity for the teleported cube")
	}
	if !ground || !srv.MobOnGround(id) {
		t.Errorf("after /tp: packet on-ground %v, mob on ground %v, want both true", ground, srv.MobOnGround(id))
	}
}
