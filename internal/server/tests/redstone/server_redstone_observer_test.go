package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// The observer a redstone-driven state change reaches: a block the signal
// rewrites — a lamp, a door, a trapdoor, a fence gate, a hopper, a bell, a
// note block, a copper bulb or a crafter — is written through a setBlock
// that leaves vanilla's flag 16 out, and the neighbour shape pass such a
// write runs (Level.setBlock) is what tells an observer watching one of its
// faces. Every pulse is checked at each tick, so its two-game-tick width is
// part of the answer, and the wire and lamp behind the observer prove the
// pulse carried a signal. The torch adds the tick-written case: its lit
// flip's flag-3 setBlock reaches an observer the same way, two ticks out.
// vanilla: RedstoneLampBlock, DoorBlock, TrapDoorBlock, FenceGateBlock,
// HopperBlock, BellBlock, NoteBlock, CopperBulbBlock, CrafterBlock,
// RedstoneTorchBlock; Level.setBlock.

// witnessObserverScene stands the block under test at (0,64,4), an observer
// at (0,64,3) facing south to watch it, and the observer's output wire at
// (0,64,2) running to a lamp at (0,64,1). The power cell (0,64,5) beside the
// witness is each edge's to write. The observer goes in last, and the
// placement wakes must leave it dark.
func witnessObserverScene(t *testing.T, block, upper string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 0, 64, 4, block)
	if upper != "" {
		servertest.Setblock(t, c, 0, 65, 4, upper)
	}
	servertest.Setblock(t, c, 0, 64, 2, "redstone_wire[north=side,south=side]")
	servertest.Setblock(t, c, 0, 64, 1, "redstone_lamp")
	servertest.Setblock(t, c, 0, 64, 3, "observer[facing=south]")
	c.Settle()

	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 0, 64, 3); powered {
		t.Fatal("setup: the observer was already pulsing")
	}
	return srv, c
}

// followWitnessEdge writes the power cell at (x, y, z) and steps the world
// tick by tick, counting the ticks the observer showed powered and reporting
// whether the lamp behind it was seen lit and later dark.
func followWitnessEdge(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z int32, cell string) (pulseTicks int, lampOn, lampOff bool) {
	t.Helper()
	servertest.Setblock(t, c, x, y, z, cell)
	for range 14 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 0, 64, 3); powered {
			pulseTicks++
		}
		if servertest.LampLitAt(srv, 0, 64, 1) {
			lampOn = true
		} else if lampOn {
			lampOff = true
		}
	}
	return pulseTicks, lampOn, lampOff
}

// TestRedstoneDrivenBlocksPulseTheObserverWatchingThem powers and unpowers
// each redstone-driven block through the cell beside it: the state change on
// each edge pulses the observer watching it for two game ticks, and the lamp
// behind the observer lights and goes dark on each pulse.
func TestRedstoneDrivenBlocksPulseTheObserverWatchingThem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, block, upper string }{
		{"redstone lamp", "redstone_lamp", ""},
		{"oak door", "oak_door[half=lower,facing=north,hinge=left,open=false,powered=false]",
			"oak_door[half=upper,facing=north,hinge=left,open=false,powered=false]"},
		{"oak trapdoor", "oak_trapdoor[facing=north,half=bottom,open=false,powered=false]", ""},
		{"oak fence gate", "oak_fence_gate[facing=north,in_wall=false,open=false,powered=false]", ""},
		{"hopper", "hopper[facing=down,enabled=true]", ""},
		{"bell", "bell[facing=north,attachment=floor,powered=false]", ""},
		{"note block", "note_block[instrument=harp,note=0,powered=false]", ""},
		{"copper bulb", "copper_bulb[lit=false,powered=false]", ""},
		{"crafter", "crafter[orientation=north_up,triggered=false,crafting=false]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := witnessObserverScene(t, tc.block, tc.upper)
			for _, edge := range []struct{ name, cell string }{
				{"power", "redstone_block"},
				{"unpower", "air"},
			} {
				pulseTicks, lampOn, lampOff := followWitnessEdge(t, srv, c, 0, 64, 5, edge.cell)
				if pulseTicks != 2 {
					t.Errorf("the %s edge showed the observer powered %d ticks, want a two-tick pulse", edge.name, pulseTicks)
				}
				if !lampOn || !lampOff {
					t.Errorf("the %s edge left the lamp behind the observer lit=%v dark=%v, want it to light and go dark", edge.name, lampOn, lampOff)
				}
			}
		})
	}
}

