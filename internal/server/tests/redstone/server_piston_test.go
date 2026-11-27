package redstone

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The piston family through a client's eyes: the way a placement is faced,
// the state the extended head lands with, the collision the reach of the
// extended piston's head gives a body stood beside it, and the state a
// carried block lands with. The drives go through the placement packets and
// the redstone block the server's own physics reacts to, the results out of
// the world and the packets a second client hears.

// wireWrite is one block an observing client was told about: the position,
// the state, whether it rode an Update Section Blocks packet, and the
// arrival number, shared by the entries of one packet.
type wireWrite struct {
	x, y, z int32
	state   uint16
	batch   bool
	packet  int
	arrival int
}

// wireAction is one Block Action packet a piston's move rides: where the
// acting block stands, the action and its parameter, and the arrival number
// of the packet.
type wireAction struct {
	x, y, z       int32
	action, param uint8
	block         int32
	arrival       int
}

// readWireWrites parses the block writes one play packet carries: the single
// write of a Block Update, the whole list of a Section Blocks Update, and
// none for the packets that carry none. A watch that counts what reached a
// client reads both, a piston move's flight riding the batched form.
func readWireWrites(t *testing.T, id int32, data []byte) []wireWrite {
	t.Helper()
	switch id {
	case protocol.IDPlayBlockUpdate:
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		state := uint16(r.ReadVarInt())
		if r.Err() != nil {
			t.Fatalf("block update: %v", r.Err())
		}
		// Every shape-misagree byte the update has left over fails the
		// reader already; the entry needs no count of its own.
		return []wireWrite{{x, y, z, state, false, 0, 0}}
	case protocol.IDPlaySectionBlocks:
		r := protocol.NewReader(data)
		packed := r.ReadI64()
		chunkX, chunkZ := int32(packed>>42)&0x3FFFFF, int32(packed>>20)&0x3FFFFF
		sectionY := int32(packed & 0xFFFFF)
		var writes []wireWrite
		for range r.ReadVarInt() {
			entry := r.ReadVarLong()
			state, cell := uint16(entry>>12), entry&0xFFF
			writes = append(writes, wireWrite{
				chunkX*16 + int32(cell>>8), sectionY*16 + int32(cell&0xF),
				chunkZ*16 + int32(cell>>4&0xF), state, true, 0, 0,
			})
		}
		if r.Err() != nil {
			t.Fatalf("section blocks update: %v", r.Err())
		}
		return writes
	}
	return nil
}

// watchPistonMove runs trigger, which moves a piston's world itself, and
// records what a second client then hears for the next game ticks: every
// block write, every block action, and how many Block Entity Data packets
// came. The window is read in full rather than stopped early, so the
// analysis can say what never arrived.
func watchPistonMove(t *testing.T, srv *server.Server, watcher *servertest.Client, trigger func()) (writes []wireWrite, actions []wireAction, blockEntityData int) {
	t.Helper()
	trigger()
	arrival, packet := 0, 0
	watcher.PumpTicks(srv, 10, func(id int32, data []byte) bool {
		packet++
		for _, w := range readWireWrites(t, id, data) {
			w.packet, w.arrival = packet, arrival
			writes = append(writes, w)
		}
		switch id {
		case protocol.IDPlayBlockAction:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			action, param := r.ReadU8(), r.ReadU8()
			actions = append(actions, wireAction{x, y, z, action, param, r.ReadVarInt(), arrival})
		case protocol.IDPlayBlockEntityData:
			blockEntityData++
		}
		arrival++
		return false
	})
	return writes, actions, blockEntityData
}

