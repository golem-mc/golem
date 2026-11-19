package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: EntityDataAccessor.setData, Leashable.readLeashData
func TestDataWriteRetainsUUIDLeash(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"cow", "oak_boat"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Herder", servertest.OpsListing(t, "Herder"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)

			srv.RunLine(fmt.Sprintf("summon minecraft:%s 5.5 64 5.5 {NoAI:1b}", kind))
			var id int32
			if kind == "cow" {
				id = c.ExpectMobSpawn(protocol.EntityTypeCow).EntityID
			} else {
				id = servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat).EntityID
			}
			c.Settle()
			giveLead(t, srv, "Herder", 1)
			c.UseOn(id)
			if attached, holder := c.ExpectLinkPacket(); attached != id || holder != c.PlayerEntityID {
				t.Fatalf("tie = (%d, %d), want (%d, %d)", attached, holder, id, c.PlayerEntityID)
			}

			answer := srv.RunLine(fmt.Sprintf("data merge entity @e[type=minecraft:%s,limit=1] {Silent:1b}", kind))
			if len(answer) != 1 || answer[0].Translate != "commands.data.entity.modified" {
				t.Fatalf("data merge = %v, want successful modification", answer)
			}
			for tick := range 2 {
				if tick == 1 {
					servertest.RunTicks(t, srv, 1)
				}
				var holder int32
				var ok bool
				if kind == "cow" {
					holder, ok = srv.MobLeashHolder(id)
				} else {
					holder, ok = srv.BoatLeashHolder(id)
				}
				if !ok || holder != c.PlayerEntityID {
					t.Errorf("holder after write and %d ticks = (%d, %v), want player %d", tick, holder, ok, c.PlayerEntityID)
				}
			}
			if n := srv.ItemCount(); n != 0 {
				t.Errorf("items after write = %d, want none", n)
			}
		})
	}
}
