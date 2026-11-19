package blocks

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The respawn anchor: the glowstone that charges it, the spawn point its
// charged use sets in the nether, the blast every other dimension answers
// with, and the charge each respawn spends of it.

// anchorStateOf looks the anchor's state up by its charges.
func anchorStateOf(charges int) uint16 {
	state, ok := registry.FindState("minecraft:respawn_anchor",
		map[string]string{"charges": strconv.Itoa(charges)})
	if !ok {
		panic("no respawn anchor state with " + strconv.Itoa(charges) + " charges")
	}
	return state
}

// clearedByBlast reports whether a cell the anchor's blast cleared holds
// air or the fire the blast may set there over a solid floor.
// vanilla: ServerExplosion.createFire
func clearedByBlast(state uint16) bool {
	name, _ := servertest.StateProps(state)
	return state == 0 || name == "minecraft:fire"
}

// expectAnchorCharges checks that the anchor at the given position holds
// exactly the charges wanted, the empty dimension naming the overworld.
func expectAnchorCharges(t *testing.T, srv *server.Server, dimension string, x, y, z int32, want int) {
	t.Helper()
	state := srv.BlockAt(x, y, z)
	if dimension != "" {
		state = srv.BlockInDimension(dimension, x, y, z)
	}
	if name, props := servertest.StateProps(state); name != "minecraft:respawn_anchor" || props["charges"] != strconv.Itoa(want) {
		gotName, gotProps := name, props["charges"]
		if gotName != "minecraft:respawn_anchor" {
			t.Fatalf("block at (%d,%d,%d) is %q, want the anchor", x, y, z, gotName)
		}
		t.Errorf("anchor at (%d,%d,%d) holds %q charges, want %d", x, y, z, gotProps, want)
	}
}

// useBlockWith right-clicks the block the way a client does with the hand
// given, useBlock's form for the hand other than the main one.
func useBlockWith(c *servertest.Client, hand, x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(x, y, z)
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
}

// anchorScene moves the test's player into a room carved out of the nether
// — a netherrack floor with air over it, standing room for a respawn — and
// stands the anchor of the charges given in the middle of it. It reports
// the server, the client ready in the nether, the player's entity ID, and
// the anchor's position.
func anchorScene(t *testing.T, name string, charges int) (*server.Server, *servertest.Client, int32, [3]int32) {
	t.Helper()
	// The smallest view keeps each dimension crossing to a few chunks, so
	// the arrival does not depend on how fast the machine generates them.
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c, entityID := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))

	// Creative while the nether's chunks arrive: the arrival cell can be
	// solid, and a survival body suffocates there on the ticks the stream
	// takes, before the test's own death.
	srv.SetPlayerGameMode(name, 1)
	srv.MovePlayerToDimension(name, world.NetherName, 85, 40, 85)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension(name) == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, 88, 88)
	}) {
		t.Fatal("the nether never arrived around the anchor's room")
	}
	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	// The room: a floor at y=39 and air up to it through y=43, so every
	// cell a respawn's stand-up search walks is open ground the search
	// decides on rather than whatever the nether generated. The cells the
	// nether generated air in already take no write, so the room is read
	// back rather than trusting each write's word for it.
	for x := int32(86); x <= 90; x++ {
		for z := int32(86); z <= 90; z++ {
			srv.SetBlockInDimension(world.NetherName, x, 39, z, netherrack)
			for y := int32(40); y <= 43; y++ {
				srv.SetBlockInDimension(world.NetherName, x, y, z, 0)
			}
		}
	}
	for _, cell := range [][3]int32{{88, 39, 88}, {88, 40, 88}, {88, 43, 88}, {88, 41, 87}} {
		want := netherrack
		if cell[1] > 39 {
			want = 0
		}
		if got := srv.BlockInDimension(world.NetherName, cell[0], cell[1], cell[2]); got != want {
			t.Fatalf("the anchor's room came out wrong at (%d,%d,%d): %d, want %d",
				cell[0], cell[1], cell[2], got, want)
		}
	}
	if !srv.SetBlockInDimension(world.NetherName, 88, 40, 88, anchorStateOf(charges)) {
		t.Fatal("could not stand the anchor")
	}
	c.StandAt(85.5, 40, 85.5, 0)
	srv.SetPlayerGameMode(name, 0)
	return srv, c, entityID, [3]int32{88, 40, 88}
}