// TestAStickyPistonsMoveReachesAClientTheVanillaWay checks, for a push and
// for its pull, the packets a watching client hears, in the shape the vanilla
// reference server sends them (compared with tools/probe): the block action
// first - the client builds the whole animation off it, from its own view of
// the blocks, so it must ride ahead of every update that disturbs the moving
// cells - then the move's own updates of the same game tick, and the final
// blocks two game ticks later as one Update Section Blocks packet for the
// flight cells a section holds. Neither the moving_piston states nor the
// moving piston's block entity are ever sent: the vanilla client animates
// purely off the block action, so a server that puts nothing else between it
// and the move leaves the client to draw the slide.
// vanilla: PistonBaseBlock.triggerEvent, ChunkHolder.broadcastChanges
func TestAStickyPistonsMoveReachesAClientTheVanillaWay(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	// The piston at (2, 64, 0) faces east with the block it takes with it
	// in front and two cells of air ahead. The power on its north side,
	// in the next chunk's section so its own update never shares a packet
	// with the move's, is what the triggers write.
	servertest.Setblock(t, actor, 2, 64, 0, "sticky_piston[facing=east]")
	servertest.Setblock(t, actor, 3, 64, 0, "stone")
	// The setup's writes go out at a tick's flush, ahead of the trigger.
	servertest.RunTicks(t, srv, 1)
	watcher.Settle()

	extended := servertest.BlockStateWith(t, "minecraft:sticky_piston", "extended=true,facing=east")
	head := servertest.BlockStateWith(t, "minecraft:piston_head", "facing=east,short=false,type=sticky")
	stone, _ := registry.FindState("minecraft:stone", nil)
	// 5 is east, the parameter the action carries.
	const east = 5

	// The push.
	writes, actions, beData := watchPistonMove(t, srv, watcher, func() {
		servertest.Setblock(t, actor, 2, 64, -1, "redstone_block")
	})
	if len(actions) != 1 {
		t.Fatalf("the push sent %d block actions, want the one that draws the animation", len(actions))
	}
	if actions[0].x != 2 || actions[0].y != 64 || actions[0].z != 0 ||
		actions[0].action != protocol.PistonActionExtend || actions[0].param != east ||
		actions[0].block != protocol.BlockStickyPiston {
		t.Errorf("the push's block action = %+v, want an extend of the sticky piston at (2, 64, 0) facing east", actions[0])
	}
	if beData != 0 {
		t.Errorf("the push sent %d block entity data packet(s), want none: the ride is the client's", beData)
	}
	var moved, base []wireWrite
	for _, w := range writes {
		name, _ := servertest.StateProps(w.state)
		if name == "minecraft:moving_piston" {
			t.Errorf("the push sent the ride itself: a write of %s at (%d, %d, %d)", name, w.x, w.y, w.z)
		}
		if w.z != 0 || w.y != 64 || w.x < 2 || w.x > 4 {
			continue
		}
		switch w.x {
		case 2:
			base = append(base, w)
		case 3, 4:
			moved = append(moved, w)
		}
		if w.arrival < actions[0].arrival {
			t.Errorf("a write of state %d at (%d, %d, %d) rode ahead of the block action", w.state, w.x, w.y, w.z)
		}
	}
	if len(base) != 1 || base[0].state != extended {
		t.Errorf("the push wrote the piston's own cell %v, want one write of the extended base", base)
	}
	// The pushed block's cells must arrive exactly once each, with the
	// final blocks, and share a packet: the two cells sit in one section.
	if len(moved) != 2 {
		t.Errorf("the push wrote its moving cells %d times, want the two final states", len(moved))
	} else {
		for _, w := range moved {
			if w.state != stone && w.state != head {
				name, _ := servertest.StateProps(w.state)
				t.Errorf("a moving cell was written with state %d (%s), want only the stone and the head", w.state, name)
			}
		}
		if moved[0].packet != moved[1].packet || !moved[0].batch {
			t.Errorf("the final blocks of the push arrived as %d packets, want the one Update Section Blocks batch",
				len(distinctPackets(moved)))
		}
	}

	watcher.Settle()

	// The pull: the same wire read for the power taken away, the stone
	// coming home with the piston.
	retracted := servertest.BlockStateWith(t, "minecraft:sticky_piston", "extended=false,facing=east")
	air, _ := registry.FindState("minecraft:air", nil)
	writes, actions, beData = watchPistonMove(t, srv, watcher, func() {
		servertest.Setblock(t, actor, 2, 64, -1, "air")
	})
	if len(actions) != 1 {
		t.Fatalf("the pull sent %d block actions, want the one that draws the animation", len(actions))
	}
	if actions[0].x != 2 || actions[0].y != 64 || actions[0].z != 0 ||
		actions[0].action != protocol.PistonActionRetract || actions[0].param != east ||
		actions[0].block != protocol.BlockStickyPiston {
		t.Errorf("the pull's block action = %+v, want a retract of the sticky piston at (2, 64, 0) facing east", actions[0])
	}
	if beData != 0 {
		t.Errorf("the pull sent %d block entity data packet(s), want none: the ride is the client's", beData)
	}
	var landed, piston, origin []wireWrite
	for _, w := range writes {
		name, _ := servertest.StateProps(w.state)
		if name == "minecraft:moving_piston" {
			t.Errorf("the pull sent the ride itself: a write of %s at (%d, %d, %d)", name, w.x, w.y, w.z)
		}
		if w.z != 0 || w.y != 64 || w.x < 2 || w.x > 4 {
			continue
		}
		switch w.x {
		case 2:
			piston = append(piston, w)
		case 3:
			landed = append(landed, w)
		case 4:
			origin = append(origin, w)
		}
		if w.arrival < actions[0].arrival {
			t.Errorf("a write of state %d at (%d, %d, %d) rode ahead of the block action", w.state, w.x, w.y, w.z)
		}
	}
	// The pulled block's origin airs with the action's own tick, so the
	// cell cannot carry anything until the stone comes back to it.
	if len(origin) != 1 || origin[0].state != air || origin[0].batch {
		t.Errorf("the pull wrote the pulled block's origin %v, want one Block Update of air in the action's tick", origin)
	}
	if len(landed) != 1 || landed[0].state != stone {
		t.Errorf("the pull wrote the stone's cell %v, want one write of the stone back home", landed)
	}
	if len(piston) != 1 || piston[0].state != retracted {
		t.Errorf("the pull wrote the piston's own cell %v, want one write of the retracted base", piston)
	}
	// The stone and the piston land together, one section shared.
	if len(landed) == 1 && len(piston) == 1 &&
		(landed[0].packet != piston[0].packet || !landed[0].batch) {
		t.Errorf("the final blocks of the pull arrived as %d packets, want the one Update Section Blocks batch",
			len(distinctPackets(append(landed, piston...))))
	}

	// The pull's origin cell, now empty, and the rest of the row are what
	// the world says: the witness hears the whole move as vanilla tells it.
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:sticky_piston",
		map[string]string{"facing": "east", "extended": "false"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:stone", nil)
	if got := srv.BlockAt(4, 64, 0); got != air {
		t.Errorf("the pulled block left %d behind at (4, 64, 0), want air", got)
	}
}

// distinctPackets counts the arrival numbers the writes name, the packets
// they rode.
func distinctPackets(writes []wireWrite) map[int]bool {
	packets := make(map[int]bool, len(writes))
	for _, w := range writes {
		packets[w.packet] = true
	}
	return packets
}

// TestAnExtendedStickyPistonCarriesAWholeHead powers a sticky piston and
// checks the head that lands in front of it: PistonBaseBlock.moveBlocks
// extends a piston_head carrying only the facing and the piston's own type,
// both kinds leaving short at its registered false — a short head's arm is
// four pixels short of reaching the base, which is what drew the extended
// piston's head detached.
func TestAnExtendedStickyPistonCarriesAWholeHead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:sticky_piston", 1))
	c.FacePlayer(0.5, 64, 0.5, 90, 0)
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:sticky_piston", map[string]string{
		"facing": "east", "extended": "false",
	})

	// The redstone block beside the piston is the power, its own
	// placement the block update that runs the extend.
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	c.PlaceBlock(3, 63, 1, protocol.FaceTop)
	c.Settle()
	servertest.AwaitTicks(t, srv, 6)

	servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:sticky_piston",
		map[string]string{"facing": "east", "extended": "true"})
	servertest.ExpectBlockNamed(t, srv, 4, 64, 0, "minecraft:piston_head", map[string]string{
		"facing": "east", "short": "false", "type": "sticky",
	})
}

