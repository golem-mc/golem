package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// selfEntityID is this connection's own player, from Login (play).
var selfEntityID int32

// position is where the server last said the player is.
var position struct {
	sync.Mutex
	x, y, z float64
}

// walk steps the player east by WALK chunks, which makes the server send the
// chunks ahead and unload those behind.
//
// Steps are short and spaced: vanilla teleports back a player covering more
// than ten blocks a tick, walking into a hill or hanging in the air. Each
// step starts from the server's last position, so a push-back only costs
// ground.
func walk(c *protocol.Conn) {
	chunks, err := strconv.Atoi(os.Getenv("WALK"))
	if err != nil || chunks <= 0 {
		return
	}

	const step = 4.0
	position.Lock()
	startX := position.x
	position.Unlock()

	var x float64
	for range chunks * 8 {
		time.Sleep(100 * time.Millisecond)

		position.Lock()
		position.x += step
		x = position.x
		y, z := position.y, position.z
		position.Unlock()

		var b protocol.Buffer
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutU8(0x01) // on the ground
		if err := c.WritePacket(protocol.IDPlayPosition, b.Bytes()); err != nil {
			fmt.Printf("PLAY walk stopped: %v\n", err)
			return
		}
		if x-startX >= float64(chunks*16) {
			break
		}
	}
	fmt.Printf("PLAY walked %.0f blocks east, to x=%.1f\n", x-startX, x)
}

// clientInformation fills in the client settings with the given Displayed
// Skin Parts mask; it is sent in configuration and again on any change. The
// render distance is VIEW_DISTANCE, default 4, so a measurement can watch
// an entity's whole tracking range.
func clientInformation(b *protocol.Buffer, skinParts uint8) {
	b.PutString("en_GB")
	b.PutI8(int8(viewDistance())) // render distance
	b.PutVarInt(0)                // chat mode: enabled
	b.PutBool(true)
	b.PutU8(skinParts)
	b.PutVarInt(1)   // main hand: right
	b.PutBool(false) // no text filtering
	b.PutBool(true)  // allow server listings
	b.PutVarInt(0)   // all particles
}

// viewDistance is the render distance a probe asks for: VIEW_DISTANCE when it
// is set to something the field can carry, four otherwise.
func viewDistance() int {
	if v := os.Getenv("VIEW_DISTANCE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 128 {
			return n
		}
	}
	return 4
}

// dumpedPackets reads HEXDUMP, a comma-separated list of play packet IDs
// (decimal or 0x hex) to print raw; unset dumps nothing. It is for working
// out a layout internal/protocol/ does not know yet from a vanilla server's bytes.
func dumpedPackets() map[int32]bool {
	spec := os.Getenv("HEXDUMP")
	if spec == "" {
		return nil
	}

	ids := map[int32]bool{}
	for one := range strings.SplitSeq(spec, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(one), 0, 32)
		if err != nil {
			fmt.Printf("HEXDUMP %q: not a packet ID\n", one)
			continue
		}
		ids[int32(id)] = true
	}
	return ids
}

// sendGive fills inventory slots as a creative client does, if GIVE is set:
// comma-separated slot:item:count triples, the slot numbered as the player
// window does (36-44 hotbar, 5-8 armour, 45 off hand) and the item its
// minecraft:item ID, optionally followed by enchantment:level pairs:
//
//	GIVE=36:926:5,5:998:1        # five diamonds in hand, a diamond helmet worn
//	GIVE=36:966:1:efficiency:5   # a pickaxe with Efficiency V in hand
//
// In creative the client decides what exists, so this is how inventories are
// built for rejoin tests and player files for vanilla to read back.
func sendGive(c *protocol.Conn) {
	spec := os.Getenv("GIVE")
	if spec == "" {
		return
	}

	for one := range strings.SplitSeq(spec, ",") {
		parts := strings.Split(one, ":")
		// Three fields, or three plus whole enchantment:level pairs.
		if len(parts) < 3 || len(parts)%2 == 0 {
			fmt.Printf("PLAY give %q: want slot:item:count[:enchant:level]...\n", one)
			continue
		}
		slot, errSlot := strconv.Atoi(parts[0])
		item, errItem := strconv.Atoi(parts[1])
		count, errCount := strconv.Atoi(parts[2])
		if errSlot != nil || errItem != nil || errCount != nil {
			fmt.Printf("PLAY give %q: want three numbers\n", one)
			continue
		}
		stack := protocol.ItemStack{ItemID: int32(item), Count: int32(count)}
		if len(parts) > 3 {
			enchantments, err := giveEnchantments(parts[3:])
			if err != nil {
				fmt.Printf("PLAY give %q: %v\n", one, err)
				continue
			}
			stack.Enchantments = enchantments
		}

		var b protocol.Buffer
		b.PutI16(int16(slot))
		b.PutUntrustedSlot(stack)
		must(c.WritePacket(protocol.IDPlayCreativeSlot, b.Bytes()))
		fmt.Printf("PLAY gave slot=%d item=%d count=%d enchantments=%v\n", slot, item, count, stack.Enchantments)
	}
}

// giveEnchantments reads a GIVE entry's enchantment:level pairs, names
// without minecraft: (the entry is split on colons). A level below one is
// refused: the wire codec reads no level-zero enchantment.
func giveEnchantments(pairs []string) (map[int32]int32, error) {
	enchantments := make(map[int32]int32, len(pairs)/2)
	for len(pairs) >= 2 {
		id, ok := registry.EnchantmentID("minecraft:" + pairs[0])
		if !ok {
			return nil, fmt.Errorf("unknown enchantment %q", pairs[0])
		}
		level, err := strconv.Atoi(pairs[1])
		if err != nil || level < 1 || level > 255 {
			return nil, fmt.Errorf("level %q is not between 1 and 255", pairs[1])
		}
		enchantments[id] = int32(level)
		pairs = pairs[2:]
	}
	return enchantments, nil
}