// expectAnchorDeplete reads the connection for the sound a respawn's
// anchor point ends with — the spent charge's, played to the fresh body
// alone — checking it lands at the block given.
func expectAnchorDeplete(c *servertest.Client, x, y, z int32) {
	c.T.Helper()
	soundID, ok := registry.SoundEventID("minecraft:block.respawn_anchor.deplete")
	if !ok {
		c.T.Fatal("the sound registry has no respawn anchor deplete")
	}
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != soundID+1 || r.ReadVarInt() != protocol.SoundCategoryBlock {
			return false
		}
		if r.ReadI32() != x*8 || r.ReadI32() != y*8 || r.ReadI32() != z*8 {
			return false
		}
		volume, pitch := r.ReadF32(), r.ReadF32()
		return volume == 1 && pitch == 1
	}) {
		c.T.Fatal("the anchor's spent charge never sounded for the fresh body")
	}
}

// TestGlowstoneChargesARespawnAnchorUntilItIsFull walks an anchor through
// its whole charge in the overworld — the charging knows no dimension —
// and the click that finds it full: there the block's own use answers,
// and outside the nether that answer is the blast. The empty anchor a
// bare hand finds answers nothing at all.
// vanilla: RespawnAnchorBlock.useItemOn, useWithoutItem, charge
func TestGlowstoneChargesARespawnAnchorUntilItIsFull(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner")

	if !srv.SetBlock(5, 64, 0, anchorStateOf(0)) {
		t.Fatal("could not stand the anchor")
	}
	// Five blocks off, at the edge of the anchor's reach, so the blast the
	// fifth click raises lands nothing on the clicker and the refused
	// glowstone stays where the hand holds it.
	c.StandAt(0.5, 64, 0.5, 0)

	// A bare click on the empty anchor does nothing: no blast, no charge,
	// no spawn point.
	c.UseBlock(5, 64, 0)
	c.Settle()
	expectAnchorCharges(t, srv, "", 5, 64, 0, 0)
	if _, _, ok := srv.SpawnPointOf("Miner"); ok {
		t.Error("a bare click on the empty anchor set a spawn point")
	}

	// Four glowstone charge it one charge each, spending one each.
	srv.GiveSlot("Miner", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"), Count: 5})
	for i := 1; i <= 4; i++ {
		c.PlaceBlock(5, 64, 0, protocol.FaceTop)
		c.Settle()
		expectAnchorCharges(t, srv, "", 5, 64, 0, i)
		if got := srv.InventorySlot("Miner", player.HotbarSlot); got.Count != int32(5-i) {
			t.Errorf("after the %dth charge the glowstone count = %d, want %d", i, got.Count, 5-i)
		}
	}

	// The fifth glowstone is refused and the click falls to the block's
	// own use, which outside the nether is the blast: the anchor goes
	// first and the ground around it with it, the refused glowstone whole.
	c.PlaceBlock(5, 64, 0, protocol.FaceTop)
	c.Settle()
	for _, pos := range [][3]int32{{5, 64, 0}, {5, 63, 1}} {
		if state := srv.BlockAt(pos[0], pos[1], pos[2]); !clearedByBlast(state) {
			name, _ := servertest.StateProps(state)
			t.Errorf("block at (%d,%d,%d) survived the anchor: %q", pos[0], pos[1], pos[2], name)
		}
	}
	if got := srv.InventorySlot("Miner", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the refused glowstone's count = %d, want 1", got.Count)
	}
}

// TestAFullAnchorInTheNetherRefusesAGlowstone checks the way a full anchor
// refuses a glowstone in the nether, the dimension it works in: no charge
// is added, none of the glowstone spent, and the click falls to the
// block's own use, which there sets the spawn point.
// vanilla: RespawnAnchorBlock.useItemOn, useWithoutItem
func TestAFullAnchorInTheNetherRefusesAGlowstone(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Anchorer", 4)
	srv.GiveSlot("Anchorer", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"), Count: 1})

	c.PlaceBlock(at[0], at[1], at[2], protocol.FaceTop)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 4)
	if got := srv.InventorySlot("Anchorer", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the refused glowstone's count = %d, want 1", got.Count)
	}
	if spawn, forced, ok := srv.SpawnPointOf("Anchorer"); !ok || forced ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("spawn point after the click = (%d,%d,%d) forced %v, %v; want the anchor's cell, unforced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
}

// TestTheNetherAnchorSetsItsSpawnPoint charges an anchor in the nether and
// clicks it bare-handed: the spawn point it sets is the anchor's own cell,
// and no charge goes with it — the charge is spent by respawning alone. A
// second click sets the same point and so tells nothing.
// vanilla: RespawnAnchorBlock.useWithoutItem, ServerPlayer.setRespawnPosition
func TestTheNetherAnchorSetsItsSpawnPoint(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Sleeper", 2)

	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()
	if spawn, forced, ok := srv.SpawnPointOf("Sleeper"); !ok || forced ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("spawn point after the click = (%d,%d,%d) forced %v, %v; want the anchor's cell, unforced, true",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 2)

	// The point did not move, so the second click tells neither the chat
	// nor anything else.
	c.UseBlock(at[0], at[1], at[2])
	if chats := c.SystemChatsBeforeSettle(); len(chats) != 0 {
		t.Errorf("the second click produced %d system chats, want none: the point did not move", len(chats))
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 2)
}

// TestTheOffHandGlowstoneChargesOnItsOwnClick pins the way the anchor
// holds a click back for the other hand's glowstone: an empty main hand
// whose off hand holds one passes the anchor without the anchor's own use
// — no spawn point in the nether either — and the off hand's own click
// then charges it, spending one of its glowstone.
// vanilla: RespawnAnchorBlock.useItemOn
func TestTheOffHandGlowstoneChargesOnItsOwnClick(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Holder", 0)
	srv.GiveSlot("Holder", player.OffHandSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"), Count: 2})

	// The main hand's click charges nothing and sets nothing: the anchor
	// holds it back for the other hand's glowstone.
	c.UseBlock(at[0], at[1], at[2])
	c.Settle()
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 0)
	if _, _, ok := srv.SpawnPointOf("Holder"); ok {
		t.Error("the empty main hand's click set a spawn point")
	}

	// The off hand's click charges from the off hand.
	useBlockWith(c, protocol.HandOff, at[0], at[1], at[2])
	c.Settle()
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 1)
	if got := srv.InventorySlot("Holder", player.OffHandSlot); got.Count != 1 {
		t.Errorf("the off hand's glowstone count = %d, want 1", got.Count)
	}
}

// TestAChargedAnchorExplodesInTheEnd checks the other dimension the anchor
// sets no spawn point in: a charged one's use there is the same blast the
// overworld's is, and the obsidian it stood on takes no harm.
// vanilla: RespawnAnchorBlock.useWithoutItem
func TestAChargedAnchorExplodesInTheEnd(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Ender")

	arrival := world.EndArrival()
	ax, ay, az := arrival.X+2, arrival.Y, arrival.Z
	srv.MovePlayerToDimension("Ender", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Ender") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, ax, az)
	}) {
		t.Fatal("the end never arrived around the anchor")
	}
	if !srv.SetBlockInDimension(world.EndName, ax, ay, az, anchorStateOf(3)) {
		t.Fatal("could not stand the anchor on the platform")
	}
	obsidian, ok := registry.FindState("minecraft:obsidian", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:obsidian")
	}

	c.UseBlock(ax, ay, az)
	c.Settle()
	if got := srv.BlockInDimension(world.EndName, ax, ay, az); !clearedByBlast(got) {
		name, _ := servertest.StateProps(got)
		t.Errorf("the anchor at (%d,%d,%d) survived its use: %q", ax, ay, az, name)
	}
	if got := srv.BlockInDimension(world.EndName, ax, ay-1, az); got != obsidian {
		name, _ := servertest.StateProps(got)
		t.Errorf("the platform under the anchor at (%d,%d,%d) = %q, want obsidian", ax, ay-1, az, name)
	}
}

