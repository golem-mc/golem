package blocks

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The bed and door tests: the two blocks that stand in pairs, where a
// placement has to mind a second cell and a break of either half has to
// take the other with it and drop exactly one item, however the pair came
// apart. The placement tests go through the wire the way a client places,
// the break and use tests arrange their worlds with SetBlock so that what
// they check is the pair's own behaviour.

// doorStateNames looks a door state up by its facing, half, and open-ness.
func doorStateName(facing, half, open string) uint16 {
	id, ok := registry.FindState("minecraft:oak_door", map[string]string{
		"facing": facing,
		"half":   half,
		"open":   open,
	})
	if !ok {
		panic("no such door state")
	}
	return id
}

// TestBedPlacementFacesTheLookDirectionAndStandsItsHead places a bed looking
// south and then west: the foot lands against the face clicked, the head one
// block further the way the player looks, and both halves face that way.
func TestBedPlacementFacesTheLookDirectionAndStandsItsHead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:red_bed", 1))

	// Yaw 0 looks south, along +Z: the head stands at Z+1.
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:red_bed", map[string]string{
		"part": "foot", "facing": "south", "occupied": "false",
	})
	servertest.ExpectBlockNamed(t, srv, 5, 64, 1, "minecraft:red_bed", map[string]string{
		"part": "head", "facing": "south", "occupied": "false",
	})

	// Yaw 90 looks west, along -X: the head stands at X-1.
	c.FacePlayer(0.5, 64, 0.5, 90, 0)
	c.PlaceBlock(0, 63, 5, protocol.FaceTop)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 0, 64, 5, "minecraft:red_bed", map[string]string{
		"part": "foot", "facing": "west", "occupied": "false",
	})
	servertest.ExpectBlockNamed(t, srv, -1, 64, 5, "minecraft:red_bed", map[string]string{
		"part": "head", "facing": "west", "occupied": "false",
	})
}

// TestBedPlacementIsRefusedWhenTheHeadCellIsTaken puts a block where the bed's
// head would stand and checks the placement does not happen: the foot cell is
// left as it was.
func TestBedPlacementIsRefusedWhenTheHeadCellIsTaken(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(5, 64, 1, stone) {
		t.Fatal("could not place the blocking stone")
	}

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:red_bed", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()

	if state := srv.BlockAt(5, 64, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("bed foot was placed over a taken head cell: (%d,%d,%d) holds %q", 5, 64, 0, name)
	}
}

// TestSurvivalBreakingBedFootLeavesExactlyOneItem stands a bed, breaks its
// foot, and checks the pop of the head that follows drops the bed's one item:
// the foot's own loot does not apply and the head's does.
func TestSurvivalBreakingBedFootLeavesExactlyOneItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}

	// The break lands in a tick, and its drop spawns after the updates go
	// out, so the ticks are held and read only once the head has gone. A
	// grounded hand takes six ticks to the bed.
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	c.BreakBlock(5, 64, 0)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the bed head popped", func() bool { return srv.BlockAt(5, 64, 1) == 0 })

	if state := srv.BlockAt(5, 64, 1); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the bed head is still standing after its foot broke: %q", name)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:red_bed")
}

// TestSurvivalBreakingBedHeadLeavesExactlyOneItem is the same from the other
// half: the head broken, the foot popped, one item either way.
func TestSurvivalBreakingBedHeadLeavesExactlyOneItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}

	// The break lands in a tick, and its drop spawns after the updates go
	// out, so the ticks are held and read only once the foot has gone. A
	// grounded hand takes six ticks to the bed.
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.HoldTicks(t, srv)
	c.BreakBlock(5, 64, 1)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the bed foot popped", func() bool { return srv.BlockAt(5, 64, 0) == 0 })

	if state := srv.BlockAt(5, 64, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the bed foot is still standing after its head broke: %q", name)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:red_bed")
}