// TestAPistonBreaksAButtonInsteadOfMovingIt powers a piston into a button of
// each material family: every button's push reaction is DESTROY, so the
// extend breaks the button where it stands and drops its item, and the head
// lands in the cell. A button left at NORMAL rides the move to the cell
// beyond, drops nothing and is still there, which is what a player saw
// before.
// vanilla: Blocks.buttonProperties, PistonBaseBlock.moveBlocks
func TestAPistonBreaksAButtonInsteadOfMovingIt(t *testing.T) {
	for _, tc := range []struct{ name, item string }{
		{"oak", "minecraft:oak_button"},
		{"stone", "minecraft:stone_button"},
		{"polished blackstone", "minecraft:polished_blackstone_button"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			// The piston at (2, 64, 0) faces east into the button at
			// (3, 64, 0), floor-mounted on the world's stone. The ground
			// under the cell past the button keeps a pushed button
			// standing there, so only the break puts an item on the ground.
			servertest.Setblock(t, c, 2, 64, 0, "piston[facing=east]")
			c.Settle()
			servertest.Setblock(t, c, 3, 64, 0, tc.item+"[face=floor,facing=north]")
			servertest.ExpectBlockNamed(t, srv, 3, 64, 0, tc.item, map[string]string{"face": "floor"})

			// The power beside the piston runs the extend.
			servertest.Setblock(t, c, 2, 64, 1, "redstone_block")
			expectOnePistonDrop(t, srv, c, tc.item, [3]int32{3, 64, 0})

			// The move lands the head where the button stood; the cell
			// past it stays air, where a pushed button would be.
			servertest.AwaitTicks(t, srv, 3)
			servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:piston_head", map[string]string{
				"facing": "east", "short": "false", "type": "normal",
			})
			servertest.ExpectAir(t, srv, [3]int32{4, 64, 0})
		})
	}
}