// TestANetherAnchorRespawnSpendsItsCharge charges an anchor in the nether,
// sets its spawn point and walks the deaths through: each respawn stands
// the body beside the anchor, in the nether, spending one charge; the
// anchor out of charges refuses the point, and the respawn falls back to
// the world spawn, telling the client no respawn block is there and
// dropping the point. A spawnpoint set on the empty anchor returns
// without a charge to spend, and the fourth respawn also crosses the body
// home from an overworld death.
// vanilla: ServerPlayer.findRespawnPositionAndUseSpawnBlock, PlayerList.respawn
func TestANetherAnchorRespawnSpendsItsCharge(t *testing.T) {
	t.Parallel()
	srv, c, entityID, at := anchorScene(t, "Wanderer", 0)
	// The room's chunk stays held through the phases that leave the nether,
	// so the anchor at the middle of it stays loaded for the reads.
	release := srv.HoldChunkInDimension(world.NetherName, at[0], at[2])
	defer release()
	beside := player.Position{X: 88.5, Y: 40, Z: 87.5}
	srv.GiveSlot("Wanderer", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"), Count: 2})
	for i := 1; i <= 2; i++ {
		c.PlaceBlock(at[0], at[1], at[2], protocol.FaceTop)
		c.Settle()
		expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], i)
	}
	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	// The first death returns beside the anchor, one charge the poorer,
	// with the spent charge's own sound behind the respawn.
	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if noBlock {
		t.Error("the first respawn said no respawn block")
	}
	if pos.X != beside.X || pos.Y != beside.Y || pos.Z != beside.Z {
		t.Errorf("the first respawn = (%v, %v, %v), want the cell beside the anchor (%v, %v, %v)",
			pos.X, pos.Y, pos.Z, beside.X, beside.Y, beside.Z)
	}
	if pos.Yaw != 0 {
		t.Errorf("the first respawn faces yaw %v, want 0, the anchor", pos.Yaw)
	}
	if got := srv.PlayerDimension("Wanderer"); got != world.NetherName {
		t.Errorf("the respawn left the body in %s, want the nether", got)
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 1)
	expectAnchorDeplete(c, at[0], at[1], at[2])
	c.MarkLoaded()

	// The second spends the last charge and keeps the point.
	pos, noBlock = servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if noBlock {
		t.Error("the second respawn said no respawn block")
	}
	if pos.X != beside.X || pos.Z != beside.Z {
		t.Errorf("the second respawn = (%v, %v, %v), want the cell beside the anchor",
			pos.X, pos.Y, pos.Z)
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 0)
	if spawn, _, ok := srv.SpawnPointOf("Wanderer"); !ok ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("the point after the second respawn = (%d,%d,%d), %v; want the anchor's cell, kept",
			spawn.X, spawn.Y, spawn.Z, ok)
	}
	c.MarkLoaded()

	// The third finds no charge to come back to: the point goes, the
	// client is told no respawn block is there, and the body falls back
	// to the world spawn in the overworld.
	pos, noBlock = servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if !noBlock {
		t.Error("the out-of-charges anchor's respawn told no missing block")
	}
	if pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("the third respawn = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	if got := srv.PlayerDimension("Wanderer"); got != world.DimensionName {
		t.Errorf("the third respawn left the body in %s, want the overworld", got)
	}
	if _, _, ok := srv.SpawnPointOf("Wanderer"); ok {
		t.Error("the refused point was kept")
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 0)
	// The crossing streamed the fresh body's health behind the teleport
	// this reading stopped at; the next death's reader would take that
	// health for its own. Drain what is left waiting.
	c.MarkLoaded()
	c.Settle()

	// A spawnpoint set on the empty anchor returns to it without a charge
	// to spend, crossing the overworld death home into the nether.
	c.ChatCommand("execute in minecraft:the_nether run spawnpoint @s 88 40 88")
	cx, cy, cz := int64(at[0]), int64(at[1]), int64(at[2])
	zero := float32(0)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &zero}, protocol.Text{Float: &zero},
		protocol.Text{Text: world.NetherName}, servertest.PlayerDisplayComponent("Wanderer"))
	pos, noBlock = servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if noBlock {
		t.Error("the forced point's respawn said no respawn block")
	}
	if pos.X != beside.X || pos.Z != beside.Z {
		t.Errorf("the fourth respawn = (%v, %v, %v), want the cell beside the anchor",
			pos.X, pos.Y, pos.Z)
	}
	if got := srv.PlayerDimension("Wanderer"); got != world.NetherName {
		t.Errorf("the fourth respawn brought the body to %s, want the nether", got)
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 0)
	if spawn, forced, ok := srv.SpawnPointOf("Wanderer"); !ok || !forced ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("the forced point after its respawn = (%d,%d,%d) forced %v, %v; want the anchor's cell, forced, kept",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	expectAnchorDeplete(c, at[0], at[1], at[2])
}