// TestObserverWatchingTheLowerDoorHalfPulsesWhenTheUpperHalfIsPowered covers
// the partner: the signal touches the upper half, which writes only itself
// and lets the lower half the observer watches follow through the shape
// update, whose own shape pass is what pulses the observer.
// vanilla: DoorBlock.neighborChanged, DoorBlock.updateShape.
func TestObserverWatchingTheLowerDoorHalfPulsesWhenTheUpperHalfIsPowered(t *testing.T) {
	t.Parallel()
	srv, c := witnessObserverScene(t,
		"oak_door[half=lower,facing=north,hinge=left,open=false,powered=false]",
		"oak_door[half=upper,facing=north,hinge=left,open=false,powered=false]")
	for _, edge := range []struct{ name, cell string }{
		{"power", "redstone_block"},
		{"unpower", "air"},
	} {
		pulseTicks, lampOn, lampOff := followWitnessEdge(t, srv, c, 0, 65, 5, edge.cell)
		if pulseTicks != 2 {
			t.Errorf("the %s edge showed the observer powered %d ticks, want a two-tick pulse", edge.name, pulseTicks)
		}
		if !lampOn || !lampOff {
			t.Errorf("the %s edge left the lamp behind the observer lit=%v dark=%v, want it to light and go dark", edge.name, lampOn, lampOff)
		}
	}
}

// TestTorchLitFlipsPulseTheObserverWatchingIt flips a lever on the support of
// a torch either way: the torch's lit flip two ticks later pulses the
// observer standing on the lever's roof, watching the torch, for two game
// ticks — on going dark and on relighting.
func TestTorchLitFlipsPulseTheObserverWatchingIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 0, 64, 4, "stone")
	servertest.Setblock(t, c, 0, 65, 4, "redstone_torch[lit=true]")
	// The lever lives on the stone's east face; the observer above it faces
	// west to watch the torch from the east.
	servertest.Setblock(t, c, 1, 64, 4, "lever[face=wall,facing=east,powered=false]")
	servertest.Setblock(t, c, 1, 65, 4, "observer[facing=west]")
	c.Settle()

	srv.TickPhysics(8)
	if _, powered := servertest.ObserverPowerAt(srv, 1, 65, 4); powered {
		t.Fatal("setup: the observer was already pulsing")
	}

	for _, edge := range []struct{ name string }{
		{"going dark"},
		{"relighting"},
	} {
		c.UseBlock(1, 64, 4)
		c.Settle()
		pulseTicks := 0
		for range 10 {
			srv.TickPhysics(1)
			if _, powered := servertest.ObserverPowerAt(srv, 1, 65, 4); powered {
				pulseTicks++
			}
		}
		if pulseTicks != 2 {
			t.Errorf("%s: the observer showed powered %d ticks, want a two-tick pulse", edge.name, pulseTicks)
		}
	}
}

// TestSetblockNoteBlockStaysUnpowered checks that the powered flip belongs to
// a neighbour change, not to the engine's every-change wake: /setblock puts a
// note block beside a powered redstone block and vanilla leaves it
// unpowered. vanilla: NoteBlock.neighborChanged.
func TestSetblockNoteBlockStaysUnpowered(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 7, "redstone_block")
	servertest.Setblock(t, c, 5, 64, 7, "note_block[instrument=harp,note=0,powered=false]")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 7, "minecraft:note_block", map[string]string{"powered": "false"})
}

// TestSetblockCopperBulbBesidePowerLights checks the bulb's placement read:
// vanilla's onPlace runs the same check its neighbour change does, so a bulb
// written beside a powered redstone block lights. vanilla:
// CopperBulbBlock.onPlace, checkAndFlip.
func TestSetblockCopperBulbBesidePowerLights(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 7, "redstone_block")
	servertest.Setblock(t, c, 5, 64, 7, "copper_bulb[lit=false,powered=false]")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 7, "minecraft:copper_bulb", map[string]string{"lit": "true", "powered": "true"})
}

