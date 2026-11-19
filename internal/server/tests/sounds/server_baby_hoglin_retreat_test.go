package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestABabyHoglinRetreatsAtTheBabyPitch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Listener", "", 0,
		func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:hoglin 3.5 64 0.5 {IsImmuneToZombification:1b}")
	var id int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:hoglin" {
			id = m.ID
		}
	}
	if id == 0 {
		t.Fatal("hoglin was not summoned")
	}
	srv.SetMobBaby(id, true)
	c.Settle()
	c.Attack(id)
	heard := false
	for tick := 0; tick < 20 && !heard; tick++ {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlaySoundEffect {
				return
			}
			name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data)
			if name != "minecraft:entity.hoglin.retreat" {
				return
			}
			heard = true
			if pitch < 1.29 || pitch > 1.71 {
				t.Errorf("baby hoglin retreat pitch = %v, want [1.29,1.71]", pitch)
			}
		})
	}
	if !heard {
		t.Fatal("no retreat sound heard")
	}
}
