package network

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAKeepAliveFollowsTheTicksEntityUpdates checks that the keep-alive
// exchange runs after the entity pass, so a ping is stamped just before the
// flush that sends it and a slow entity pass does not count toward the
// client's latency: in a tick that both pings and moves a falling chicken,
// the move comes first.
// vanilla: MinecraftServer.tickChildren (tickConnection after the levels)
func TestAKeepAliveFollowsTheTicksEntityUpdates(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Pinger")
	servertest.HoldKeepAlive(t, srv, servertest.KeepAliveWaitTicks)
	srv.RunLine("summon minecraft:chicken 2.5 90 2.5")

	both := 0
	for tick := 0; tick < 30 && both < 3; tick++ {
		servertest.RunTicks(t, srv, 1)
		var order []int32
		var ping []byte
		settleUnanswered(c, func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayKeepAlive:
				ping = data
				order = append(order, id)
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlaySetEntityMotion:
				order = append(order, id)
			}
		})
		if ping == nil {
			continue
		}
		c.AnswerKeepAlive(ping)
		if len(order) < 2 {
			continue
		}
		both++
		if order[len(order)-1] != protocol.IDPlayKeepAlive {
			t.Fatalf("tick %d sent % X: the keep-alive came before the entity updates", tick, order)
		}
	}
	if both == 0 {
		t.Fatal("no tick both pinged and moved the chicken")
	}
}
