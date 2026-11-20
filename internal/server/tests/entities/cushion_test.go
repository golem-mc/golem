package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
// on: the survives poll, on the 101st tick of the cushion's life, breaks it
// and lays its item. The loop is held from before the placement: on a free
// loop the placement can land after the tick's cushion pass, and the poll
// then falls on the last tick end a 101-tick wait counts, so one tick of
// skew fails it.
// vanilla: BlockAttachedEntity.tick, Cushion.wouldSurviveAt
func TestACushionFallsWithTheBlockUnderIt(t *testing.T) {
	t.Parallel()
	srv, c := joinCushionPlacer(t, 1)
	servertest.HoldTicks(t, srv)
	placeRedCushion(c)

	if !srv.SetBlock(3, 63, 3, 0) {
		t.Fatal("the stone under the cushion was not removed")
	}
	servertest.RunTicks(t, srv, 100)
	if n := srv.FixtureCount(); n != 1 {
		t.Fatalf("fixtures after 100 ticks = %d, want the cushion until its poll", n)
	}
	servertest.RunTicks(t, srv, 1)
	if n := srv.FixtureCount(); n != 0 {
		t.Fatalf("fixtures after the 101st tick = %d, want the unsupported cushion gone", n)
	}
	awaitRedCushionDrop(t, srv, 1)
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

	back := connect().JoinKeeping("Sitter")
	seat := back.AwaitSpawn(cushionType).EntityID
	if riders := servertest.AwaitPassengersFor(back, seat); len(riders) != 1 || riders[0] != back.PlayerEntityID {
		t.Fatalf("the restored cushion's passengers = %v, want the sitter %d", riders, back.PlayerEntityID)
	}
	awaitSeatY(t, srv, "Sitter", 64+0.25-0.6)
	if n := srv.FixtureCount(); n != 1 {
		t.Errorf("fixtures after rejoining = %d, want the one cushion", n)
	}
}