// TestSetblockCopperBulbOverItselfStaysPut checks the onPlace block-identity
// gate: a /setblock of a bulb over a bulb is no placement, so vanilla runs no
// power read and the written state stands. vanilla: CopperBulbBlock.onPlace.
func TestSetblockCopperBulbOverItselfStaysPut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 7, "redstone_block")
	servertest.Setblock(t, c, 5, 64, 7, "copper_bulb[lit=false,powered=false]")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 7, "minecraft:copper_bulb", map[string]string{"lit": "true", "powered": "true"})

	servertest.Setblock(t, c, 5, 64, 7, "copper_bulb[lit=true,powered=false]")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 7, "minecraft:copper_bulb", map[string]string{"lit": "true", "powered": "false"})
}

// TestPlayerRedstoneEdgesPulseTheObserverWatchingThem checks that the
// player-driven writes of a lever pull, a button press and release, a plate
// edge and a repeater delay click run the shape pass, so an observer watching
// the block gives one two-tick pulse.
// vanilla: LeverBlock.pull, ButtonBlock.press, ButtonBlock.checkPressed,
// BasePressurePlateBlock.checkPressed, RepeaterBlock.useWithoutItem
func TestPlayerRedstoneEdgesPulseTheObserverWatchingThem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, block string
		press       func(srv *server.Server, c *servertest.Client)
		releases    bool
	}{
		{"lever", "lever[face=floor,facing=north,powered=false]", useWatched, false},
		{"oak button", "oak_button[face=floor,facing=north,powered=false]", useWatched, true},
		{"repeater", "repeater[facing=north,delay=1]", useWatched, false},
		{"stone pressure plate", "stone_pressure_plate[powered=false]", func(srv *server.Server, c *servertest.Client) {
			c.MoveToGround(0.5, 64, 4.5, true)
			c.Settle()
			srv.TickRedstoneTouch(1)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			srv.StopTick()
			servertest.Setblock(t, c, 0, 64, 4, tc.block)
			servertest.Setblock(t, c, 1, 64, 4, "observer[facing=west]")
			c.Settle()
			srv.TickPhysics(8)
			if _, powered := servertest.ObserverPowerAt(srv, 1, 64, 4); powered {
				t.Fatal("setup: the observer was already pulsing")
			}

			tc.press(srv, c)
			if got := observerPulseTicks(srv); got != 2 {
				t.Errorf("press: the observer showed powered %d ticks, want a two-tick pulse", got)
			}
			if !tc.releases {
				return
			}
			// The oak button lets go 30 ticks after the press, 10 of them run.
			srv.TickPhysics(19)
			if _, props := servertest.StateProps(srv.BlockAt(0, 64, 4)); props["powered"] != "true" {
				t.Fatal("the button let go before its 30 ticks")
			}
			srv.TickPhysics(1)
			if _, props := servertest.StateProps(srv.BlockAt(0, 64, 4)); props["powered"] != "false" {
				t.Fatal("the button is still pressed 30 ticks on")
			}
			if got := observerPulseTicks(srv); got != 2 {
				t.Errorf("release: the observer showed powered %d ticks, want a two-tick pulse", got)
			}
		})
	}
}

// useWatched uses the block the observer watches with an empty hand.
func useWatched(_ *server.Server, c *servertest.Client) {
	c.UseBlock(0, 64, 4)
	c.Settle()
}

// observerPulseTicks runs 10 physics ticks and counts those the observer at
// (1,64,4) ends powered.
func observerPulseTicks(srv *server.Server) int {
	n := 0
	for range 10 {
		srv.TickPhysics(1)
		if _, powered := servertest.ObserverPowerAt(srv, 1, 64, 4); powered {
			n++
		}
	}
	return n
}

