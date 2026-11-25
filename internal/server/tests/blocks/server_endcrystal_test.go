package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// crystalPacket is the next packet with the given ID, the acknowledgements
// and background traffic before it skipped.
func crystalPacket(c *servertest.Client, id int32) []byte {
	c.T.Helper()
	var got []byte
	if !c.Pump(servertest.ExpectTimeout, func(pid int32) bool { return pid != id }, func(_ int32, data []byte) bool {
		got = data
		return true
	}) {
		c.T.Fatalf("no packet 0x%02X arrived", id)
	}
	return got
}

var endCrystalItemID = servertest.RegistryItemID("minecraft:end_crystal")

// endCrystalPlayer logs a creative player in holding an end crystal, three
// blocks south of the cell (3, 64, 3).
func endCrystalPlayer(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: endCrystalItemID, Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 20, true)
	c.Settle()
	return srv, c
}

// TestAnEndCrystalStandsOnObsidian uses the item on obsidian and on bedrock:
// the crystal appears centred over the block, with its bottom hidden.
// vanilla: EndCrystalItem.useOn
func TestAnEndCrystalStandsOnObsidian(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"minecraft:obsidian", "minecraft:bedrock"} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			srv, c := endCrystalPlayer(t)
			srv.SetBlock(3, 63, 3, registry.MustState(base, nil))
			c.PlaceBlock(3, 63, 3, protocol.FaceTop)
			data := crystalPacket(c, protocol.IDPlaySpawnEntity)
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if got := r.ReadVarInt(); got != protocol.EntityTypeEndCrystal {
				t.Fatalf("spawned entity type = %d, want the end crystal %d", got, protocol.EntityTypeEndCrystal)
			}
			if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 3.5 || y != 64 || z != 3.5 {
				t.Errorf("crystal at (%v, %v, %v), want (3.5, 64, 3.5)", x, y, z)
			}
			meta := protocol.NewReader(crystalPacket(c, protocol.IDPlayEntityMetadata))
			meta.ReadVarInt()
			// Only the field that differs from its default is sent.
			if idx := meta.ReadU8(); idx != protocol.MetadataIndexEndCrystalShowBottom {
				t.Fatalf("first metadata index = %d, want the show-bottom flag", idx)
			}
			meta.ReadVarInt()
			if meta.ReadBool() {
				t.Error("a placed crystal shows its bottom, want it hidden")
			}
			if end := meta.ReadU8(); end != 0xFF {
				t.Errorf("metadata continues at index %d, want the end marker", end)
			}
			if got := srv.EndCrystalCount(); got != 1 {
				t.Errorf("%d end crystals stand, want 1", got)
			}
		})
	}
}

// TestAnEndCrystalNeedsObsidianAndRoom refuses stone, an occupied cell above
// and a body in the way.
func TestAnEndCrystalNeedsObsidianAndRoom(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	c.PlaceBlock(3, 63, 3, protocol.FaceTop) // stone
	obsidian := registry.MustState("minecraft:obsidian", nil)
	srv.SetBlock(5, 63, 3, obsidian)
	srv.SetBlock(5, 64, 3, obsidian)
	c.PlaceBlock(5, 63, 3, protocol.FaceTop) // covered
	srv.SetBlock(7, 63, 3, obsidian)
	srv.SpawnArmorStand(7.5, 64, 3.5)
	c.PlaceBlock(7, 63, 3, protocol.FaceTop) // a body in the cell
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	if got := srv.EndCrystalCount(); got != 0 {
		t.Errorf("%d end crystals stand, want none", got)
	}
}

func TestArrowBlocksEntityPlacement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		item   registry.Item
		typ    int32
		count  func(*server.Server) int
		clearX float64
	}{
		{"end crystal", registry.ItemEndCrystal, protocol.EntityTypeEndCrystal, (*server.Server).EndCrystalCount, 9.25},
		{"armor stand", registry.ItemArmorStand, protocol.EntityTypeArmorStand, (*server.Server).ArmorStandCount, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "Placer"
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(8, 63, 0, registry.MustState("minecraft:obsidian", nil))
			srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: int32(tc.item), Count: 2})
			c.MoveToGrounded(8.5, 64, -2.5, 0, 30, true)
			c.ChatCommand("summon arrow 8.7 64.55 0.6 {NoGravity:1b}")
			c.AwaitSpawn(protocol.EntityTypeArrow)

			c.PlaceBlockAt(8, 63, 0, protocol.FaceTop, 1)
			c.Settle()
			if got := tc.count(srv); got != 0 {
				t.Errorf("placed entities with an overlapping arrow = %d, want 0", got)
			}
			if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 2 {
				t.Errorf("held count with an overlapping arrow = %d, want 2", got)
			}
			if t.Failed() {
				return
			}

			// Touching the east face of the placement box leaves its interior free.
			c.ChatCommand(fmt.Sprintf("tp @e[type=arrow,limit=1] %g 64.55 0.6", tc.clearX))
			servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
			c.PlaceBlockAt(8, 63, 0, protocol.FaceTop, 1)
			c.AwaitSpawn(tc.typ)
			c.Settle()
			if got := tc.count(srv); got != 1 {
				t.Errorf("placed entities after moving the arrow = %d, want 1", got)
			}
			if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
				t.Errorf("held count after moving the arrow = %d, want 1", got)
			}
		})
	}
}

