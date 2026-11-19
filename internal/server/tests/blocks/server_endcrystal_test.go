package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

// TestABlastRemovesAnEndCrystalWithoutAnother has one crystal's blast take
// down the second crystal beside it without a second explosion.
func TestABlastRemovesAnEndCrystalWithoutAnother(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	first := srv.SpawnEndCrystal(3.5, 64, 3.5)
	srv.SpawnEndCrystal(5.5, 64, 3.5)
	c.Attack(first)
	booms := 0
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 40, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayExplosion {
			booms++
		}
		return false
	})
	if booms != 1 {
		t.Errorf("%d explosions, want only the hit crystal's", booms)
	}
	if got := srv.EndCrystalCount(); got != 0 {
		t.Errorf("%d end crystals stand, want the blast to have taken both", got)
	}
}

// TestADefaultEndCrystalSendsNoMetadata shows a crystal whose fields all hold
// their defaults, as the End's pillar crystals do: the spawn alone goes out.
// vanilla: ServerEntity.sendPairingData
func TestADefaultEndCrystalSendsNoMetadata(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	id := srv.SpawnEndCrystal(3.5, 64, 3.5)
	crystalPacket(c, protocol.IDPlaySpawnEntity)
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			t.Error("a default crystal was sent metadata")
		}
		return false
	})
}

// TestAWindChargeLeavesAnEndCrystalStanding bursts a wind charge beside a
// crystal: its blast may not hurt entities.
// vanilla: ServerExplosion.hurtEntities, WindCharge.explode
func TestAWindChargeLeavesAnEndCrystalStanding(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	stone := registry.BlockStates["minecraft:stone"]
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(14, y, 0, stone)
	}
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	srv.SpawnEndCrystal(12.5, 64, 0.5)
	servertest.UseHeld(c, -90, 0)
	servertest.HoldTicks(t, srv)
	c.ReadTicks(srv, 60, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayExplosion
	})
	c.SettleAndCollect(func(int32, []byte) {})
	if got := srv.EndCrystalCount(); got != 1 {
		t.Errorf("%d end crystals stand after the burst, want the one it left", got)
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

// TestAnItemInTheWayStopsAnEndCrystal refuses a crystal over a dropped item.
// vanilla: EndCrystalItem.useOn
func TestAnItemInTheWayStopsAnEndCrystal(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	srv.SetBlock(3, 63, 3, registry.MustState("minecraft:obsidian", nil))
	srv.SpawnItemEntityDelay(3.5, 64, 3.5, "minecraft:stone", 1, 1000)
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	if got := srv.EndCrystalCount(); got != 0 {
		t.Errorf("%d end crystals stand, want none over an item", got)
	}
}

// TestAnEndCrystalObstructsAMobSpawn lists the crystal among the bodies a
// spawn is measured against.
// vanilla: Entity.blocksBuilding
func TestAnEndCrystalObstructsAMobSpawn(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	if got := len(srv.SpawnBodiesForTest(nil)); got != 0 {
		t.Fatalf("%d bodies before the crystal, want none", got)
	}
	srv.SpawnEndCrystal(3.5, 64, 3.5)
	if got := len(srv.SpawnBodiesForTest(nil)); got != 1 {
		t.Errorf("%d bodies with a crystal standing, want 1", got)
	}
}

// TestHittingAnEndCrystalPlaysTheBlow checks that the hit spends the attack
// like any other: the attack sound plays.
// vanilla: Player.attack
func TestHittingAnEndCrystalPlaysTheBlow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Puncher")
	c.MoveToGrounded(3.5, 64, 5.5, 180, 20, true)
	c.Settle()
	id := srv.SpawnEndCrystal(3.5, 64, 3.5)
	c.Attack(id)
	sounded := false
	c.Pump(servertest.ExpectTimeout, nil, func(pid int32, _ []byte) bool {
		if pid == protocol.IDPlaySoundEffect {
			sounded = true
		}
		return sounded
	})
	if !sounded {
		t.Error("no attack sound followed the hit")
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

// TestAnEndCrystalIsShownOnArrival has a crystal standing in the End shown to
// a player who crosses into it.
func TestAnEndCrystalIsShownOnArrival(t *testing.T) {
	t.Parallel()
	srv, c := endCrystalPlayer(t)
	t.Cleanup(srv.HoldChunkInDimension(world.EndName, 8, 8))
	id := srv.SpawnEndCrystalInDimension(world.EndName, 8.5, 70, 8.5)
	srv.MovePlayerToDimension("Placer", world.EndName, 8.5, 72, 8.5)
	if !c.Pump(servertest.ExpectTimeout, func(pid int32) bool { return pid != protocol.IDPlaySpawnEntity }, func(_ int32, data []byte) bool {
		return protocol.NewReader(data).ReadVarInt() == id
	}) {
		t.Error("the End crystal was not shown on arrival")
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

// TestAPlacedEndCrystalSurvivesARestart saves a placed crystal and stands the
// server back up: the crystal returns with its bottom hidden.
// vanilla: EndCrystal.addAdditionalSaveData
func TestAPlacedEndCrystalSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: endCrystalItemID, Count: 1})
	srv.SetBlock(3, 63, 3, registry.MustState("minecraft:obsidian", nil))
	c.MoveToGrounded(3.5, 64, 5.5, 180, 20, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	servertest.WaitFor(t, srv, 100, func() bool { return srv.EndCrystalCount() == 1 })
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:end_crystal", func(compound nbt.Compound) {
		if got := compound["ShowBottom"]; got != nbt.Tag(nbt.Byte(0)) {
			t.Errorf("saved ShowBottom = %v, want 0", got)
		}
	})
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{protocol.EntityTypeEndCrystal: "crystal"})
	back := servertest.CapturedOr(t, again, shown, "crystal", protocol.EntityTypeEndCrystal)
	if back.X != 3.5 || back.Y != 64 || back.Z != 3.5 {
		t.Errorf("the restored crystal stands at (%v, %v, %v), want (3.5, 64, 3.5)", back.X, back.Y, back.Z)
	}
	if bottom := servertest.MetaOf(back.Meta, protocol.MetadataIndexEndCrystalShowBottom); bottom == nil || bottom.FlagVal {
		t.Errorf("the restored crystal's show-bottom = %v, want hidden", bottom)
	}
	if againSrv.EndCrystalCount() != 1 {
		t.Errorf("the restarted server holds %d crystals, want 1", againSrv.EndCrystalCount())
	}
}
