package entities

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// cushionColorIndex is the cushion's DATA_COLOR, after Entity's eight
// fields. vanilla: Cushion.DATA_COLOR
const cushionColorIndex = 8

var cushionType = int32(registry.EntityTypeCushion)

// joinCushionPlacer joins a survival player two blocks south of a stone at
// (3, 63, 3), holding n red cushions.
func joinCushionPlacer(t *testing.T, n int32, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Sitter", setup...)
	srv.SetBlock(3, 63, 3, registry.BlockStone.DefaultState())
	srv.GiveSlot("Sitter", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemRedCushion), Count: n})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	return srv, c
}

// placeRedCushion clicks the stone's top as a client does, the cursor on
// the face, and returns the cushion's spawn.
func placeRedCushion(c *servertest.Client) servertest.CapturedEntity {
	c.T.Helper()
	c.PlaceBlockAt(3, 63, 3, protocol.FaceTop, 1)
	return c.AwaitSpawn(cushionType)
}

// TestACushionGoesDownOnAStonesTopOnly uses a red cushion on a stone's top:
// a red cushion stands centred on the column at the click's height and one
// item is spent. A second use on the same spot and a use on a side face
// spawn nothing and spend nothing.
// vanilla: CushionItem.useOn
func TestACushionGoesDownOnAStonesTopOnly(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 3)

	spawn := placeRedCushion(c)
	if spawn.X != 3.5 || spawn.Y != 64 || spawn.Z != 3.5 {
		t.Errorf("the cushion stands at (%v, %v, %v), want (3.5, 64, 3.5)", spawn.X, spawn.Y, spawn.Z)
	}
	meta := awaitFixtureMeta(c, spawn.EntityID)
	red := int32(14) // vanilla: DyeColor.RED
	if f, ok := meta[cushionColorIndex]; !ok || f.typ != 43 || f.intV != red {
		t.Errorf("the cushion's color field is %+v (present %v), want dye color %d", f, ok, red)
	}
	servertest.UntilTicks(t, srv, 20, "one cushion spent", func() bool {
		return srv.InventorySlot("Sitter", player.HotbarSlot).Count == 2
	})

	// The same spot again, then the stone at (3, 64, 3)'s north face.
	c.PlaceBlockAt(3, 63, 3, protocol.FaceTop, 1)
	srv.SetBlock(3, 64, 4, registry.BlockStone.DefaultState())
	c.PlaceBlockAtCursor(3, 64, 4, protocol.FaceNorth, 0.5, 0.5, 0)
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySpawnEntity))
	if got := srv.FixtureCount(); got != 1 {
		t.Errorf("%d cushions stand, want the first alone", got)
	}
	if got := srv.InventorySlot("Sitter", player.HotbarSlot).Count; got != 2 {
		t.Errorf("the hand holds %d cushions after the refused uses, want 2", got)
	}
}

// awaitRedCushionDrop waits for the one red cushion item a broken cushion
// lays.
func awaitRedCushionDrop(t *testing.T, srv *server.Server, ticks int) {
	t.Helper()
	var items []server.TestItem
	servertest.UntilTicks(t, srv, ticks, "the cushion's drop", func() bool {
		items = srv.ItemsInDimension("minecraft:overworld")
		return len(items) > 0
	})
	if len(items) != 1 || items[0].Item != "minecraft:red_cushion" || items[0].Count != 1 {
		t.Errorf("the drops are %+v, want one red_cushion", items)
	}
}