// TestCreativeBreakingBedFootLeavesNothingBehind breaks a bed's foot in
// creative, where nothing drops, and checks the head did not survive the
// removal of its partner.
func TestCreativeBreakingBedFootLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}

	c.BreakBlock(5, 64, 0)
	c.Settle()

	if state := srv.BlockAt(5, 64, 1); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the bed head is still standing after its foot broke in creative: %q", name)
	}
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestSurvivalBreakingDoorHalfLeavesExactlyOneItem breaks each half of a door
// in turn and checks the whole door comes down leaving one item, whichever
// half the player hit.
func TestSurvivalBreakingDoorHalfLeavesExactlyOneItem(t *testing.T) {
	t.Parallel()
	// A bare hand is ninety ticks to the door, and the hand is the
	// tool this test's mechanics turn on - a door half broken by a tool
	// that is correct for its drops leaves its partner standing - so the
	// ticks run on the step clock rather than the wall's.
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// On the ground and in reach: the dig's pace counts the standing, and
	// the door sits five blocks out from the spawn.
	c.MoveToGround(0.5, 64, 0.5, true)

	if !srv.SetBlock(5, 64, 0, doorStateName("south", "lower", "false")) ||
		!srv.SetBlock(5, 65, 0, doorStateName("south", "upper", "false")) {
		t.Fatal("could not stand the door")
	}

	// The upper half: its own loot does not apply, the lower half's does.
	c.BreakBlock(5, 65, 0)
	// A hand is ninety ticks to the door, which the step clock runs out in
	// a moment; the settle has the dig on the books before the steps run.
	c.Settle()
	servertest.StepTicks(t, srv, clock, 120)
	c.Settle()
	if state := srv.BlockAt(5, 64, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the door's lower half is still standing after its upper broke: %q", name)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_door")

	// And the lower half, which drops through its own loot. The first
	// item is still lying where the upper's break left it, so two stand
	// in the world by the end.
	if !srv.SetBlock(5, 64, 0, doorStateName("south", "lower", "false")) ||
		!srv.SetBlock(5, 65, 0, doorStateName("south", "upper", "false")) {
		t.Fatal("could not stand the door again")
	}
	c.BreakBlock(5, 64, 0)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 120)
	c.Settle()
	if state := srv.BlockAt(5, 65, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the door's upper half is still standing after its lower broke: %q", name)
	}
	// Two doors in all: the two drops lie close enough to merge into one
	// stack of two, so the count is of items, not of entities.
	items := srv.ItemsInDimension(world.DimensionName)
	doors := 0
	for _, it := range items {
		if it.Item != "minecraft:oak_door" {
			t.Errorf("the world holds a %q, want only oak doors", it.Item)
		}
		doors += int(it.Count)
	}
	if doors != 2 {
		t.Fatalf("the world holds %d oak doors after both halves broke, want 2: %v", doors, items)
	}
}

// TestDoorBreaksWhenItsSupportGoes breaks the block a door stands on and
// checks the whole door comes down leaving its item behind, the way a door
// does when the world takes the ground from under it.
func TestDoorBreaksWhenItsSupportGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, doorStateName("south", "lower", "false")) ||
		!srv.SetBlock(5, 65, 0, doorStateName("south", "upper", "false")) {
		t.Fatal("could not stand the door")
	}

	// The grass the door stands on.
	c.BreakBlock(5, 63, 0)
	c.Settle()

	if state := srv.BlockAt(5, 64, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the door's lower half is still standing with nothing under it: %q", name)
	}
	if state := srv.BlockAt(5, 65, 0); state != 0 {
		name, _ := servertest.StateProps(state)
		t.Fatalf("the door's upper half is still standing with nothing under it: %q", name)
	}
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:oak_door")
}

// TestDoorHandToggleOpensBothHalves right-clicks a door and checks both
// halves swing together, the way the server has always toggled them.
func TestDoorHandToggleOpensBothHalves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, doorStateName("south", "lower", "false")) ||
		!srv.SetBlock(5, 65, 0, doorStateName("south", "upper", "false")) {
		t.Fatal("could not stand the door")
	}

	c.UseBlock(5, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"open": "true"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:oak_door", map[string]string{"open": "true"})

	c.UseBlock(5, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"open": "false"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:oak_door", map[string]string{"open": "false"})
}

// TestDoorHandUseSoundSkipsTheUser opens a door by hand with two clients
// watching: the other player hears the swing once and the user hears nothing,
// because the user's own client predicted the sound and vanilla's
// DoorBlock.playSound passes the using player, whom Level.playSound skips.
func TestDoorHandUseSoundSkipsTheUser(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	if !srv.SetBlock(5, 64, 0, doorStateName("south", "lower", "false")) ||
		!srv.SetBlock(5, 65, 0, doorStateName("south", "upper", "false")) {
		t.Fatal("could not stand the door")
	}

	open, ok := registry.SoundEventID("minecraft:block.wooden_door.open")
	if !ok {
		t.Fatal("no sound event minecraft:block.wooden_door.open")
	}

	actor.UseBlock(5, 64, 0)
	actor.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySoundEffect {
			t.Error("the user heard the door sound echoed back, but the client predicts it already")
		}
	})

	// The actor's barrier follows the synchronous sound broadcast, so the
	// watcher's sound is queued before its own barrier.
	sounds := 0
	count := func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			sounds++
			if got := protocol.NewReader(data).ReadVarInt() - 1; got != open {
				t.Errorf("door sound = %d, want %d (minecraft:block.wooden_door.open)", got, open)
			}
		}
	}
	watcher.SettleAndCollect(count)
	if sounds != 1 {
		t.Errorf("the other player heard %d door sounds, want exactly 1", sounds)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestBedUseByDaySetsTheRespawnPointAndSaysSo right-clicks a bed in the
// daytime, where sleeping is refused but the spawn point is still set: the
// message that the spawn was set comes first and the one that night has not
// fallen second.
func TestBedUseByDaySetsTheRespawnPointAndSaysSo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	srv.SetDayTime(6000)

	c.UseBlock(5, 64, 0)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)

	spawn, _, ok := srv.SpawnPointOf("Actor")
	if !ok {
		t.Fatal("the bed did not set a spawn point")
	}
	if spawn.X != 5 || spawn.Y != 64 || spawn.Z != 1 {
		t.Errorf("spawn point is (%d,%d,%d), want the bed's head (5,64,1)", spawn.X, spawn.Y, spawn.Z)
	}
}