// expectNoAnchorDeplete reads the packet stream of a dimension change the
// credits' respawn runs until its chunks arrive, failing if a respawn anchor
// deplete sound comes with it: the credits spend no charge, so none sounds.
func expectNoAnchorDeplete(c *servertest.Client) {
	c.T.Helper()
	soundID, ok := registry.SoundEventID("minecraft:block.respawn_anchor.deplete")
	if !ok {
		c.T.Fatal("the sound registry has no respawn anchor deplete")
	}
	saw := false
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt() == soundID+1 {
			saw = true
		}
		return id == protocol.IDPlayChunkBatchFinished
	}) {
		c.T.Fatal("the credits' arrival never completed")
	}
	if saw {
		c.T.Error("the credits' respawn sounded the anchor deplete, want none: no charge was spent")
	}
}

// TestTheEndPortalReturnUsesANetherAnchor checks the other crossing that
// resolves a spawn point: coming home from the end at an anchor's point
// returns into the nether beside it — the credits' respawn, which keeps
// everything — never spending the charge a death would.
// vanilla: EndPortalBlock.getPortalDestination
func TestTheEndPortalReturnUsesANetherAnchor(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Wanderer", 3)
	release := srv.HoldChunkInDimension(world.NetherName, at[0], at[2])
	defer release()

	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Wanderer", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Wanderer") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, arrival.X, arrival.Z)
	}) {
		t.Fatal("the end never arrived")
	}

	// An end portal block under the body's feet holds the credits, whose
	// respawn request carries the body home to the anchor.
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, srv.EndPortalState()) {
		t.Fatal("could not stand the end portal on")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the command that ends the credits")
	}
	expectNoAnchorDeplete(c)
	servertest.WaitFor(t, srv, 400, func() bool { return srv.PlayerDimension("Wanderer") == world.NetherName })
	pos := srv.PlayerPosition("Wanderer")
	if pos.X != 88.5 || pos.Y != 40 || pos.Z != 87.5 {
		t.Errorf("the credits' respawn = (%v, %v, %v), want the cell beside the anchor (88.5, 40, 87.5)",
			pos.X, pos.Y, pos.Z)
	}
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 3)
	if spawn, _, ok := srv.SpawnPointOf("Wanderer"); !ok ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("the point after the credits' respawn = (%d,%d,%d), %v; want the anchor's cell, kept",
			spawn.X, spawn.Y, spawn.Z, ok)
	}
}