// TestAPunchedCushionDropsItsItem attacks a placed cushion in survival: it
// is removed and lays one red cushion.
// vanilla: BlockAttachedEntity.skipAttackInteraction, Cushion.dropItem
func TestAPunchedCushionDropsItsItem(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	spawn := placeRedCushion(c)

	c.Attack(spawn.EntityID)
	servertest.UntilTicks(t, srv, 20, "the cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
}

// TestACushionFallsWithTheBlockUnderIt removes the stone a cushion rests
// on: the survives poll, every 101st tick, breaks it and lays its item.
// vanilla: BlockAttachedEntity.tick, Cushion.wouldSurviveAt
func TestACushionFallsWithTheBlockUnderIt(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	placeRedCushion(c)

	srv.SetBlock(3, 63, 3, 0)
	servertest.UntilTicks(t, srv, 101, "the unsupported cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
}

// TestASummonedCushionKeepsItsColorThroughARestart summons a blue cushion
// on the ground, saves and stands a server back up: the file holds the
// color and the cell, and the cushion comes back blue.
// vanilla: Cushion.addAdditionalSaveData, BlockAttachedEntity.addAdditionalSaveData
func TestASummonedCushionKeepsItsColorThroughARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Mapper", servertest.OpsListing(t, "Mapper"))

	c.ChatCommand(`summon minecraft:cushion 3.5 64 4.5 {color:"blue"}`)
	c.AwaitSpawn(cushionType)
	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cushion", func(compound nbt.Compound) {
		if color, _ := nbt.Get[nbt.String](compound, "color"); color != "blue" {
			t.Errorf("saved color = %v, want blue", compound["color"])
		}
		if pos, _ := nbt.Get[nbt.IntArray](compound, "block_pos"); len(pos) != 3 || pos[0] != 3 || pos[1] != 64 || pos[2] != 4 {
			t.Errorf("saved block_pos = %v, want [3 64 4]", compound["block_pos"])
		}
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Mapper")
	again.Configure()
	again.JoinCapturing("Mapper", map[int32]string{cushionType: "cushion"})
	if got := againSrv.FixtureCount(); got != 1 {
		t.Fatalf("the restarted server holds %d fixtures, want the cushion", got)
	}
	if color, _ := nbt.Get[nbt.String](againSrv.FixtureSavedTag(), "color"); color != "blue" {
		t.Errorf("the reloaded cushion's color = %v, want blue", color)
	}
}

// TestACushionPutDownInLavaLastsUntilThePoll places a cushion in a lava
// cell: placement tests fire alone, so it stands until the poll, every
// 101st tick, breaks it and lays its item.
// vanilla: CushionItem.useOn, Cushion.tickAtCheckInterval
func TestACushionPutDownInLavaLastsUntilThePoll(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	srv.SetBlock(3, 64, 3, registry.BlockLava.DefaultState())

	placeRedCushion(c)
	// Held: the drop burns in the lava within a few ticks, so a free loop
	// running ahead of the poll can take it before it is seen.
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	if got := srv.FixtureCount(); got != 1 {
		t.Fatalf("%d cushions stand 5 ticks after the placement in lava, want 1", got)
	}
	servertest.UntilTicks(t, srv, 101, "the cushion's removal in lava", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
}

// TestAPistonHeadBreaksACushionInItsCell extends a piston into a placed
// cushion's cell: the move kills it and lays one red cushion.
// vanilla: BlockAttachedEntity.move
func TestAPistonHeadBreaksACushionInItsCell(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1, servertest.OpsListing(t, "Sitter"))
	placeRedCushion(c)

	servertest.Setblock(t, c, 2, 64, 3, "piston[facing=east]")
	servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
	servertest.UntilTicks(t, srv, 10, "the pushed cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
}

// TestALightningStruckCushionsDropOutlastsTheBoltsFire summons a bolt onto
// a cushion on stone, on normal so the bolt lights the strike cell: the
// cushion's item lands in that fire, invulnerable for 20 ticks, and still
// lies there 15 ticks on. A drop that took the fire's damage at once would
// burn up within five ticks; one left vulnerable may meet a late flash.
// vanilla: Cushion.thunderHit, Cushion.dropItem
func TestALightningStruckCushionsDropOutlastsTheBoltsFire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Striker", servertest.OpsListing(t, "Striker"))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Striker")
	x, y, z := int32(math.Floor(pos.X+8)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	srv.SetBlock(x, y-1, z, registry.BlockStone.DefaultState())
	srv.SetBlock(x, y, z, 0)
	bx, bz := float64(x)+0.5, float64(z)+0.5

	c.ChatCommand(fmt.Sprintf(`summon minecraft:cushion %v %d %v {color:"red"}`, bx, y, bz))
	c.AwaitSpawn(cushionType)
	c.ChatCommand(fmt.Sprintf("summon minecraft:lightning_bolt %v %d %v", bx, y, bz))
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "the struck cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	if name := srv.BlockNameAt(x, y, z); name != "minecraft:fire" {
		t.Fatalf("the strike cell holds %q, want the bolt's fire", name)
	}
	awaitRedCushionDrop(t, srv, 1)

	servertest.RunTicks(t, srv, 15)
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 1 || items[0].Item != "minecraft:red_cushion" {
		t.Errorf("15 ticks after the strike the drops are %+v, want the red cushion", items)
	}
}

