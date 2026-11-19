package blocks

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The door-like hand uses: the iron trapdoor that ignores them, the fence
// gate that swings away from its opener, and the one sound a swing plays —
// to everyone but the opener, whose client predicts it.

// vanilla: DoorBlock.useWithoutItem, TrapDoorBlock.useWithoutItem,
// FenceGateBlock.useWithoutItem
func TestPoplarBlocksOpenByHand(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"minecraft:poplar_door", "minecraft:poplar_trapdoor", "minecraft:poplar_fence_gate"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
			var props string
			switch name {
			case "minecraft:poplar_door":
				props = "facing=south,half=lower,hinge=left,open=false,powered=false"
			case "minecraft:poplar_trapdoor":
				props = "facing=south,half=bottom,open=false,powered=false,waterlogged=false"
			case "minecraft:poplar_fence_gate":
				props = "facing=south,in_wall=false,open=false,powered=false"
			}
			if !srv.SetBlock(5, 64, 0, servertest.BlockStateWith(t, name, props)) {
				t.Fatalf("could not stand %s", name)
			}
			if name == "minecraft:poplar_door" {
				if !srv.SetBlock(5, 65, 0, servertest.BlockStateWith(t, name, "facing=south,half=upper,hinge=left,open=false,powered=false")) {
					t.Fatal("could not stand the door's upper half")
				}
			}
			for _, open := range []string{"true", "false"} {
				c.UseBlock(5, 64, 0)
				c.Settle()
				servertest.ExpectBlockNamed(t, srv, 5, 64, 0, name, map[string]string{"open": open, "powered": "false"})
				if name == "minecraft:poplar_door" {
					servertest.ExpectBlockNamed(t, srv, 5, 65, 0, name, map[string]string{"open": open, "powered": "false"})
				}
			}
		})
	}
}

// TestIronTrapdoorIgnoresHandUse right-clicks an iron trapdoor: it stays
// shut and the click stays free for the held block, which places against it
// instead — vanilla's InteractionResult.PASS for a set that can't be opened
// by hand. vanilla: TrapDoorBlock.useWithoutItem
func TestIronTrapdoorIgnoresHandUse(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	if !srv.SetBlock(5, 64, 0, servertest.BlockStateWith(t, "minecraft:iron_trapdoor",
		"facing=south,half=bottom,open=false,powered=false,waterlogged=false")) {
		t.Fatal("could not stand the iron trapdoor")
	}

	// The stone in hand is what a passed click falls through to.
	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.UseBlock(5, 64, 0)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:iron_trapdoor", map[string]string{"open": "false"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:stone", nil)
}

// TestGateOpensAwayFromItsOpener opens two fence gates from the south: the
// one whose facing meets the opener's look direction head-on turns to face
// away from them, and the one already facing their way keeps its facing —
// vanilla only turns a gate whose facing is the opener direction's
// opposite. vanilla: FenceGateBlock.useWithoutItem
func TestGateOpensAwayFromItsOpener(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	const facingSouth = "facing=south,in_wall=false,open=false,powered=false"
	if !srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:oak_fence_gate", facingSouth)) ||
		!srv.SetBlock(5, 64, 0, servertest.BlockStateWith(t, "minecraft:oak_fence_gate", facingSouth)) {
		t.Fatal("could not stand the fence gates")
	}
	c.MoveToGround(4.5, 64, 2.5, true)
	c.Settle()

	// Looking north (yaw 180) meets the first gate face-on: it swings to
	// face north, away from the opener.
	c.FacePlayer(4.5, 64, 2.5, 180, 0)
	c.UseBlock(3, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:oak_fence_gate",
		map[string]string{"facing": "north", "open": "true"})

	// Looking south (yaw 0) meets the second gate from behind its facing:
	// it opens where it stands.
	c.FacePlayer(4.5, 64, 2.5, 0, 0)
	c.UseBlock(5, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_fence_gate",
		map[string]string{"facing": "south", "open": "true"})
}

