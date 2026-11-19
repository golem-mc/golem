package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

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
