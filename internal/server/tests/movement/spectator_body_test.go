package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// bodyTraffic is what a settle saw of one player's body: the spawns and the
// removals that named its entity.
type bodyTraffic struct{ spawns, removals int }

// settleBodies settles c and counts what it was sent of entity id.
func settleBodies(c *servertest.Client, id int32) bodyTraffic {
	c.T.Helper()
	var seen bodyTraffic
	c.SettleAndCollect(func(packetID int32, data []byte) {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			if r.ReadVarInt() == id {
				seen.spawns++
			}
		case protocol.IDPlayRemoveEntities:
			n := r.ReadVarInt()
			for range n {
				if r.ReadVarInt() == id {
					seen.removals++
				}
			}
		}
	})
	return seen
}

// switchMode has the operator set a player's mode and runs the tracking pass
// by hand, the loop being stopped.
func switchMode(t *testing.T, srv *server.Server, op *servertest.Client, name, mode string, want int32) {
	t.Helper()
	op.ChatCommand("gamemode " + mode + " " + name)
	op.SettleKeeping()
	if srv.PlayerGameMode(name) != want {
		t.Fatal("the game mode switch applies")
	}
	srv.RunTick()
}

// TestASpectatorsBodyLeavesSurvivalViewers: a survival player's client
// drops the body of a player who becomes a spectator, the tab list entry
// staying, and is shown it again once they leave spectator mode.
func TestASpectatorsBodyLeavesSurvivalViewers(t *testing.T) {
	t.Parallel()
	srv, op, guest, guestID := servertest.StartExecuteWorldForTwo(t, "Operator", "Guest")
	srv.StopTick()
	guest.Settle()

	switchMode(t, srv, op, "Guest", "spectator", protocol.GameModeSpectator)
	if got := settleBodies(op, guestID); got.removals != 1 || got.spawns != 0 {
		t.Errorf("after the switch to spectator the viewer saw %+v, want one removal", got)
	}

	switchMode(t, srv, op, "Guest", "survival", protocol.GameModeSurvival)
	if got := settleBodies(op, guestID); got.spawns != 1 || got.removals != 0 {
		t.Errorf("after the switch back the viewer saw %+v, want one spawn", got)
	}
}

// TestASpectatorViewerSeesSurvivalBodies: a spectator keeps the bodies of
// the players who are not spectators.
func TestASpectatorViewerSeesSurvivalBodies(t *testing.T) {
	t.Parallel()
	srv, op, guest, guestID := servertest.StartExecuteWorldForTwo(t, "Operator", "Guest")
	srv.StopTick()
	guest.Settle()

	switchMode(t, srv, op, "Operator", "spectator", protocol.GameModeSpectator)
	if got := settleBodies(op, guestID); got != (bodyTraffic{}) {
		t.Errorf("the spectator viewer saw %+v of a survival body, want nothing", got)
	}
}

// TestASpectatorsBodyIsNotSpawnedForALateJoiner: a player who joins after
// another became a spectator gets the profile but no body.
func TestASpectatorsBodyIsNotSpawnedForALateJoiner(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Operator"))
	op := connect().Join("Operator")
	spec := connect().Join("Spec")
	specID := spec.PlayerEntityID
	op.ExpectPlayerAdded("Spec")
	srv.StopTick()
	switchMode(t, srv, op, "Spec", "spectator", protocol.GameModeSpectator)

	late := connect()
	late.Login("Late")
	late.Configure()
	spawns := 0
	late.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == specID {
			spawns++
		}
	})
	late.MarkLoaded()
	if spawns != 0 {
		t.Errorf("the late joiner was sent the spectator's body %d times, want 0", spawns)
	}
	srv.RunTick()
	if got := settleBodies(late, specID); got.spawns != 0 {
		t.Errorf("a tracking pass sent the late joiner the spectator's body %d times, want 0", got.spawns)
	}
}