// A piston broken by hand in survival drops itself, its loot table asking
// for no tool. An extended piston's head is no loot of its own: breaking
// either half drops the base's item and takes both. Creative drops nothing.

// pistonBreaker joins a player in the named game mode standing at the flat
// world's spawn, with nothing in either hand.
func pistonBreaker(t *testing.T, gameMode string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = gameMode
	srv, c := servertest.Join(t, cfg, "Breaker")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	return srv, c
}

// standRetractedPiston lays the one-block piston the tests break, facing
// east and unextended.
func standRetractedPiston(t *testing.T, srv *server.Server, sticky bool) [3]int32 {
	t.Helper()
	block := "minecraft:piston"
	if sticky {
		block = "minecraft:sticky_piston"
	}
	if !srv.SetBlock(2, 64, 0, servertest.BlockStateWith(t, block, "extended=false,facing=east")) {
		t.Fatal("the piston's chunk was not loaded")
	}
	return [3]int32{2, 64, 0}
}

// standExtendedPiston lays the extended pair the tests break: the base at
// (2, 64, 0) facing east and the head out at (3, 64, 0), the base's item
// being what either break drops.
func standExtendedPiston(t *testing.T, srv *server.Server, sticky bool) (base, head [3]int32) {
	t.Helper()
	return standExtendedPistonFacing(t, srv, sticky, "east")
}

