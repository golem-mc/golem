package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestBorderStopsMovingBodies checks that a thrown item and a pushed cow stop
// at the edge of a 10-block border, while with a wide border the item passes
// x=5.
// vanilla: Entity.collectCollidersIgnoringWorldBorder
func TestBorderStopsMovingBodies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		border string
		wall   bool
		// edge is the wall's outer face: the border's edge rounded out.
		edge float64
	}{
		{"narrow", "10", true, 5},
		{"fractional", "10.5", true, 6},
		{"wide", "1000", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Walker", servertest.OpsListing(t, "Walker"))
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			c.ChatCommand("worldborder set " + tc.border)
			c.Settle()

			servertest.HoldTicks(t, srv)
			itemID := srv.SpawnItemEntityMotion(3.5, 66, 0.5, "minecraft:stone", 1, 1.0, 0.2, 0)
			cow := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
			srv.PushMob(cow, 1.0, 0, 0)
			servertest.RunTicks(t, srv, 20)

			var itemX float64
			found := false
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				if it.ID == itemID {
					itemX, found = it.X, true
				}
			}
			if !found {
				t.Fatal("the item is gone")
			}
			cowX, _, _, ok := srv.MobPos(cow)
			if !ok {
				t.Fatal("the cow is gone")
			}
			if tc.wall {
				if itemX > tc.edge-0.125+1e-6 {
					t.Errorf("item x = %v, want <= %v", itemX, tc.edge-0.125)
				}
				if cowX > tc.edge-0.45+1e-6 {
					t.Errorf("cow x = %v, want <= %v", cowX, tc.edge-0.45)
				}
			} else if itemX <= 5 {
				t.Errorf("item x = %v, want past 5 with a wide border", itemX)
			}
		})
	}
}
