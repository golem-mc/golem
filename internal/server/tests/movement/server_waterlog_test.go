package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// waterlogCase is one placement: the item placed, the water level of the cell
// it goes into ("" for dry air), the properties the placed state must carry in
// every block update at the cell, and whether the block needs a wall to lean
// on, as a ladder does.
type waterlogCase struct {
	what      string
	item      string
	level     string
	want      map[string]string
	needsWall bool
}

// waterlogCases are the waterloggable blocks and the fluid each is placed
// into. A source waterlogs most of them and a flow of level 1-7 does not; this
// is the fluid test FenceBlock, TrapDoorBlock and the slab use. The coral
// family and the conduit read is(FluidTags.WATER) && isFull instead, which a
// falling column (level 8 and up) also passes. The campfire is unlit when the
// water waterlogs it, and the light, whose class has no getStateForPlacement,
// is left dry.
// vanilla: TrapDoorBlock.getStateForPlacement, BaseCoralPlantTypeBlock.getStateForPlacement
func waterlogCases() []waterlogCase {
	var cases []waterlogCase
	sourceOnly := []struct{ name, item string }{
		{"stair", "minecraft:oak_stairs"},
		{"slab", "minecraft:oak_slab"},
		{"fence", "minecraft:oak_fence"},
		{"glass pane", "minecraft:glass_pane"},
		{"wall", "minecraft:cobblestone_wall"},
		{"chest", "minecraft:chest"},
		{"ladder", "minecraft:ladder"},
		{"trapdoor", "minecraft:oak_trapdoor"},
		{"sign", "minecraft:oak_sign"},
		{"leaves", "minecraft:oak_leaves"},
		{"bars", "minecraft:iron_bars"},
		{"chain", "minecraft:iron_chain"},
	}
	for _, b := range sourceOnly {
		wall := b.name == "ladder"
		cases = append(cases,
			waterlogCase{b.name + " in a source", b.item, "0", map[string]string{"waterlogged": "true"}, wall},
			waterlogCase{b.name + " in flowing water", b.item, "1", map[string]string{"waterlogged": "false"}, wall},
			waterlogCase{b.name + " in falling water", b.item, "8", map[string]string{"waterlogged": "false"}, wall},
		)
	}
	coral := []struct{ name, item string }{
		{"coral", "minecraft:tube_coral"},
		{"coral fan", "minecraft:tube_coral_fan"},
		{"dead coral", "minecraft:dead_tube_coral"},
	}
	for _, b := range coral {
		cases = append(cases,
			waterlogCase{b.name + " in a source", b.item, "0", map[string]string{"waterlogged": "true"}, false},
			waterlogCase{b.name + " in flowing water", b.item, "1", map[string]string{"waterlogged": "false"}, false},
			waterlogCase{b.name + " in falling water", b.item, "8", map[string]string{"waterlogged": "true"}, false},
			waterlogCase{b.name + " in falling water at level 9", b.item, "9", map[string]string{"waterlogged": "true"}, false},
			waterlogCase{b.name + " on dry land", b.item, "", map[string]string{"waterlogged": "false"}, false},
		)
	}
	return append(cases,
		waterlogCase{"conduit in a source", "minecraft:conduit", "0", map[string]string{"waterlogged": "true"}, false},
		waterlogCase{"conduit in flowing water", "minecraft:conduit", "1", map[string]string{"waterlogged": "false"}, false},
		waterlogCase{"conduit in falling water", "minecraft:conduit", "8", map[string]string{"waterlogged": "true"}, false},
		waterlogCase{"conduit in falling water at level 15", "minecraft:conduit", "15", map[string]string{"waterlogged": "true"}, false},
		waterlogCase{"campfire in a source", "minecraft:campfire", "0", map[string]string{"waterlogged": "true", "lit": "false"}, false},
		waterlogCase{"campfire in flowing water", "minecraft:campfire", "1", map[string]string{"waterlogged": "false", "lit": "true"}, false},
		waterlogCase{"soul campfire in a source", "minecraft:soul_campfire", "0", map[string]string{"waterlogged": "true", "lit": "false"}, false},
		waterlogCase{"light in a source", "minecraft:light", "0", map[string]string{"waterlogged": "false"}, false},
		waterlogCase{"light on dry land", "minecraft:light", "", map[string]string{"waterlogged": "false"}, false},
	)
}

