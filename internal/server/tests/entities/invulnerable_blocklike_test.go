package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// expectBlocklikeStand checks that the cushion, the painting and the knot
// all still stand and none of them laid its item.
func expectBlocklikeStand(t *testing.T, srv *server.Server, what string) {
	t.Helper()
	if f, p, k := srv.FixtureCount(), srv.PaintingCount(), srv.KnotCount(); f != 1 || p != 1 || k != 1 {
		t.Errorf("%d cushions, %d paintings and %d knots stand after %s, want one each", f, p, k, what)
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		// The blast's broken blocks drop theirs.
		switch item := registry.Item(it.Stack.ItemID); {
		case item == registry.ItemPainting, item == registry.ItemLead,
			item >= registry.ItemWhiteCushion && item <= registry.ItemBlackCushion:
			t.Errorf("%s laid %s", what, it.Item)
		}
	}
}

// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.push
func TestAnExplosionPushBreaksInvulnerableBlocklikeEntities(t *testing.T) {
	t.Parallel()
	for _, shielded := range []bool{false, true} {
		name := "exposed"
		if shielded {
			name = "shielded"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
			servertest.HoldTicks(t, srv)
			obsidian := registry.BlockObsidian.DefaultState()
			// Keep the support intact so only the blast's push can break a body.
			for x := int32(0); x <= 7; x++ {
				for z := int32(0); z <= 6; z++ {
					srv.SetBlock(x, 63, z, obsidian)
				}
			}
			srv.SetBlock(1, 64, 5, obsidian)
			srv.SetBlock(5, 64, 4, registry.BlockOakFence.DefaultState())
			if shielded {
				for x := int32(0); x <= 7; x++ {
					for y := int32(64); y <= 67; y++ {
						srv.SetBlock(x, y, 3, obsidian)
					}
				}
			}
			c.MoveToGrounded(3.5, 64, 0.5, 0, 0, true)
			c.Settle()
			c.ChatCommand(`summon minecraft:painting 1.5 64 4.5 {facing:2b,variant:"minecraft:kebab",Invulnerable:1b}`)
			painting := c.AwaitSpawn(protocol.EntityTypePainting)
			c.ChatCommand(`summon minecraft:cushion 3.5 64 4.5 {Invulnerable:1b}`)
			cushion := c.AwaitSpawn(cushionType)
			c.ChatCommand(`summon minecraft:leash_knot 5 64 4 {Invulnerable:1b}`)
			knot := c.AwaitSpawn(protocol.EntityTypeLeashKnot)
			// A fence encloses the knot's exposure samples, so expose it without
			// waiting for the 100-tick support poll.
			if !shielded {
				srv.SetBlock(5, 64, 4, registry.BlockAir.DefaultState())
			}
			c.ChatCommand(`summon minecraft:tnt 3.5 65.5 1.5 {fuse:0}`)
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			if shielded {
				expectBlocklikeStand(t, srv, "the occluded blast")
				return
			}
			if f, p, k := srv.FixtureCount(), srv.PaintingCount(), srv.KnotCount(); f != 0 || p != 0 || k != 0 {
				t.Fatalf("%d cushions, %d paintings and %d knots survived the exposed blast, want none", f, p, k)
			}
			servertest.AwaitRemovalsOfTicks(t, c, srv, 0, painting.EntityID, cushion.EntityID, knot.EntityID)
			counts := make(map[registry.Item]int)
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				counts[registry.Item(it.Stack.ItemID)] += int(it.Stack.Count)
			}
			for _, item := range []registry.Item{registry.ItemPainting, registry.ItemWhiteCushion} {
				if counts[item] != 1 {
					t.Errorf("blast dropped %d of item %v, want 1", counts[item], item)
				}
			}
			if counts[registry.ItemLead] != 0 {
				t.Errorf("blast dropped %d leads, want none from a knot", counts[registry.ItemLead])
			}
		})
	}
}
