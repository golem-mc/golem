package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: SummonCommand.createEntity, Entity.positionRider
func TestSummonedMobPassengerSpawnsAtSummonPosition(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.ChatCommand(`summon minecraft:pig 0.3 200 0.5 {NoAI:1b,NoGravity:1b,Passengers:[{id:"minecraft:cow"}]}`)
	var cowY float64
	var seen bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySpawnEntity {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		typ := r.ReadVarInt()
		r.ReadF64()
		y := r.ReadF64()
		if typ == protocol.EntityTypeCow {
			cowY, seen = y, true
		}
	})
	if !seen {
		t.Fatal("no cow spawn")
	}
	if cowY != 200 {
		t.Errorf("cow spawned at Y=%g, want the summon height 200", cowY)
	}
}