// TestBedClickReplacesASpawnAnotherDimensionNames checks that a bed clicked
// at the cells a spawn point another dimension names still tells of the new
// spawn: the bed's point is this dimension's, so the two are not one spawn,
// however alike their cells.
func TestBedClickReplacesASpawnAnotherDimensionNames(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	c.ChatCommand("execute in minecraft:the_nether run spawnpoint @s 5 64 1")
	cx, cy, cz := int64(5), int64(64), int64(1)
	cyaw, cpitch := float32(0), float32(0)
	servertest.AwaitCommandFeedbackText(t, c, "commands.spawnpoint.success.single",
		protocol.Text{Number: &cx}, protocol.Text{Number: &cy}, protocol.Text{Number: &cz},
		protocol.Text{Float: &cyaw}, protocol.Text{Float: &cpitch},
		protocol.Text{Text: "minecraft:the_nether"}, servertest.PlayerDisplayComponent("Actor"))
	srv.SetDayTime(6000)

	c.UseBlock(5, 64, 0)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)
}

// TestBedUseAtNightSetsTheRespawnPointWithoutRefusing stands a bed at night,
// where sleeping would be allowed, and checks the click sets the spawn point,
// puts the body down, and says the two things that makes: the spawn point's
// tell and the night the sleep will pass.
func TestBedUseAtNightSetsTheRespawnPointWithoutRefusing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	srv.SetDayTime(18000)

	c.UseBlock(5, 64, 0)
	// The count runs to the counting ping's answer and a little past it,
	// so the click's messages are caught however their ride out races
	// the ping's answer. The spawn point's tell and the sleep's own: a
	// click at night puts the body down, and the sleep begun out of
	// nobody lying is one the recount tells of — the night that will
	// pass, the sleeper being enough on their own.
	chats := c.SystemChatsBeforeSettle()
	if len(chats) != 2 {
		t.Fatalf("the night click produced %d system chats, want the spawn one and the sleep's", len(chats))
	}
	if want := servertest.SystemChatPayload("block.minecraft.set_spawn", false); !bytes.Equal(chats[0], want) {
		t.Fatalf("night click message is % X, want the set-spawn one % X", chats[0], want)
	}
	if want := servertest.SystemChatPayload("sleep.skipping_night", true); !bytes.Equal(chats[1], want) {
		t.Fatalf("night click message is % X, want the night-passing one % X", chats[1], want)
	}

	spawn, _, ok := srv.SpawnPointOf("Actor")
	if !ok {
		t.Fatal("the bed did not set a spawn point")
	}
	if spawn.X != 5 || spawn.Y != 64 || spawn.Z != 1 {
		t.Errorf("spawn point is (%d,%d,%d), want the bed's head (5,64,1)", spawn.X, spawn.Y, spawn.Z)
	}
}