// TestHandToggledDoorsPulseTheObserverWatchingThem opens and then closes a
// trapdoor, a fence gate and a door by hand: each write leaves flag 16 out, so
// its shape pass pulses the observer beside it for two game ticks both times.
// The door is watched at either half, since both halves flip.
// vanilla: DoorBlock.useWithoutItem, TrapDoorBlock.toggle,
// FenceGateBlock.useWithoutItem.
func TestHandToggledDoorsPulseTheObserverWatchingThem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	type cell struct{ x, y, z int32 }
	cases := []struct {
		name     string
		blocks   map[cell]string // written in y order below
		use      cell
		observer cell
	}{
		{"oak_trapdoor", map[cell]string{
			{-2, 64, -3}: "oak_trapdoor[facing=north,half=bottom,open=false]",
		}, cell{-2, 64, -3}, cell{-1, 64, -3}},
		{"oak_fence_gate", map[cell]string{
			{-2, 64, -1}: "oak_fence_gate[facing=north,open=false]",
		}, cell{-2, 64, -1}, cell{-1, 64, -1}},
		{"oak_door lower half watched", map[cell]string{
			{-2, 64, 1}: "oak_door[facing=north,half=lower,hinge=left,open=false]",
			{-2, 65, 1}: "oak_door[facing=north,half=upper,hinge=left,open=false]",
		}, cell{-2, 64, 1}, cell{-1, 64, 1}},
		{"oak_door upper half watched", map[cell]string{
			{-2, 64, 3}: "oak_door[facing=north,half=lower,hinge=left,open=false]",
			{-2, 65, 3}: "oak_door[facing=north,half=upper,hinge=left,open=false]",
		}, cell{-2, 64, 3}, cell{-1, 65, 3}},
	}
	for _, tc := range cases {
		for y := int32(64); y <= 65; y++ {
			for at, block := range tc.blocks {
				if at.y == y {
					servertest.Setblock(t, c, at.x, at.y, at.z, block)
				}
			}
		}
		// Facing west, the observer watches the block on its west side.
		servertest.Setblock(t, c, tc.observer.x, tc.observer.y, tc.observer.z, "observer[facing=west]")
	}
	c.Settle()
	srv.TickPhysics(8)

	for _, tc := range cases {
		if _, powered := servertest.ObserverPowerAt(srv, tc.observer.x, tc.observer.y, tc.observer.z); powered {
			t.Fatalf("%s setup: the observer was already pulsing", tc.name)
		}
		for _, edge := range []struct {
			name string
			open string
		}{{"opening", "true"}, {"closing", "false"}} {
			c.UseBlock(tc.use.x, tc.use.y, tc.use.z)
			c.Settle()
			if _, props := servertest.StateProps(srv.BlockAt(tc.use.x, tc.use.y, tc.use.z)); props["open"] != edge.open {
				t.Fatalf("%s %s: open=%s, want %s", tc.name, edge.name, props["open"], edge.open)
			}
			pulseTicks := 0
			for range 10 {
				srv.TickPhysics(1)
				if _, powered := servertest.ObserverPowerAt(srv, tc.observer.x, tc.observer.y, tc.observer.z); powered {
					pulseTicks++
				}
			}
			if pulseTicks != 2 {
				t.Errorf("%s %s: the observer showed powered %d ticks, want a two-tick pulse", tc.name, edge.name, pulseTicks)
			}
		}
	}
}

// TestFlagTwoBlockWritesPulseTheObserverWatchingThem checks that flag-2 block
// writes still run the shape pass, so the observer beside them pulses for two
// ticks.
// vanilla: ComposterBlock.tick, AbstractSkullBlock.neighborChanged
func TestFlagTwoBlockWritesPulseTheObserverWatchingThem(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, block string) (*server.Server, *servertest.Client) {
		srv, c := servertest.AttachActor(t)
		srv.StopTick()
		servertest.Setblock(t, c, 0, 64, 4, block)
		servertest.Setblock(t, c, 1, 64, 4, "observer[facing=west]")
		c.Settle()
		srv.TickPhysics(8)
		if _, powered := servertest.ObserverPowerAt(srv, 1, 64, 4); powered {
			t.Fatal("setup: the observer was already pulsing")
		}
		return srv, c
	}

	t.Run("composter turning ready", func(t *testing.T) {
		t.Parallel()
		srv, _ := setup(t, "composter[level=7]")
		powered, ready := 0, false
		for range 30 {
			srv.TickPhysics(1)
			if _, props := servertest.StateProps(srv.BlockAt(0, 64, 4)); props["level"] == "8" {
				ready = true
			}
			if _, p := servertest.ObserverPowerAt(srv, 1, 64, 4); p {
				powered++
			}
		}
		if !ready {
			t.Fatal("the composter never turned ready")
		}
		if powered != 2 {
			t.Errorf("the observer showed powered %d ticks, want a two-tick pulse", powered)
		}
	})

	t.Run("powered skull", func(t *testing.T) {
		t.Parallel()
		srv, c := setup(t, "skeleton_skull")
		servertest.Setblock(t, c, 0, 64, 5, "redstone_block")
		if got := observerPulseTicks(srv); got != 2 {
			t.Errorf("the observer showed powered %d ticks, want a two-tick pulse", got)
		}
	})
}