// sendUse right-clicks with the held item if USE is set, which equips
// armour; a server that ignores it leaves the armour in hand, visible only in
// what gets saved.
func sendUse(c *protocol.Conn) {
	if os.Getenv("USE") == "" {
		return
	}

	var b protocol.Buffer
	b.PutVarInt(protocol.HandMain)
	b.PutVarInt(1) // block change sequence number
	b.PutF32(0)    // yaw
	b.PutF32(0)    // pitch
	must(c.WritePacket(protocol.IDPlayUseItem, b.Bytes()))
	fmt.Println("PLAY sent use-item")
}

// act swings an arm, sneaks, sprints and changes skin layers if ACT is set,
// for a second probe to check they are relayed. Steps are spaced so the
// other transcript reads in order, and each state is left again before the
// next so it is relayed both ways.
func act(c *protocol.Conn) {
	if os.Getenv("ACT") == "" {
		return
	}

	steps := []struct {
		what   string
		id     int32
		fields func(*protocol.Buffer)
	}{
		{"punch", protocol.IDPlayPunch, func(*protocol.Buffer) {}},
		{"start sneaking", protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
			b.PutU8(protocol.InputSneak)
		}},
		{"stop sneaking", protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
			b.PutU8(0)
		}},
		{"start sprinting", protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
			b.PutVarInt(0) // entity ID, which the server takes from the connection
			b.PutVarInt(protocol.PlayerCommandStartSprinting)
			b.PutVarInt(0) // jump boost, which only a horse uses
		}},
		{"stop sprinting", protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
			b.PutVarInt(0)
			b.PutVarInt(protocol.PlayerCommandStopSprinting)
			b.PutVarInt(0)
		}},
		// Every skin layer off, reported again in full in the play phase.
		{"hide the skin layers", protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
			clientInformation(b, 0x00)
		}},
		{"show the skin layers", protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
			clientInformation(b, 0x7F)
		}},
	}

	for _, step := range steps {
		time.Sleep(500 * time.Millisecond)
		var b protocol.Buffer
		step.fields(&b)
		if err := c.WritePacket(step.id, b.Bytes()); err != nil {
			fmt.Printf("PLAY acting stopped: %v\n", err)
			return
		}
		fmt.Printf("PLAY sent %s\n", step.what)
	}
}

// subscribeDebugSamples asks for the tick-time samples the F3 TPS chart is
// drawn from, when DEBUG_SAMPLES is set; a server sends them to operators only.
func subscribeDebugSamples(c *protocol.Conn) {
	if os.Getenv("DEBUG_SAMPLES") == "" {
		return
	}
	var b protocol.Buffer
	b.PutVarInt(1)
	b.PutVarInt(protocol.DebugSubscriptionTickTime)
	must(c.WritePacket(protocol.IDPlayDebugSubscriptionRequest, b.Bytes()))
	fmt.Println("PLAY sent debug subscription request")
}

// sendChat says whatever CHAT holds, once the world has loaded. The message is
// unsigned, which a server only accepts while it does not enforce secure chat.
func sendChat(c *protocol.Conn) {
	message := os.Getenv("CHAT")
	if message == "" {
		return
	}

	var b protocol.Buffer
	b.PutString(message)
	b.PutI64(time.Now().UnixMilli())
	b.PutI64(0)      // salt, which only matters to a signature
	b.PutBool(false) // no signature
	b.PutVarInt(0)   // no messages of the server's acknowledged
	b.PutRaw([]byte{0, 0, 0})
	// The checksum covers the acknowledged messages, of which there are none:
	// Java hashes an empty array to 1, and the server compares against that.
	b.PutU8(1)

	must(c.WritePacket(protocol.IDPlayChatMessage, b.Bytes()))
	fmt.Printf("PLAY sent chat %q\n", message)
}

// sendCommand runs COMMAND, without its leading slash as Chat Command
// carries it. COMMAND2 is sent COMMAND2_DELAY seconds later, for a run that
// sets a state first and observes second.
func sendCommand(c *protocol.Conn) {
	command := os.Getenv("COMMAND")
	if command == "" {
		return
	}
	// COMMAND_DELAY holds the command back that many seconds, for commands
	// that only mean something once the server counts the player as loaded.
	if delay, err := strconv.Atoi(os.Getenv("COMMAND_DELAY")); err == nil && delay > 0 {
		time.Sleep(time.Duration(delay) * time.Second)
	}

	var b protocol.Buffer
	b.PutString(command)
	must(c.WritePacket(protocol.IDPlayChatCommand, b.Bytes()))
	fmt.Printf("PLAY sent command %q\n", command)

	second := os.Getenv("COMMAND2")
	if second == "" {
		return
	}
	if delay, err := strconv.Atoi(os.Getenv("COMMAND2_DELAY")); err == nil && delay > 0 {
		time.Sleep(time.Duration(delay) * time.Second)
	}
	b = protocol.Buffer{}
	b.PutString(second)
	must(c.WritePacket(protocol.IDPlayChatCommand, b.Bytes()))
	fmt.Printf("PLAY sent command2 %q\n", second)
}

// sendCommandSuggestion asks for completions of SUGGEST, the text left of
// the cursor with its leading slash.
func sendCommandSuggestion(c *protocol.Conn) {
	text := os.Getenv("SUGGEST")
	if text == "" {
		return
	}

	var b protocol.Buffer
	b.PutVarInt(1) // transaction ID
	b.PutString(text)
	must(c.WritePacket(protocol.IDPlayCommandSuggestion, b.Bytes()))
	fmt.Printf("PLAY sent command-suggestion %q\n", text)
}