// TestPlacingAPoweredDoorOpensItInThePlacement places a door beside a
// redstone block: the placement's first block update is already open and
// powered, rather than the door standing shut for the redstone pass to swing
// it with a second sound.
// vanilla: DoorBlock.getStateForPlacement
func TestPlacingAPoweredDoorOpensItInThePlacement(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// A redstone block beside the door's lower cell powers the placement.
	redstone, _ := registry.FindState("minecraft:redstone_block", nil)
	if !srv.SetBlock(4, 64, 0, redstone) {
		t.Fatal("could not stand the redstone block")
	}

	actor.MoveToGround(5.5, 64, 2.5, true)
	actor.Settle()

	door, _ := registry.ItemID("minecraft:oak_door")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: door, Count: 1})
	watcher.ExpectEquipment(entityID)

	open, ok := registry.SoundEventID("minecraft:block.wooden_door.open")
	if !ok {
		t.Fatal("no sound event minecraft:block.wooden_door.open")
	}

	actor.FacePlayer(5.5, 64, 2.5, 0, 0)
	actor.PlaceBlock(5, 63, 0, protocol.FaceTop)

	// The door's lower half on the very first update, before anything else.
	var lower *servertest.BlockChange
	doorSounds := 0
	watcher.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range servertest.BlockChangesIn(t, id, data) {
				if ch.X == 5 && ch.Y == 64 && ch.Z == 0 && lower == nil {
					lower = &ch
				}
			}
		case protocol.IDPlaySoundEffect:
			if protocol.NewReader(data).ReadVarInt()-1 == open {
				doorSounds++
			}
		}
		return lower != nil
	})
	if lower == nil {
		t.Fatal("no block update for the door's lower half arrived")
	}
	if name, props := servertest.StateProps(lower.State); name != "minecraft:oak_door" ||
		props["half"] != "lower" || props["open"] != "true" || props["powered"] != "true" {
		t.Errorf("the door's first update is %s %v, want the lower half open and powered", name, props)
	}

	// Nothing swings the door afterwards: the placement already opened it.
	servertest.HoldTicks(t, srv)
	watcher.ReadTicks(srv, servertest.QuietTicks, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == open {
			doorSounds++
		}
		return false
	})
	if doorSounds != 0 {
		t.Errorf("the watcher heard %d door-open sounds, want none: the placement itself opens the door", doorSounds)
	}
	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"open": "true", "powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:oak_door", map[string]string{"open": "true", "powered": "true"})

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestPlacingNextToASignalOpensTrapdoorsAndGates places each trapdoor and
// fence gate beside a redstone block and with no signal: a powered placement
// arrives open and powered in its first update with no open sound, an
// unpowered one closed.
// vanilla: TrapDoorBlock.getStateForPlacement, FenceGateBlock.getStateForPlacement
func TestPlacingNextToASignalOpensTrapdoorsAndGates(t *testing.T) {
	t.Parallel()
	cases := []struct{ block, sound string }{
		{"minecraft:oak_trapdoor", "minecraft:block.wooden_trapdoor.open"},
		{"minecraft:iron_trapdoor", "minecraft:block.iron_trapdoor.open"},
		{"minecraft:oak_fence_gate", "minecraft:block.fence_gate.open"},
	}
	for _, tc := range cases {
		for _, signal := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/signal=%v", tc.block, signal), func(t *testing.T) {
				t.Parallel()
				srv, connect := servertest.Start(t, servertest.BlockTestConfig())
				watcher := connect().Join("Watcher")
				actor := connect().Join("Actor")
				entityID := watcher.ExpectPlayerAdded("Actor")
				watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

				if signal {
					redstone, _ := registry.FindState("minecraft:redstone_block", nil)
					if !srv.SetBlock(4, 64, 0, redstone) {
						t.Fatal("could not stand the redstone block")
					}
				}
				open, ok := registry.SoundEventID(tc.sound)
				if !ok {
					t.Fatalf("no sound event %s", tc.sound)
				}
				actor.MoveToGround(5.5, 64, 2.5, true)
				actor.Settle()
				item, _ := registry.ItemID(tc.block)
				actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 1})
				watcher.ExpectEquipment(entityID)
				actor.FacePlayer(5.5, 64, 2.5, 0, 0)
				actor.PlaceBlock(5, 63, 0, protocol.FaceTop)

				var placed *servertest.BlockChange
				sounds := 0
				watcher.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
					switch id {
					case protocol.IDPlayBlockUpdate:
						r := protocol.NewReader(data)
						x, y, z := r.ReadPosition()
						state := uint16(r.ReadVarInt())
						if x == 5 && y == 64 && z == 0 && placed == nil {
							placed = &servertest.BlockChange{X: x, Y: y, Z: z, State: state}
						}
					case protocol.IDPlaySoundEffect:
						if protocol.NewReader(data).ReadVarInt()-1 == open {
							sounds++
						}
					}
					return placed != nil
				})
				if placed == nil {
					t.Fatal("no block update for the placement arrived")
				}
				want := strconv.FormatBool(signal)
				if name, props := servertest.StateProps(placed.State); name != tc.block ||
					props["open"] != want || props["powered"] != want {
					t.Errorf("first update is %s %v, want open=powered=%s", name, props, want)
				}
				servertest.HoldTicks(t, srv)
				watcher.ReadTicks(srv, servertest.QuietTicks, func(id int32, data []byte) bool {
					if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == open {
						sounds++
					}
					return false
				})
				if sounds != 0 {
					t.Errorf("watcher heard %d open sounds, want none", sounds)
				}
				actor.Conn.Close()
				watcher.ExpectPlayerRemoved(entityID, "Actor")
			})
		}
	}
}
