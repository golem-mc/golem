package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// bodyEvents reads n ticks of w's traffic and reports whether the death poof
// for entity id, its removal and a spawn for it arrived.
func bodyEvents(w *servertest.Client, srv *server.Server, id int32, n int) (poof, removed, spawned bool) {
	w.T.Helper()
	w.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch pid {
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusDeathPoof {
				poof = true
			}
		case protocol.IDPlayRemoveEntities:
			for k := r.ReadVarInt(); k > 0; k-- {
				if r.ReadVarInt() == id {
					removed = true
				}
			}
		case protocol.IDPlaySpawnEntity:
			if r.ReadVarInt() == id {
				spawned = true
			}
		}
		return false
	})
	return
}

// TestADeadPlayersBodyLeavesTheirWatchersAfterTwentyTicks kills one player
// and checks the other still sees the body lying for nineteen ticks, then the
// death poof and the removal on the twentieth, and the body again once the
// dead player respawns.
// vanilla: LivingEntity.tickDeath
func TestADeadPlayersBodyLeavesTheirWatchersAfterTwentyTicks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Victim"))
	victim := connect()
	id := victim.JoinAs("Victim")
	watcher := connect().Join("Watcher")
	watcher.Settle()
	victim.Settle()
	servertest.HoldTicks(t, srv)

	victim.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, victim, id, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Victim")},
	})
	if poof, removed, _ := bodyEvents(watcher, srv, id, 18); poof || removed {
		t.Fatalf("body gone before its twentieth dead tick (poof %v, removed %v)", poof, removed)
	}
	if poof, removed, _ := bodyEvents(watcher, srv, id, 4); !poof || !removed {
		t.Fatalf("after 22 dead ticks: poof %v, removed %v, want both", poof, removed)
	}

	victim.PerformRespawn()
	victim.ExpectRespawnThroughTeleport()
	// The teleport goes out before the new body is broadcast to the
	// watchers; the settle answers once the whole respawn has run.
	victim.Settle()
	if _, _, spawned := bodyEvents(watcher, srv, id, 3); !spawned {
		t.Error("the respawned body was not shown to the watcher again")
	}
}

func TestCorpseExpiryKeepsRespawnedBody(t *testing.T) {
	t.Parallel()
	srv, watcher, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Watcher", servertest.OpsListing(t, "Victim"))
	victim := servertest.DialServer(t, srv)
	id := victim.JoinAs("Victim")
	if got := watcher.ExpectPlayerAdded("Victim"); got != id {
		t.Fatalf("watcher saw entity %d, want victim %d", got, id)
	}
	victim.Settle()
	servertest.HoldTicks(t, srv)

	victim.ChatCommand("kill")
	victim.Settle()
	servertest.RunTicks(t, srv, 19)
	watcher.Settle()
	expired := false
	srv.CorpseExpiryBetween(func() {
		expired = true
		victim.PerformRespawn()
		victim.ExpectRespawnThroughTeleport()
		victim.Settle()
		if _, removed, spawned := bodyEvents(watcher, srv, id, 0); !removed || !spawned {
			t.Fatalf("respawn while the tick was paused: removal %v, spawn %v, want both", removed, spawned)
		}
	})
	srv.RunTick()
	if !expired {
		t.Fatal("the twentieth death tick did not reach corpse expiry")
	}
	if poof, removed, _ := bodyEvents(watcher, srv, id, 1); poof || removed {
		t.Errorf("expired tick affected the live body: poof %v, removal %v", poof, removed)
	}
	if !srv.PlayerHoldsChunks("Victim") {
		t.Error("expired tick dropped the respawned player's chunk holds")
	}
}