// standExtendedPistonFacing lays the extended pair with the base at
// (2, 64, 0) facing the named direction and the head one cell that way.
func standExtendedPistonFacing(t *testing.T, srv *server.Server, sticky bool, facing string) (base, head [3]int32) {
	t.Helper()
	baseBlock, headType := "minecraft:piston", "type=normal"
	if sticky {
		baseBlock, headType = "minecraft:sticky_piston", "type=sticky"
	}
	base = [3]int32{2, 64, 0}
	head = base
	switch facing {
	case "up":
		head[1]++
	case "down":
		head[1]--
	case "south":
		head[2]++
	case "north":
		head[2]--
	case "west":
		head[0]--
	default:
		head[0]++
	}
	if !srv.SetBlock(base[0], base[1], base[2], servertest.BlockStateWith(t, baseBlock, "extended=true,facing="+facing)) {
		t.Fatal("the piston's chunk was not loaded")
	}
	if !srv.SetBlock(head[0], head[1], head[2], servertest.BlockStateWith(t, "minecraft:piston_head", "facing="+facing+",short=false,"+headType)) {
		t.Fatal("the piston head's chunk was not loaded")
	}
	return base, head
}

// expectOnePistonDrop reads the item spawn the break sent, checks it holds
// exactly one of the named item at the block the drop came from, and that
// the world ends with that one item and no other. The spawn's arrival is
// what says the break has been through: the item is laid while the block
// changes are still being made.
func expectOnePistonDrop(t *testing.T, srv *server.Server, c *servertest.Client, item string, at [3]int32) {
	t.Helper()
	drop := c.ExpectDroppedItem()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	if drop.Stack.ItemID != id || drop.Stack.Count != 1 {
		t.Errorf("the drop = %+v, want one %s (item %d)", drop.Stack, item, id)
	}
	const jitter = 0.25 + 1e-9
	if math.Abs(drop.X-float64(at[0])-0.5) > jitter ||
		math.Abs(drop.Y-float64(at[1])-0.375) > jitter ||
		math.Abs(drop.Z-float64(at[2])-0.5) > jitter {
		t.Errorf("the drop spawned at (%v, %v, %v), want its box within a quarter block of the centre of (%d, %d, %d)",
			drop.X, drop.Y, drop.Z, at[0], at[1], at[2])
	}
	found := servertest.ExpectOneDroppedItem(t, srv, item)
	if found.Count != 1 {
		t.Errorf("the item in the world holds %d, want 1", found.Count)
	}
}

// TestPistonDropsWhenBrokenByHand digs the piston family with nothing in the
// hand: a retracted piston, an extended one broken at its base and one
// broken at its head, each leave exactly one item, the sticky pair the
// sticky piston's own.
func TestPistonDropsWhenBrokenByHand(t *testing.T) {
	for _, sticky := range []bool{false, true} {
		baseBlock, item := "minecraft:piston", "minecraft:piston"
		if sticky {
			baseBlock, item = "minecraft:sticky_piston", "minecraft:sticky_piston"
		}
		for _, tc := range []struct {
			name      string
			extended  bool
			breakHead bool
		}{
			{"retracted", false, false},
			{"extended, broken by its base", true, false},
			{"extended, broken by its head", true, true},
		} {
			name := baseBlock[len("minecraft:"):] + " " + tc.name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				srv, c := pistonBreaker(t, "survival")

				var base, head [3]int32
				if tc.extended {
					base, head = standExtendedPiston(t, srv, sticky)
				} else {
					base = standRetractedPiston(t, srv, sticky)
				}
				c.Settle()

				at := base
				if tc.breakHead {
					at = head
				}
				c.BreakBlock(at[0], at[1], at[2])
				// The drop is laid around the cells' updates - before
				// them when the base breaks, between them when the head
				// does - and a survival dig finishes on the tick, so wait
				// on the updates themselves: a settle only proves the
				// connection read the break, not that the tick applied it.
				if tc.extended {
					c.AwaitAirUpdates(base, head)
				} else {
					c.AwaitAirUpdates(base)
				}
				expectOnePistonDrop(t, srv, c, item, base)
				// Both halves go with the break, the head having no loot of
				// its own to drop.
				if tc.extended {
					servertest.ExpectAir(t, srv, base, head)
				} else {
					servertest.ExpectAir(t, srv, base)
				}
			})
		}
	}
}