// TestCreativeChargingKeepsTheGlowstone checks the game's two other hands:
// a creative player's charge costs no glowstone — infinite materials —
// and a spectator's click neither charges the anchor nor sets it off.
// vanilla: ItemStack.consume, ServerPlayerGameMode.useItemOn
func TestCreativeChargingKeepsTheGlowstone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Creator", servertest.OpsListing(t, "Creator"))

	if !srv.SetBlock(5, 64, 0, anchorStateOf(1)) {
		t.Fatal("could not stand the anchor")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	c.SetCreativeSlot(player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:glowstone"), Count: 3})

	c.PlaceBlock(5, 64, 0, protocol.FaceTop)
	c.Settle()
	expectAnchorCharges(t, srv, "", 5, 64, 0, 2)
	if got := srv.InventorySlot("Creator", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the creative glowstone count = %d, want 3", got.Count)
	}

	// A spectator's click answers none of the anchor's uses: no blast for
	// the charged one, no charge added either.
	c.ChatCommand("gamemode spectator Creator")
	c.Settle()
	c.UseBlock(5, 64, 0)
	c.Settle()
	expectAnchorCharges(t, srv, "", 5, 64, 0, 2)
	if _, _, ok := srv.SpawnPointOf("Creator"); ok {
		t.Error("a spectator's click set a spawn point")
	}
}

// TestARespawnLoadsAnUnloadedAnchor sets a point at a charged anchor and
// walks the body out to the overworld until the anchor's chunk unloads: the
// respawn must load the chunk and find the anchor there, not read the
// unloaded cell as air and lose the point.
func TestARespawnLoadsAnUnloadedAnchor(t *testing.T) {
	t.Parallel()
	srv, c, entityID, at := anchorScene(t, "Wanderer", 2)
	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	srv.MovePlayerToDimension("Wanderer", world.DimensionName, 0.5, 64, 0.5)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Wanderer") == world.DimensionName
	}) {
		t.Fatal("the overworld never arrived")
	}
	c.Settle()
	servertest.WaitFor(t, srv, 400, func() bool {
		return !srv.ChunkLoadedInDimension(world.NetherName, at[0], at[2])
	})

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if noBlock {
		t.Error("the unloaded anchor's respawn said no respawn block")
	}
	if got := srv.PlayerDimension("Wanderer"); got != world.NetherName {
		t.Fatalf("the respawn left the body in %s, want the nether", got)
	}
	if pos.X != 88.5 || pos.Y != 40 || pos.Z != 87.5 {
		t.Errorf("the respawn = (%v, %v, %v), want the cell beside the anchor (88.5, 40, 87.5)",
			pos.X, pos.Y, pos.Z)
	}
	// The chunk the anchor stands in may unload again once the respawn gives
	// it up; hold it to read the charge the respawn spent.
	release := srv.HoldChunkInDimension(world.NetherName, at[0], at[2])
	defer release()
	expectAnchorCharges(t, srv, world.NetherName, at[0], at[1], at[2], 1)
}

