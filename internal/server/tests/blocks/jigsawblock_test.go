package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestSetJigsawBlockProgramsTheBlockEntity: a creative operator's Set Jigsaw
// Block writes the seven fields, `/data get block` reads them back and the
// client is sent Block Entity Data; a non-operator's packet changes nothing.
// vanilla: ServerGamePacketListenerImpl.handleSetJigsawBlock
func TestSetJigsawBlockProgramsTheBlockEntity(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	c := connect().Join("Actor")
	guest := connect().Join("Guest")
	c.Settle()
	guest.Settle()
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:jigsaw[orientation=north_up]")

	c.SetJigsawBlock(servertest.JigsawBlockPacket{
		X: 10, Y: 64, Z: 10, Name: "minecraft:a", Target: "minecraft:empty", Pool: "minecraft:empty",
		FinalState: "minecraft:air", Joint: "rollable", SelectionPriority: 3, Placement: 4,
	})
	// The placement's default tag can arrive before the programmed update.
	var data nbt.Compound
	for range 4 {
		var beType int32
		_, _, _, beType, data = c.AwaitBlockEntityData()
		if beType != protocol.BlockEntityJigsaw {
			t.Fatalf("block entity type = %d, want the jigsaw's %d", beType, protocol.BlockEntityJigsaw)
		}
		if data["name"] == nbt.String("minecraft:a") {
			break
		}
	}
	if data["name"] != nbt.String("minecraft:a") {
		t.Fatalf("update tag %v has no programmed name", data)
	}

	guest.SetJigsawBlock(servertest.JigsawBlockPacket{
		X: 10, Y: 64, Z: 10, Name: "minecraft:b", Target: "minecraft:b", Pool: "minecraft:b",
		FinalState: "minecraft:stone", Joint: "aligned", SelectionPriority: 9, Placement: 9,
	})
	guest.Settle()

	for field, want := range map[string]string{
		"name": `"minecraft:a"`, "pool": `"minecraft:empty"`, "joint": `"rollable"`,
		"selection_priority": "3", "placement_priority": "4",
	} {
		c.ChatCommand("data get block 10 64 10 " + field)
		c.ExpectCommandFeedback("commands.data.block.query", "10", "64", "10", want)
	}
}

// TestJigsawGenerateGrowsThePool: Jigsaw Generate from an operator builds the
// programmed pool's piece beside the block, replacing the jigsaw blocks; a
// non-operator's packet is ignored.
// vanilla: ServerGamePacketListenerImpl.handleJigsawGenerate, JigsawBlockEntity.generate
func TestJigsawGenerateGrowsThePool(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	c := connect().Join("Actor")
	guest := connect().Join("Guest")
	c.Settle()
	guest.Settle()
	servertest.Setblock(t, c, 10, 64, 10, "minecraft:jigsaw[orientation=up_north]")
	c.SetJigsawBlock(servertest.JigsawBlockPacket{
		X: 10, Y: 64, Z: 10, Name: "minecraft:a", Target: "minecraft:bottom",
		Pool: "minecraft:village/plains/town_centers", FinalState: "minecraft:air", Joint: "rollable",
	})
	c.Settle()

	before := map[[3]int32]uint16{}
	for x := int32(-10); x <= 30; x++ {
		for z := int32(-10); z <= 30; z++ {
			for y := int32(55); y <= 100; y++ {
				before[[3]int32{x, y, z}] = srv.BlockAt(x, y, z)
			}
		}
	}
	changed := func() int {
		n := 0
		for pos, state := range before {
			if srv.BlockAt(pos[0], pos[1], pos[2]) != state {
				n++
			}
		}
		return n
	}
	guest.JigsawGenerate(10, 64, 10, 1, false)
	guest.Settle()
	if n := changed(); n != 0 {
		t.Fatalf("a non-operator's Jigsaw Generate placed %d blocks", n)
	}
	c.JigsawGenerate(10, 64, 10, 1, false)
	c.Settle()
	if changed() == 0 {
		t.Fatal("Jigsaw Generate placed nothing")
	}
	if n := jigsawsAbove(srv.BlockAt); n != 0 {
		t.Fatalf("keepJigsaws=false left %d jigsaw blocks in the placed piece", n)
	}
}

// jigsawsAbove counts jigsaw blocks above the source block in the scanned area.
func jigsawsAbove(at func(x, y, z int32) uint16) int {
	n := 0
	for x := int32(-10); x <= 30; x++ {
		for z := int32(-10); z <= 30; z++ {
			for y := int32(65); y <= 100; y++ {
				if registry.BlockOf(at(x, y, z)) == registry.BlockJigsaw {
					n++
				}
			}
		}
	}
	return n
}