// TestAnArrowBreaksACushion shoots a plain arrow into a placed cushion
// along the stone's top: the cushion is removed, lays one red cushion and
// the shaft is spent.
// vanilla: Cushion.hurtServer, BlockAttachedEntity.hurtServer
func TestAnArrowBreaksACushion(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	placeRedCushion(c)

	// Cross the north face at z=3 instead of ending the first tick on it.
	if id := srv.ShootArrow(3.5, 64.15, 1.75, 0, 0, 1.5); id == 0 {
		t.Fatal("the arrow never flew")
	}
	servertest.UntilTicks(t, srv, 20, "the cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
	if n := srv.ArrowCount(); n != 0 {
		t.Errorf("the server holds %d arrows after the shot, want the spent shaft gone", n)
	}
}

// vanilla: AABB.clipPoint, ProjectileUtil.getManyEntityHitResult, computeMargin
func TestAnArrowEndingOnACushionsFacePassesThrough(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	placeRedCushion(c)
	servertest.HoldTicks(t, srv)

	if id := srv.ShootArrow(3.5, 64.15, 1.5, 0, 0, 1.5); id == 0 {
		t.Fatal("the arrow never flew")
	}
	// The first tick ends at z=3 and the second starts on that face.
	// Neither tick has a margin; stop before the arrow reaches the sitter.
	servertest.RunTicks(t, srv, 2)
	if n := srv.FixtureCount(); n != 1 {
		t.Errorf("%d cushions stand, want the untouched cushion", n)
	}
	arrows := srv.ArrowsInDimension("minecraft:overworld")
	if len(arrows) != 1 || arrows[0].VZ <= 0 || arrows[0].Z <= 4 {
		t.Errorf("arrows = %+v, want one continuing south beyond the cushion", arrows)
	}
}

// TestASnowballBreaksACushion throws a snowball into a placed cushion: a
// hurt of no damage breaks it all the same.
// vanilla: Snowball.onHitEntity, BlockAttachedEntity.hurtServer
func TestASnowballBreaksACushion(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	placeRedCushion(c)

	if id := srv.ThrowThrown(entity.ThrownSnowball, 3.5, 64.15, 2, 0, 0, 0, 1); id == 0 {
		t.Fatal("the snowball never flew")
	}
	servertest.UntilTicks(t, srv, 20, "the cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitRedCushionDrop(t, srv, 20)
}

// TestAnAdventurePlayersArrowCannotBreakACushion shoots the placer's own
// shaft at a cushion after a switch to adventure: a shooter who may not
// build is refused, so the cushion stands and the shaft bounces off.
// vanilla: Cushion.isBreakingDeniedFor
func TestAnAdventurePlayersArrowCannotBreakACushion(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	placeRedCushion(c)
	srv.SetPlayerGameMode("Sitter", protocol.GameModeAdventure)

	shaft := srv.ShootPlayerArrow("Sitter", 1.5, 64.15, 3.5, 1.5, 0, 0)
	if shaft == 0 {
		t.Fatal("the arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 10)
	if n := srv.FixtureCount(); n != 1 {
		t.Errorf("%d cushions stand, want the adventure shooter's shaft refused", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it bounced and still in the world")
	}
}

// useCushion right-clicks the cushion with the main hand, the sneak key
// held or not.
func useCushion(c *servertest.Client, id int32, sneaking bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0, 0.25, 0)
		b.PutBool(sneaking)
	})
}

// awaitSeatY waits for the named player's feet to reach y.
func awaitSeatY(t *testing.T, srv *server.Server, name string, y float64) {
	t.Helper()
	servertest.UntilTicks(t, srv, 20, "the player's feet at the seat", func() bool {
		return servertest.Abs64(srv.PlayerPosition(name).Y-y) < 1e-9
	})
}

// TestAPlayerSitsOnACushion has one player sit on a placed cushion and get
// up again, while a second watches: a sneaking click seats nobody, a plain
// one seats the player 0.35 below the cushion's feet with the sit sound, the
// second player's click on the occupied cushion is refused, a sneak stands
// the sitter on the cushion's top with the get-up sound, /ride seats them
// again and the second player's blow breaks the cushion from under them.
// vanilla: Cushion.interact, Cushion.removePassenger, LivingEntity.dismountVehicle
func TestAPlayerSitsOnACushion(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Sitter"))
	c := connect().Join("Sitter")
	srv.SetBlock(3, 63, 3, registry.BlockStone.DefaultState())
	srv.GiveSlot("Sitter", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemRedCushion), Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	other := connect().Join("Other")
	other.MoveToGrounded(5.5, 64, 3.5, 90, 40, true)
	other.Settle()
	id := placeRedCushion(c).EntityID
	other.AwaitSpawn(cushionType)

	useCushion(c, id, true)
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySetPassengers))

	useCushion(c, id, false)
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the cushion's passengers = %v, want the sitter %d", riders, c.PlayerEntityID)
	}
	c.AwaitSoundNamed("minecraft:entity.cushion.sit")
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)
	servertest.AwaitPassengersFor(other, id)

	useCushion(other, id, false)
	other.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySetPassengers))

	c.SendSneak(true)
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 0 {
		t.Fatalf("the cushion's passengers after the sneak = %v, want none", riders)
	}
	c.AwaitSoundNamed("minecraft:entity.cushion.get_up")
	awaitSeatY(t, srv, "Sitter", 64.25)
	c.SendSneak(false)

	c.ChatCommand("ride @s mount @e[type=cushion,limit=1]")
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the cushion's passengers after /ride = %v, want the sitter", riders)
	}
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)

	other.Attack(id)
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 0 {
		t.Fatalf("the broken cushion's passengers = %v, want none", riders)
	}
	servertest.UntilTicks(t, srv, 20, "the cushion's removal", func() bool { return srv.FixtureCount() == 0 })
	awaitSeatY(t, srv, "Sitter", 64)
}