// TestARemovedAnchorDropsTheSpawnPoint replaces the anchor a point names
// with netherrack and dies: neither anchor nor bed, the point is dropped,
// the client told no respawn block is there, and the body falls to the
// overworld spawn.
func TestARemovedAnchorDropsTheSpawnPoint(t *testing.T) {
	t.Parallel()
	srv, c, entityID, at := anchorScene(t, "Wanderer", 2)
	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	if !srv.SetBlockInDimension(world.NetherName, at[0], at[1], at[2], netherrack) {
		t.Fatal("could not replace the anchor")
	}

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Wanderer")
	if !noBlock {
		t.Error("the removed anchor's respawn told no missing block")
	}
	if pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("the respawn = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	if _, _, ok := srv.SpawnPointOf("Wanderer"); ok {
		t.Error("the removed anchor's point was kept")
	}
}

// TestTheEndPortalReturnDropsARemovedAnchor replaces the anchor a nether
// point names with netherrack and comes home from the end: the credits'
// respawn runs the same stand-up search a death would and tells the client
// with the no-respawn-block game event instead of silently keeping the
// point.
func TestTheEndPortalReturnDropsARemovedAnchor(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Wanderer", 3)
	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	if !srv.SetBlockInDimension(world.NetherName, at[0], at[1], at[2], netherrack) {
		t.Fatal("could not replace the anchor")
	}

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Wanderer", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Wanderer") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, arrival.X, arrival.Z)
	}) {
		t.Fatal("the end never arrived")
	}
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, srv.EndPortalState()) {
		t.Fatal("could not stand the end portal on")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	sawNoBlock := false
	see := func(id int32, data []byte) {
		if id == protocol.IDPlayGameEvent &&
			protocol.NewReader(data).ReadU8() == protocol.GameEventNoRespawnBlockAvailable {
			sawNoBlock = true
		}
	}
	c.ReadTicksSkipping(srv, 400, nil, func(id int32, data []byte) bool {
		see(id, data)
		return srv.PlayerDimension("Wanderer") == world.DimensionName
	})
	// The event is queued before the crossing, but the dimension can read
	// home before the mailbox has written it; the settle's answer comes
	// behind it.
	c.SettleAndCollect(see)
	if got := srv.PlayerDimension("Wanderer"); got != world.DimensionName {
		t.Fatalf("the credits' respawn left the body in %s, want the overworld", got)
	}
	if !sawNoBlock {
		t.Error("the end portal return told no missing block for the removed anchor")
	}
}

// TestWaterBracingTheAnchorStopsTheBlast stands water beside a charged
// anchor in the end and sets it off: water's resistance in the anchor's own
// cell kills every ray where it starts, so the stone beside it survives
// where an unbraced blast would take it.
func TestWaterBracingTheAnchorStopsTheBlast(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Ender")

	arrival := world.EndArrival()
	ax, ay, az := arrival.X+2, arrival.Y, arrival.Z
	srv.MovePlayerToDimension("Ender", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Ender") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, ax, az)
	}) {
		t.Fatal("the end never arrived around the anchor")
	}
	// Freeze the fluids so the water stays where it is put.
	srv.StopTick()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:water")
	}
	if !srv.SetBlockInDimension(world.EndName, ax, ay, az, anchorStateOf(3)) {
		t.Fatal("could not stand the anchor on the platform")
	}
	if !srv.SetBlockInDimension(world.EndName, ax+1, ay, az, water) {
		t.Fatal("could not stand the water beside the anchor")
	}
	if !srv.SetBlockInDimension(world.EndName, ax, ay, az+1, stone) {
		t.Fatal("could not stand the stone beside the anchor")
	}

	c.UseBlock(ax, ay, az)
	c.Settle()
	if got := srv.BlockInDimension(world.EndName, ax, ay, az); !clearedByBlast(got) {
		name, _ := servertest.StateProps(got)
		t.Errorf("the anchor at (%d,%d,%d) survived its use: %q", ax, ay, az, name)
	}
	if got := srv.BlockInDimension(world.EndName, ax, ay, az+1); got != stone {
		name, _ := servertest.StateProps(got)
		t.Errorf("the stone beside the flooded anchor at (%d,%d,%d) = %q, want it standing",
			ax, ay, az+1, name)
	}
}