// TestBedCeilingsBlockSleepOnlyWhenTheySuffocate lays a ceiling over both
// halves of a bed and clicks it at night: a glass roof and a top slab leave
// the body free to sleep, while a stone one answers the obstructed refusal.
// The ceiling sits at the cell above each half, the space vanilla's
// ServerPlayer.bedBlocked asks freeAt about.
func TestBedCeilingsBlockSleepOnlyWhenTheySuffocate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		ceiling    uint16
		obstructed bool
	}{
		{"glass", servertest.MustState(t, "minecraft:glass", nil), false},
		{"top stone slab", servertest.MustState(t, "minecraft:stone_slab", map[string]string{"type": "top"}), false},
		{"stone", servertest.MustState(t, "minecraft:stone", nil), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)

			srv.StopTick()
			servertest.StandSleepBed(t, srv)
			servertest.SetNight(t, srv)
			if !srv.SetBlock(servertest.SleepBedHeadX, servertest.SleepBedHeadY+1, servertest.SleepBedHeadZ, tc.ceiling) ||
				!srv.SetBlock(servertest.SleepBedHeadX, servertest.SleepBedHeadY+1, servertest.SleepBedFootZ, tc.ceiling) {
				t.Fatal("could not lay the ceiling")
			}
			c.MoveTo(6.5, 64, 0.5, 0, 0)
			c.Settle()

			c.UseBlock(servertest.SleepBedHeadX, servertest.SleepBedHeadY, servertest.SleepBedHeadZ)
			chats := c.SystemChatsBeforeSettle()
			obstructed := false
			for _, chat := range chats {
				if bytes.Contains(chat, []byte("block.minecraft.bed.obstructed")) {
					obstructed = true
				}
			}
			if obstructed != tc.obstructed {
				t.Errorf("a bed under %s answered obstructed=%v, want %v (chats %q)",
					tc.name, obstructed, tc.obstructed, chats)
			}
			if got, want := srv.Sleeping("Actor"), !tc.obstructed; got != want {
				t.Errorf("a bed under %s left the sleeper lying=%v, want %v", tc.name, got, want)
			}
		})
	}
}

// TestBedUseInTheNetherExplodes stands a bed in the nether, where beds are
// not for sleeping, and checks clicking one takes the bed apart and the
// ground around it with it.
func TestBedUseInTheNetherExplodes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	srv.MovePlayerToDimension("Actor", world.NetherName, 85, 40, 85)
	// Wait for the arrival: the SetBlock and useBlock below need the player in
	// the nether with the bed's chunk loaded.
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Actor") == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, 88, 88)
	}) {
		t.Fatal("the nether never arrived for the actor")
	}

	if !srv.SetBlockInDimension(world.NetherName, 88, 40, 88, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlockInDimension(world.NetherName, 88, 40, 89, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed in the nether")
	}

	c.UseBlock(88, 40, 88)
	c.Settle()

	for _, pos := range [][3]int32{{88, 40, 88}, {88, 40, 89}, {87, 40, 89}} {
		if state := srv.BlockInDimension(world.NetherName, pos[0], pos[1], pos[2]); state != 0 {
			name, _ := servertest.StateProps(state)
			t.Errorf("nether block at (%d,%d,%d) survived the bed: %q", pos[0], pos[1], pos[2], name)
		}
	}
}

// netherFires counts the fire in the box and fails for any fire that stands
// on something other than a solid block; the box is the room the blast
// cleared above its floor.
func netherFires(t *testing.T, srv *server.Server, x0, x1, y, z0, z1 int32) int {
	t.Helper()
	fire, ok := registry.FindState("minecraft:fire", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:fire")
	}
	fireName, _ := servertest.StateProps(fire)
	n := 0
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			name, _ := servertest.StateProps(srv.BlockInDimension(world.NetherName, x, y, z))
			if name != fireName {
				continue
			}
			n++
			if below := srv.BlockInDimension(world.NetherName, x, y-1, z); below == 0 {
				t.Errorf("fire at (%d,%d,%d) stands over air", x, y, z)
			}
		}
	}
	return n
}

// netherRoom lays a netherrack floor at y=39 over the box and clears the
// seven layers above it (y 40 to 46).
func netherRoom(t *testing.T, srv *server.Server, x0, x1, z0, z1 int32) {
	t.Helper()
	netherrack, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:netherrack")
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			srv.SetBlockInDimension(world.NetherName, x, 39, z, netherrack)
			for y := int32(40); y <= 46; y++ {
				srv.SetBlockInDimension(world.NetherName, x, y, z, 0)
			}
		}
	}
}

// TestBedUseInTheNetherLeavesFires checks a bed's blast over a netherrack
// floor lights about a third of the cleared cells that have solid ground
// below.
// vanilla: ServerExplosion.createFire
func TestBedUseInTheNetherLeavesFires(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	srv.MovePlayerToDimension("Actor", world.NetherName, 85, 40, 85)
	if !c.Pump(servertest.JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension("Actor") == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, 88, 88)
	}) {
		t.Fatal("the nether never arrived for the actor")
	}
	netherRoom(t, srv, 78, 98, 78, 98)
	if !srv.SetBlockInDimension(world.NetherName, 88, 40, 88, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlockInDimension(world.NetherName, 88, 40, 89, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed in the nether")
	}

	c.UseBlock(88, 40, 88)
	c.Settle()

	// Fires stand in the cleared ground, so the cells at the floor's top
	// and the layers over it are all counted.
	total := 0
	for y := int32(39); y <= 46; y++ {
		total += netherFires(t, srv, 78, 98, y, 78, 98)
	}
	if total < 5 {
		t.Errorf("the bed's blast left %d fires, want about a third of the cleared ground", total)
	}
}