// TestPlacingWaterloggableBlocksReadsTheFluid checks that every block able to
// hold water takes its waterlogged property from the fluid in the cell it is
// placed into, in the first block update the placing client hears: a water
// source waterlogs it, a flow does not, and no update at the cell says
// otherwise before the acknowledgement. A placement that waterlogs only on the
// next tick — or not at all, as fences, panes, walls and trapdoors did — fails
// here.
func TestPlacingWaterloggableBlocksReadsTheFluid(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, actor := servertest.Join(t, cfg, "Actor")
	actor.Settle()

	for i, c := range waterlogCases() {
		// A cell of its own, so nothing one case leaves behind reaches the
		// next.
		x := int32(4 + (i%20)*2)
		z := int32(4 + (i/20)*2)
		actor.MoveToGround(float64(x)-0.5, 64, float64(z)-0.5, true)
		actor.Settle()

		srv.SetBlock(x, 63, z, 1)
		// A stone hem keeps the cases apart: a waterlogged block spreads its
		// water, and a neighbour case's spread would wake or flood this
		// case's cell before the placement reads it.
		srv.SetBlock(x-1, 64, z, 1)
		srv.SetBlock(x+1, 64, z, 1)
		srv.SetBlock(x, 64, z-1, 1)
		srv.SetBlock(x, 64, z+1, 1)
		if c.level != "" {
			water, ok := registry.FindState("minecraft:water", map[string]string{"level": c.level})
			if !ok || !srv.SetBlock(x, 64, z, water) {
				t.Fatalf("%s: could not set the water", c.what)
			}
		}
		item, _ := registry.ItemID(c.item)
		actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 1})
		actor.FacePlayer(float64(x)-0.5, 64, float64(z)-0.5, 180, 0)

		// A ladder needs a wall: a stone north of the water cell, clicked
		// on its south face, takes the ladder into the water. Every other
		// block goes on top of the floor under the water.
		clickX, clickY, clickZ := x, int32(63), z
		var face int32 = protocol.FaceTop
		if c.needsWall {
			srv.SetBlock(x, 64, z-1, 1)
			clickX, clickY, clickZ = x, 64, z-1
			face = protocol.FaceSouth
		}
		actor.PlaceBlock(clickX, clickY, clickZ, face)

		// The updates at the cell up to the acknowledgement must all carry
		// the fluid's waterlogging, and the ack must not overtake the first
		// of them: one that did would make the client roll its prediction
		// back to the dry block the ack named.
		var updates []map[string]string
		firstSeen, ackFirst := false, false
		actor.ReadTicks(srv, 100, func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlayBlockUpdate:
				r := protocol.NewReader(data)
				ux, uy, uz := r.ReadPosition()
				if ux != x || uy != 64 || uz != z {
					return false
				}
				_, props, _ := registry.BlockStateName(uint16(r.ReadVarInt()))
				updates = append(updates, registry.ParseStateProps(props))
				if !firstSeen {
					firstSeen = true
					if ackFirst {
						t.Errorf("%s: the block ack arrived before the first block update at the cell", c.what)
					}
				}
			case protocol.IDPlayAckBlockChange:
				if !firstSeen {
					ackFirst = true
				}
				return true
			}
			return false
		})
		if len(updates) == 0 {
			t.Fatalf("%s: no block update at the placed cell", c.what)
		}
		for n, props := range updates {
			for prop, want := range c.want {
				if props[prop] != want {
					t.Errorf("%s: block update %d at the cell has %s=%s, want %s",
						c.what, n, prop, props[prop], want)
				}
			}
		}
	}
}
