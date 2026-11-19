package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// pigShoveTicks is how long a team collision test lets bodies push.
const pigShoveTicks = 20

// shoveWorld is a held world with a player standing at (2.5, 64, 5.5).
type shoveWorld struct {
	t   *testing.T
	srv *server.Server
	c   *servertest.Client
	ids []int32
}

func newShoveWorld(t *testing.T, name string) *shoveWorld {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return &shoveWorld{t: t, srv: srv, c: c}
}

// neverTeam makes team t with collisionRule never.
func (w *shoveWorld) neverTeam() {
	w.srv.RunLine("team add t")
	w.srv.RunLine("team modify t collisionRule never")
}

func (w *shoveWorld) pig(x, z float64) {
	w.srv.RunLine(fmt.Sprintf("summon minecraft:pig %g 64 %g {attributes:[{id:\"minecraft:movement_speed\",base:0d}]}", x, z))
	w.c.ExpectMobSpawn(protocol.EntityTypePig)
	w.ids = w.srv.MobIDsOfKind("minecraft:pig")
}

func (w *shoveWorld) run() { servertest.RunTicks(w.t, w.srv, pigShoveTicks) }

// moved says whether pig i is off (x, z).
func (w *shoveWorld) moved(i int, x, z float64) bool {
	px, _, pz, _ := w.srv.MobPos(w.ids[i])
	return math.Abs(px-x) > 0.01 || math.Abs(pz-z) > 0.01
}

// TestAMobIsNotPushedByAPlayerWhoseTeamNeverCollides stands a pig a fifth of
// a block from a player: it is shoved away, but not once the player's team
// has collisionRule never.
// vanilla: EntitySelector.pushableBy
func TestAMobIsNotPushedByAPlayerWhoseTeamNeverCollides(t *testing.T) {
	t.Parallel()
	for _, never := range []bool{false, true} {
		w := newShoveWorld(t, "Shover")
		w.pig(2.7, 5.5)
		if never {
			w.neverTeam()
			w.srv.RunLine("team join t Shover")
		}
		w.run()
		if moved := w.moved(0, 2.7, 5.5); moved == never {
			t.Errorf("never=%v: pig moved = %v after %d ticks", never, moved, pigShoveTicks)
		}
	}
}