// TestAPlayerOnAPigSitsOnACushion has a player riding a pig click a
// cushion: they leave the pig and sit down.
// vanilla: Entity.startRiding
func TestAPlayerOnAPigSitsOnACushion(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Sitter", servertest.OpsListing(t, "Sitter"))
	srv.SetBlock(3, 63, 3, registry.BlockStone.DefaultState())
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	c.ChatCommand(`summon minecraft:cushion 3.5 64 3.5`)
	id := c.AwaitSpawn(cushionType).EntityID
	c.ChatCommand(`summon minecraft:pig 3.5 64 5.5 {NoAI:1b}`)
	pig := c.AwaitSpawn(int32(registry.EntityTypePig)).EntityID
	c.ChatCommand("ride @s mount @e[type=pig,limit=1]")
	servertest.AwaitPassengersFor(c, pig)

	useCushion(c, id, false)
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("the cushion's passengers = %v, want the sitter %d", riders, c.PlayerEntityID)
	}
	if got := srv.MobPassengerIDs(pig); len(got) != 0 {
		t.Errorf("the pig's passengers = %v, want none", got)
	}
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)
}

// TestAPlayerLeavingSeatedTakesTheCushion has a player sit on a cushion and
// leave: the cushion leaves the world with them, without its drop, their
// file carries it as RootVehicle, and the next join seats them on it again.
// vanilla: ServerPlayer.saveParentVehicle, PlayerList.remove,
// ServerPlayer.loadAndSpawnParentVehicle
func TestAPlayerLeavingSeatedTakesTheCushion(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Sitter")
	srv.SetBlock(3, 63, 3, registry.BlockStone.DefaultState())
	srv.GiveSlot("Sitter", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemRedCushion), Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	id := placeRedCushion(c).EntityID
	useCushion(c, id, false)
	if riders := servertest.AwaitPassengersFor(c, id); len(riders) != 1 {
		t.Fatalf("the cushion's passengers = %v, want the sitter", riders)
	}
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)
	c.Disconnect()

	servertest.UntilTicks(t, srv, 20, "the cushion gone with the sitter", func() bool { return srv.FixtureCount() == 0 })
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("item entities = %d after leaving, want no drop", n)
	}
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Sitter"))
	saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle")
	if !ok {
		t.Fatal("no RootVehicle in the file of a sitter who left")
	}
	tag, _ := nbt.Get[nbt.Compound](saved, "Entity")
	if got, _ := nbt.Get[nbt.String](tag, "id"); got != "minecraft:cushion" {
		t.Errorf("RootVehicle entity = %q, want the cushion", got)
	}
	if got, _ := nbt.Get[nbt.String](tag, "color"); got != "red" {
		t.Errorf("RootVehicle cushion color = %q, want red", got)
	}
	attach, _ := nbt.Get[nbt.IntArray](saved, "Attach")
	if ids, _ := nbt.Get[nbt.IntArray](tag, "UUID"); fmt.Sprint(attach) != fmt.Sprint(ids) {
		t.Errorf("Attach = %v, want the cushion's UUID %v", attach, ids)
	}

	back := connect().Join("Sitter")
	seat := back.AwaitSpawn(cushionType).EntityID
	if riders := servertest.AwaitPassengersFor(back, seat); len(riders) != 1 || riders[0] != back.PlayerEntityID {
		t.Fatalf("the restored cushion's passengers = %v, want the sitter %d", riders, back.PlayerEntityID)
	}
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)
	if n := srv.FixtureCount(); n != 1 {
		t.Errorf("fixtures after rejoining = %d, want the one cushion", n)
	}
}