// TestTheAnchorBracesItsBlastOnlyOnWaterThatWouldFlow pins the water the
// anchor's blast reads: a waterlogged neighbour and water of any kind above
// brace it, while a horizontal flow too thin to run on does not.
// vanilla: RespawnAnchorBlock.explode, isWaterThatWouldFlow
func TestTheAnchorBracesItsBlastOnlyOnWaterThatWouldFlow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ender")

	arrival := world.EndArrival()
	ax, ay, az := arrival.X+2, arrival.Y, arrival.Z
	srv.MovePlayerToDimension("Ender", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Ender") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, ax, az)
	}) {
		t.Fatal("the end never arrived around the anchor")
	}
	// Freeze the fluids so the water stays where it is put.
	srv.StopTick()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	slab, ok := registry.FindState("minecraft:oak_slab", map[string]string{"waterlogged": "true"})
	if !ok {
		t.Fatal("the block registry has no waterlogged oak slab")
	}
	seagrass, ok := registry.FindState("minecraft:seagrass", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:seagrass")
	}
	thin, ok := registry.FindState("minecraft:water", map[string]string{"level": "7"})
	if !ok {
		t.Fatal("the block registry has no water at level 7")
	}
	cases := []struct {
		what  string
		brace uint16
		above bool
		want  bool // the stone beside the anchor survives
	}{
		{"a waterlogged slab beside it", slab, false, true},
		{"seagrass above it", seagrass, true, true},
		{"a level-7 flow beside it", thin, false, false},
	}
	for _, tc := range cases {
		// Clear the brace of the variant before and stand a fresh anchor
		// with the stone beside it.
		for _, cell := range [][3]int32{{ax + 1, ay, az}, {ax, ay + 1, az}, {ax, ay, az + 1}} {
			srv.SetBlockInDimension(world.EndName, cell[0], cell[1], cell[2], 0)
		}
		if !srv.SetBlockInDimension(world.EndName, ax, ay, az, anchorStateOf(3)) {
			t.Fatalf("%s: could not stand the anchor", tc.what)
		}
		if !srv.SetBlockInDimension(world.EndName, ax, ay, az+1, stone) {
			t.Fatalf("%s: could not stand the stone", tc.what)
		}
		brace := [3]int32{ax + 1, ay, az}
		if tc.above {
			brace = [3]int32{ax, ay + 1, az}
		}
		srv.SetBlockInDimension(world.EndName, brace[0], brace[1], brace[2], tc.brace)

		c.UseBlock(ax, ay, az)
		c.Settle()
		if got := srv.BlockInDimension(world.EndName, ax, ay, az); !clearedByBlast(got) {
			name, _ := servertest.StateProps(got)
			t.Errorf("%s: the anchor survived its use: %q", tc.what, name)
		}
		got := srv.BlockInDimension(world.EndName, ax, ay, az+1)
		if survived := got == stone; survived != tc.want {
			name, _ := servertest.StateProps(got)
			t.Errorf("%s: the stone beside the anchor = %q, survived %v, want %v",
				tc.what, name, survived, tc.want)
		}
	}
}

// TestAChargedAnchorEmitsLight checks the anchor's light follows its charge:
// the scaled 0/3/7/11/15 a charge of none to four gives. The cells are a
// light's reach apart so no anchor lights another.
// vanilla: Blocks.RESPAWN_ANCHOR, RespawnAnchorBlock.getScaledChargeLevel
func TestAChargedAnchorEmitsLight(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Lantern")
	srv.StopTick()

	for _, tc := range []struct {
		charges int
		want    uint8
	}{{0, 0}, {1, 3}, {2, 7}, {3, 11}, {4, 15}} {
		x := int32(5 + 20*tc.charges)
		release := srv.HoldChunk(x, 0)
		if !srv.SetBlock(x, 64, 0, anchorStateOf(tc.charges)) {
			release()
			t.Fatalf("could not stand the anchor with %d charges", tc.charges)
		}
		srv.RelightAt(x, 0)
		got := srv.BlockLightAt(x, 64, 0)
		release()
		if got != tc.want {
			t.Errorf("an anchor with %d charges emits %d light, want %d", tc.charges, got, tc.want)
		}
	}
}

