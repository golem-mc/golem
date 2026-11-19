package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const panda = "minecraft:panda"

func pandaType() int32 { id, _ := registry.EntityTypeID(panda); return int32(id) }

func pandaBabies(srv *server.Server) (babies int) {
	for _, m := range srv.Mobs() {
		if m.Kind == panda && m.Baby {
			babies++
		}
	}
	return babies
}

// mobAttribute reads the attribute value of the first mob of the kind
// through /attribute.
func mobAttribute(t *testing.T, c *servertest.Client, kind, attribute string) float64 {
	t.Helper()
	c.ChatCommand("attribute @e[type=" + kind + ",limit=1] " + attribute + " get")
	var value string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
			value = text.With[len(text.With)-1].Text
		}
	})
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("%s = %q, not a number", attribute, value)
	}
	return v
}

// feedPanda feeds the panda bamboo and reads, in ticks, the hearts that follow.
func feedPanda(t *testing.T, srv *server.Server, c *servertest.Client, id int32) {
	t.Helper()
	c.UseOn(id)
	if !c.ReadTicks(srv, 20, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == id && r.ReadU8() == loveHearts
	}) {
		t.Fatalf("panda %d showed no hearts within 20 ticks", id)
	}
}

// plantPandas joins with the world frozen, holds the loop before anything
// spawns or is fed, and leaves two fed pandas whose bamboo is spent.
func plantPandas(t *testing.T, bamboo bool) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:bamboo", 64,
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)
	if bamboo {
		srv.SetBlock(4, 64, 4, blockNamed(t, "minecraft:bamboo"))
	}
	p1 := srv.SpawnMob(panda, 1.5, 64, 2.5)
	p2 := srv.SpawnMob(panda, 2.5, 64, 2.5)
	feedPanda(t, srv, c, p1)
	feedPanda(t, srv, c, p2)
	if n := srv.InventorySlot("Rancher", player.HotbarSlot).Count; n != 62 {
		t.Fatalf("bamboo left = %d, want 62", n)
	}
	return srv, c
}

func TestTwoFedPandasNextToBambooBreed(t *testing.T) {
	t.Parallel()
	srv, c := plantPandas(t, true)
	if !servertest.ASpawnOfWithinTicks(c, srv, 300, pandaType()) {
		t.Fatal("no panda cub spawned within 300 ticks")
	}
	if n := pandaBabies(srv); n != 1 {
		t.Fatalf("%d cubs, want 1", n)
	}
}
