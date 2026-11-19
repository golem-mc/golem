package commands

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestCommandDataOnAnArrowOrACartIsToldOnTheNextTick merges metadata onto a
// held-up arrow and a held-up cart, with a new Pos: the write itself tells a
// watcher nothing, and the next tick's tracker sends tell it the metadata
// once and the new position, off the movement schedule.
// vanilla: Entity.load, ServerEntity.sendChanges
func TestCommandDataOnAnArrowOrACartIsToldOnTheNextTick(t *testing.T) {
	t.Parallel()
	minecartType, ok := registry.EntityTypeID("minecraft:minecart")
	if !ok {
		t.Fatal("no entity type minecraft:minecart")
	}
	kinds := []struct {
		name, summon, merge string
		entityType          int32
		indices             []uint8
	}{
		{"arrow", `summon minecraft:arrow 0.5 70 6.5 {NoGravity:1b,Tags:["held"]}`,
			`data merge entity @e[tag=held,limit=1] {crit:1b,NoGravity:0b,Pos:[0.5d,71.0d,6.5d]}`,
			protocol.EntityTypeArrow, []uint8{protocol.MetadataIndexNoGravity, protocol.MetadataIndexArrowFlags}},
		{"minecart", `summon minecraft:minecart 3.5 70 4.5 {NoGravity:1b,Tags:["held"]}`,
			`data merge entity @e[tag=held,limit=1] {NoGravity:0b,DisplayOffset:9,Pos:[3.5d,71.0d,4.5d]}`,
			minecartType, []uint8{protocol.MetadataIndexNoGravity, protocol.MetadataIndexMinecartDisplayOffset}},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			srv, c := servertest.Join(t, cfg, "Marker", servertest.OpsListing(t, "Marker"))
			c.ChatCommand("gamerule spawn_mobs false")
			c.Settle()
			servertest.HoldTicks(t, srv)
			r := servertest.NewMetadataRecorder(t, srv, c)

			id := servertest.SpawnedID(r.Run(k.summon), k.entityType)
			if id == 0 {
				t.Fatal("summoning the body sent no spawn")
			}
			r.Read(2)

			c.ChatCommand(k.merge)
			// No tick has run since the command: nothing yet.
			r.Record("at once", id, r.Read(0))
			if got := r.Got["at once"]; len(got) != 0 {
				t.Fatalf("the write told the watcher %v before the next tick, want nothing", got)
			}
			next := r.Read(1)
			if n := positionPackets(next, id); n != 1 {
				t.Errorf("the next tick sent %d position packets for the written Pos, want one", n)
			}
			r.Record("tick", id, next)
			got := r.Got["tick"]
			if len(got) != 1 {
				t.Fatalf("the next tick told %d metadata packets (%v), want one", len(got), got)
			}
			if indices := servertest.MetadataIndices(t, got[0]); !slices.Equal(indices, k.indices) {
				t.Errorf("the next tick told metadata fields %v, want %v", indices, k.indices)
			}
			r.Record("later", id, r.Read(3))
			if got := r.Got["later"]; len(got) != 0 {
				t.Errorf("the ticks after told %v again, want nothing", got)
			}
		})
	}
}

// positionPackets counts the packets in packets that move entity id.
func positionPackets(packets []servertest.PushedPacket, id int32) int {
	n := 0
	for _, p := range packets {
		switch p.Id {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
			if protocol.NewReader(p.Data).ReadVarInt() == id {
				n++
			}
		}
	}
	return n
}