// TestClickingTheAnchorYouAlreadySpawnAtChangesNothing sets a forced point
// with angles at the anchor's own cell and clicks it: a point the body
// already respawns at is consumed and left alone, its forced flag and
// angles kept rather than reset to an unforced point facing nothing.
func TestClickingTheAnchorYouAlreadySpawnAtChangesNothing(t *testing.T) {
	t.Parallel()
	srv, c, _, at := anchorScene(t, "Wanderer", 2)

	c.ChatCommand(fmt.Sprintf("execute in minecraft:the_nether run spawnpoint @s %d %d %d 90 45",
		at[0], at[1], at[2]))
	cx, cy, cz := int64(at[0]), int64(at[1]), int64(at[2])
	yaw, pitch := float32(90), float32(45)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &yaw}, protocol.Text{Float: &pitch},
		protocol.Text{Text: world.NetherName}, servertest.PlayerDisplayComponent("Wanderer"))

	c.UseBlock(at[0], at[1], at[2])
	c.Settle()
	if spawn, forced, ok := srv.SpawnPointOf("Wanderer"); !ok || !forced ||
		spawn != (world.SpawnPos{X: at[0], Y: at[1], Z: at[2]}) {
		t.Errorf("the point after the click = (%d,%d,%d) forced %v, %v; want the anchor's cell, forced, kept",
			spawn.X, spawn.Y, spawn.Z, forced, ok)
	}
	if gotYaw, gotPitch := srv.SpawnPointAnglesOf("Wanderer"); gotYaw != 90 || gotPitch != 45 {
		t.Errorf("the point's angles after the click = %v, %v, want 90, 45", gotYaw, gotPitch)
	}
}

// TestAnAnchorBlastLeavesFiresOnTheEndPlatform checks the blast of a charged
// anchor in the End lights some of the cleared cells over the obsidian
// platform, each on solid ground.
// vanilla: ServerExplosion.createFire
func TestAnAnchorBlastLeavesFiresOnTheEndPlatform(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Ender")

	arrival := world.EndArrival()
	ax, ay, az := arrival.X+2, arrival.Y, arrival.Z
	srv.MovePlayerToDimension("Ender", world.EndName,
		float64(arrival.X), float64(arrival.Y), float64(arrival.Z))
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Ender") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, ax, az)
	}) {
		t.Fatal("the end never arrived around the anchor")
	}
	if !srv.SetBlockInDimension(world.EndName, ax, ay, az, anchorStateOf(3)) {
		t.Fatal("could not stand the anchor on the platform")
	}
	c.UseBlock(ax, ay, az)
	c.Settle()

	fire, ok := registry.FindState("minecraft:fire", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:fire")
	}
	fireName, _ := servertest.StateProps(fire)
	n := 0
	for x := ax - 4; x <= ax+4; x++ {
		for z := az - 4; z <= az+4; z++ {
			name, _ := servertest.StateProps(srv.BlockInDimension(world.EndName, x, ay, z))
			if name == fireName {
				n++
				if srv.BlockInDimension(world.EndName, x, ay-1, z) == 0 {
					t.Errorf("fire at (%d,%d,%d) stands over air", x, ay, z)
				}
			}
		}
	}
	if n == 0 {
		t.Error("the anchor's blast left no fire on the platform")
	}
}

// TestAnAnchorRespawnAvoidsAMagmaFloor checks that the stand-up search
// passes over the first cell, north of the anchor, while it stands on a
// magma block, and returns the body to the next cell, west.
// vanilla: RespawnAnchorBlock.findStandUpPosition, DismountHelper.findSafeDismountLocation
func TestAnAnchorRespawnAvoidsAMagmaFloor(t *testing.T) {
	t.Parallel()
	srv, c, entityID, at := anchorScene(t, "Magma", 4)
	release := srv.HoldChunkInDimension(world.NetherName, at[0], at[2])
	defer release()
	magma, ok := registry.FindState("minecraft:magma_block", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:magma_block")
	}
	if !srv.SetBlockInDimension(world.NetherName, at[0], at[1]-1, at[2]-1, magma) {
		t.Fatal("could not lay the magma floor north of the anchor")
	}
	c.UseBlock(at[0], at[1], at[2])
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.Settle()

	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Magma")
	if noBlock {
		t.Error("the respawn said no respawn block")
	}
	wantX, wantZ := float64(at[0])-0.5, float64(at[2])+0.5
	if pos.X != wantX || pos.Y != float64(at[1]) || pos.Z != wantZ {
		t.Errorf("the respawn = (%v, %v, %v), want the cell west of the anchor (%v, %v, %v)",
			pos.X, pos.Y, pos.Z, wantX, at[1], wantZ)
	}
}