// TestHittingAnEndCrystalExplodesIt removes the crystal and sets off a
// power-6 blast at it.
// vanilla: EndCrystal.hurtServer
func TestHittingAnEndCrystalExplodesIt(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	id := srv.SpawnEndCrystal(3.5, 64, 3.5)
	crystalPacket(c, protocol.IDPlaySpawnEntity)

	c.Attack(id)
	removed, exploded := false, false
	c.Pump(servertest.ExpectTimeout, nil, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlayRemoveEntities:
			removed = true
		case protocol.IDPlayExplosion:
			if boom := servertest.ReadExplosion(c, data); boom.Radius != 6 || boom.X != 3.5 || boom.Y != 64 || boom.Z != 3.5 {
				t.Errorf("explosion %+v, want power 6 at the crystal", boom)
			}
			exploded = true
		}
		return removed && exploded
	})
	if !removed || !exploded {
		t.Fatalf("removed = %v, exploded = %v, want both", removed, exploded)
	}
	if got := srv.EndCrystalCount(); got != 0 {
		t.Errorf("%d end crystals stand after the hit, want none", got)
	}
}

// TestABlockCannotBePlacedIntoAnEndCrystal refuses stone in the crystal's
// cell, and an armor stand there.
// vanilla: EndCrystal.canBeCollidedWith, BlockItem.canPlace
func TestABlockCannotBePlacedIntoAnEndCrystal(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	srv.SpawnEndCrystal(3.5, 64, 3.5)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.RegistryItemID("minecraft:stone"), Count: 1})
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.RegistryItemID("minecraft:armor_stand"), Count: 1})
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	if got := srv.BlockAt(3, 64, 3); got != 0 {
		t.Errorf("block %d stands in the crystal's cell, want air", got)
	}
	if got := srv.ArmorStandCount(); got != 0 {
		t.Errorf("%d armor stands stand, want none placed into the crystal", got)
	}
}

// TestAnEndCrystalLightsFireOnlyInTheEnd ticks a crystal in the End and one
// in the overworld: the End's fills its air cell with fire.
// vanilla: EndCrystal.tick
func TestAnEndCrystalLightsFireOnlyInTheEnd(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	srv.StopTick()
	t.Cleanup(srv.HoldChunkInDimension(world.EndName, 8, 8))
	srv.SetBlockInDimension(world.EndName, 8, 69, 8, registry.MustState("minecraft:obsidian", nil))
	srv.SetBlockInDimension(world.EndName, 8, 70, 8, 0)
	srv.SpawnEndCrystalInDimension(world.EndName, 8.5, 70, 8.5)
	srv.SetBlock(3, 63, 3, registry.MustState("minecraft:obsidian", nil))
	srv.SpawnEndCrystal(3.5, 64, 3.5)
	srv.TickEndCrystals()
	if srv.BlockInDimension(world.EndName, 8, 70, 8) == 0 {
		t.Error("the End crystal's cell is still air, want fire")
	}
	if srv.BlockAt(3, 64, 3) != 0 {
		t.Error("an overworld crystal lit a fire")
	}
}

// TestAPlacedEndCrystalUsesUpTheItemInSurvival keeps the stack in creative
// and takes one in survival.
// vanilla: EndCrystalItem.useOn
func TestAPlacedEndCrystalUsesUpTheItemInSurvival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Placer")
	srv.GiveSlot("Placer", 36, protocol.ItemStack{ItemID: endCrystalItemID, Count: 2})
	srv.SetBlock(3, 63, 3, registry.MustState("minecraft:obsidian", nil))
	c.MoveToGrounded(3.5, 64, 5.5, 180, 20, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.EndCrystalCount() == 1 })
	servertest.WaitFor(t, srv, 100, func() bool { return srv.InventorySlot("Placer", 36).Count == 1 })
}
