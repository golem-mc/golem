package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

type plantPlacement struct {
	item, head, body, base string
	dy                     int32 // growth direction
	face                   int32 // face of the head the item is used on
}

var plantPlacements = []plantPlacement{
	{"minecraft:kelp", "minecraft:kelp", "minecraft:kelp_plant", "minecraft:sand", 1, protocol.FaceTop},
	{"minecraft:twisting_vines", "minecraft:twisting_vines", "minecraft:twisting_vines_plant", "minecraft:stone", 1, protocol.FaceTop},
	{"minecraft:weeping_vines", "minecraft:weeping_vines", "minecraft:weeping_vines_plant", "minecraft:stone", -1, protocol.FaceBottom},
	{"minecraft:glow_berries", "minecraft:cave_vines", "minecraft:cave_vines_plant", "minecraft:stone", -1, protocol.FaceBottom},
}

// TestPlacingAGrowingPlantOnItsHeadMovesTheHead checks that a kelp or vine
// laid against the head in its growth direction becomes the new head with a
// random age of 0 to 24 and turns the old head into the body.
// vanilla: GrowingPlantBlock.getStateForPlacement, GrowingPlantHeadBlock.updateShape
func TestPlacingAGrowingPlantOnItsHeadMovesTheHead(t *testing.T) {
	t.Parallel()
	for _, tc := range plantPlacements {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
			srv.StopTick()
			item, _ := registry.ItemID(tc.item)
			srv.GiveSlot("Gardener", player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 64})

			const trials = 8
			const y = 66
			ages := map[string]bool{}
			for i := range int32(trials) {
				x := i - 4
				fill := "minecraft:air"
				if tc.item == "minecraft:kelp" {
					fill = "minecraft:water"
				}
				for dy := int32(1); dy <= 3; dy++ {
					if fill != "minecraft:air" {
						servertest.SetFarmBlock(t, c, srv, x, y+dy*tc.dy, 0, fill, nil)
					}
				}
				servertest.SetFarmBlock(t, c, srv, x, y-tc.dy, 0, tc.base, nil)
				servertest.SetFarmBlock(t, c, srv, x, y, 0, tc.head, map[string]string{"age": "3"})

				c.PlaceBlock(x, y, 0, tc.face)
				c.SettleKeeping()
				srv.FlushBlockAcks()
				c.ExpectBlockAck()

				name, props := servertest.StateProps(srv.BlockAt(x, y, 0))
				if name != tc.body {
					t.Fatalf("trial %d: the old head stands as %s %v, want %s", i, name, props, tc.body)
				}
				name, props = servertest.StateProps(srv.BlockAt(x, y+tc.dy, 0))
				if name != tc.head {
					t.Fatalf("the new tip stands as %s %v, want %s", name, props, tc.head)
				}
				age, err := strconv.Atoi(props["age"])
				if err != nil || age < 0 || age > 24 {
					t.Fatalf("the new tip has age %q, want 0 to 24", props["age"])
				}
				ages[props["age"]] = true
			}
			if len(ages) < 2 {
				t.Errorf("%d placements all gave age %v, want a random age", trials, ages)
			}
		})
	}
}

// TestPlacingAGrowingPlantBelowItsHeadGivesTheBody checks that a plant laid
// in the cell its growth direction points away from, with a head already
// standing in the growth direction, is the body block.
// vanilla: GrowingPlantBlock.getStateForPlacement
func TestPlacingAGrowingPlantBelowItsHeadGivesTheBody(t *testing.T) {
	t.Parallel()
	for _, tc := range plantPlacements {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
			srv.StopTick()
			item, _ := registry.ItemID(tc.item)
			srv.GiveSlot("Gardener", player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 3})

			const y = 66
			fill := "minecraft:air"
			if tc.item == "minecraft:kelp" {
				fill = "minecraft:water"
			}
			for dy := int32(0); dy <= 3; dy++ {
				if fill != "minecraft:air" {
					servertest.SetFarmBlock(t, c, srv, 3, y+dy*tc.dy, 0, fill, nil)
				}
			}
			servertest.SetFarmBlock(t, c, srv, 3, y-tc.dy, 0, tc.base, nil)
			servertest.SetFarmBlock(t, c, srv, 3, y+tc.dy, 0, tc.head, map[string]string{"age": "3"})

			// The base's far face, aimed at the cell between it and the head.
			baseFace := int32(protocol.FaceTop)
			if tc.dy < 0 {
				baseFace = protocol.FaceBottom
			}
			c.PlaceBlock(3, y-tc.dy, 0, baseFace)
			c.SettleKeeping()
			srv.FlushBlockAcks()
			c.ExpectBlockAck()

			if name, props := servertest.StateProps(srv.BlockAt(3, y, 0)); name != tc.body {
				t.Errorf("the plant laid below the head stands as %s %v, want %s", name, props, tc.body)
			}
			if name, props := servertest.StateProps(srv.BlockAt(3, y+tc.dy, 0)); name != tc.head || props["age"] != "3" {
				t.Errorf("the head stands as %s %v, want %s at age 3", name, props, tc.head)
			}
		})
	}
}