// TestAMovedObserverKeepsTheEnginePumping lays out the core a flying machine
// flies on and kicks it through the observed cell: the piston pushes the
// slime and the observer out, and the moved observer's landing stops the
// pulse it left with and books the next, so the machine cycles on its own.
// A caught state lasts a game tick or two, so the cycle is read off the
// client's block updates rather than by polling, which a catch-up burst can
// skip. Both forms of the updates are read, since a move's landing rides a
// Section Blocks Update when its section holds more than one flight cell.
// vanilla: PistonMovingBlockEntity.tick, ObserverBlock.updateShape, ObserverBlock.onPlace
func TestAMovedObserverKeepsTheEnginePumping(t *testing.T) {
	// Not parallel: a long held-tick run starves under a loaded runner and times out.
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 8, 65, 0, "sticky_piston[facing=east]")
	servertest.Setblock(t, c, 9, 65, 0, "slime_block")
	servertest.Setblock(t, c, 9, 66, 0, "observer[facing=east]")
	c.Settle()

	// The kick: a stone in the observed cell starts the pulse, whose back
	// powers the piston, and the extend carries a powered observer out.
	servertest.HoldTicks(t, srv)
	c.ChatCommand("setblock 10 66 0 stone")

	extended := -1
	turns := 0
	awayDark, homeDark := false, false
	watch := func(id int32, data []byte) bool {
		for _, w := range readWireWrites(t, id, data) {
			name, props := servertest.StateProps(w.state)
			switch {
			case w.x == 8 && w.y == 65 && w.z == 0 && name == "minecraft:sticky_piston":
				e := 0
				if props["extended"] == "true" {
					e = 1
				}
				if extended != -1 && e != extended {
					turns++
				}
				extended = e
			case name == "minecraft:observer" && props["powered"] == "false":
				if w.x == 10 && w.y == 66 && w.z == 0 {
					awayDark = true
				}
				if w.x == 9 && w.y == 66 && w.z == 0 {
					homeDark = true
				}
			}
		}
		return turns >= 4 && awayDark && homeDark
	}
	if !c.PumpTicks(srv, 120, watch) {
		t.Fatalf("the engine stopped pumping: %d piston turns, away dark=%v, home dark=%v",
			turns, awayDark, homeDark)
	}
}

// TestAStickyPistonsRetractionKeepsAnAttachedDiode lays a repeater, a
// comparator and redstone dust on the base of a sticky piston, extends the
// piston and retracts it: the base turns into a moving piston for the
// retraction and the wire on it stays, as in vanilla.
// vanilla: PistonBaseBlock.triggerEvent
func TestAStickyPistonsRetractionKeepsAnAttachedDiode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, block, kept string }{
		{"repeater", "repeater[facing=north]", "repeater"},
		{"comparator", "comparator[facing=north]", "comparator"},
		{"dust", "redstone_wire", "redstone_wire"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.Setblock(t, c, 2, 70, 0, "sticky_piston[facing=down]")
			servertest.Setblock(t, c, 2, 71, 0, tc.block)
			servertest.Setblock(t, c, 2, 70, 1, "redstone_block")
			c.Settle()
			servertest.AwaitTicks(t, srv, 8)
			servertest.ExpectBlockNamed(t, srv, 2, 70, 0, "minecraft:sticky_piston",
				map[string]string{"facing": "down", "extended": "true"})
			servertest.ExpectBlockNamed(t, srv, 2, 71, 0, "minecraft:"+tc.kept, nil)
			servertest.Setblock(t, c, 2, 70, 1, "air")
			c.Settle()
			servertest.AwaitTicks(t, srv, 8)
			servertest.ExpectBlockNamed(t, srv, 2, 70, 0, "minecraft:sticky_piston",
				map[string]string{"facing": "down", "extended": "false"})
			servertest.ExpectBlockNamed(t, srv, 2, 71, 0, "minecraft:"+tc.kept, nil)
		})
	}
}
