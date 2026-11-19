// The scenarios a probe runs once the world has loaded: SETUP commands, a
// FALL replayed one movement packet a tick, an EAT hold, and the container
// and block scenarios below. Each captures a server's answers to a scripted
// client, run against vanilla and against this server to compare.
//
//	SETUP="fill 64 199 64 68 199 68 stone" FALL=sprint2 ...
//	EAT=1 ...
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// scenario runs the SETUP, FALL, BOUNCE, EAT, ENCHANT, SLEEP, LID, PISTON and ANVIL
// steps the environment asks for, in that order.
func scenario(c *protocol.Conn) {
	if os.Getenv("SETUP") == "" && os.Getenv("FALL") == "" &&
		os.Getenv("EAT") == "" && os.Getenv("ENCHANT") == "" &&
		os.Getenv("SLEEP") == "" && os.Getenv("LID") == "" &&
		os.Getenv("PISTON") == "" && os.Getenv("ANVIL") == "" &&
		os.Getenv("BOUNCE") == "" {
		return
	}
	traceSurvivalPackets()
	defer func() { traceScenarioPacket = nil }()
	// The world and its chunks have to be around the player before anything
	// scripted happens in it.
	time.Sleep(1500 * time.Millisecond)
	setupCommands(c)
	if fall := os.Getenv("FALL"); fall != "" {
		time.Sleep(1000 * time.Millisecond)
		replayFall(c, fall)
	}
	if os.Getenv("BOUNCE") != "" {
		time.Sleep(1000 * time.Millisecond)
		replayBounce(c)
	}
	if os.Getenv("EAT") != "" {
		// EAT_DELAY holds the meal back so a watching probe can be in the
		// world first.
		if delay, err := strconv.Atoi(os.Getenv("EAT_DELAY")); err == nil && delay > 0 {
			time.Sleep(time.Duration(delay) * time.Second)
		}
		time.Sleep(1000 * time.Millisecond)
		holdMeal(c)
	}
	if os.Getenv("ENCHANT") != "" {
		time.Sleep(1000 * time.Millisecond)
		enchantScenario(c)
	}
	if os.Getenv("SLEEP") != "" {
		time.Sleep(1000 * time.Millisecond)
		sleepScenario(c)
	}
	if os.Getenv("LID") != "" {
		time.Sleep(1000 * time.Millisecond)
		lidScenario(c)
	}
	if os.Getenv("PISTON") != "" {
		time.Sleep(1000 * time.Millisecond)
		pistonScenario(c)
	}
	if os.Getenv("ANVIL") != "" {
		time.Sleep(1000 * time.Millisecond)
		anvilScenario(c)
	}
}

// setupCommands sends the pipe-separated commands SETUP holds, spaced far
// enough apart that each has been seen before the next is sent.
func setupCommands(c *protocol.Conn) {
	spec := os.Getenv("SETUP")
	if spec == "" {
		return
	}
	for command := range strings.SplitSeq(spec, "|") {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		scenarioCommand(c, command)
		time.Sleep(700 * time.Millisecond)
	}
}

// sendCommand runs one chat command as the probe's own player, which is what
// builds a scenario's platforms and teleports it about.
func scenarioCommand(c *protocol.Conn, command string) {
	var b protocol.Buffer
	b.PutString(command)
	if err := c.WritePacket(protocol.IDPlayChatCommand, b.Bytes()); err != nil {
		fmt.Printf("PLAY scenario stopped: %v\n", err)
		return
	}
	fmt.Printf("PLAY scenario command %q\n", command)
}

// movePacket sends one step of a replay, in the shape a client reports one.
func movePacket(c *protocol.Conn, x, y, z float64, onGround bool) {
	var b protocol.Buffer
	b.PutF64(x)
	b.PutF64(y)
	b.PutF64(z)
	b.PutF32(0) // yaw
	b.PutF32(0) // pitch
	var flags uint8
	if onGround {
		flags = 0x01
	}
	b.PutU8(flags)
	must(c.WritePacket(protocol.IDPlayPositionAndRotation, b.Bytes()))
}

// statusPacket sends a movement-flags-only packet: the client's report that
// its ground flag changed with no position attached.
func statusPacket(c *protocol.Conn, onGround bool) {
	var flags uint8
	if onGround {
		flags = 0x01
	}
	must(c.WritePacket(protocol.IDPlayMovementFlags, []byte{flags}))
}

// bounceStep is one packet of a bounce replay; status marks the flags-only one.
type bounceStep struct {
	y        float64
	onGround bool
	status   bool
}

// bounceApproach is the n falling packets that end in a step of last blocks
// down onto y, walking the fall's gravity and drag backwards.
func bounceApproach(y, last float64, n int) []bounceStep {
	steps := make([]bounceStep, n)
	for i := n - 1; i >= 0; i-- {
		y += last
		last = last/0.98 - 0.08
		steps[i] = bounceStep{y: y}
	}
	return steps
}

// replayBounce lands on a slime block twice and prints the jump statistic
// each time: a landing packet then a rise (A) must not count a jump, a
// landing by a flags-only packet then a rise (B) must.
func replayBounce(c *protocol.Conn) {
	const tick = 50 * time.Millisecond
	sequences := []struct {
		name  string
		steps []bounceStep
	}{
		{"A", append(bounceApproach(-59.110, 0.83, 5), []bounceStep{{-59.110, false, false}, {-60.000, true, false}, {-58.909, false, false}}...)},
		{"B", append(bounceApproach(-59.170, 0.6957, 5), []bounceStep{{-59.170, false, false}, {-59.930, false, false}, {0, true, true}, {-59.593, false, false}}...)},
	}
	scenarioCommand(c, "scoreboard objectives add jj minecraft.custom:minecraft.jump")
	scenarioCommand(c, "gamemode survival")
	scenarioCommand(c, "fill -3 -62 -3 3 -62 3 stone")
	scenarioCommand(c, "setblock 0 -61 0 slime_block")
	time.Sleep(700 * time.Millisecond)
	for _, seq := range sequences {
		scenarioCommand(c, "scoreboard players set @s jj 0")
		start := seq.steps[0].y
		teleportTo(c, 0.5, start, 0.5)
		// Vanilla kicks a second position packet in one tick unless a client
		// tick end separates them.
		for range 2 {
			movePacket(c, 0.5, start, 0.5, true)
			must(c.WritePacket(protocol.IDPlayClientTickEnd, nil))
			time.Sleep(400 * time.Millisecond)
		}
		for _, st := range seq.steps {
			if st.status {
				statusPacket(c, st.onGround)
			} else {
				movePacket(c, 0.5, st.y, 0.5, st.onGround)
			}
			must(c.WritePacket(protocol.IDPlayClientTickEnd, nil))
			if st.status {
				// Vanilla's player entity tick between the flags packet and
				// the next position packet clears the ground flag, so the
				// two have to reach the server in one tick, as they do when
				// a client tick and a server tick line up.
				time.Sleep(3 * time.Millisecond)
				continue
			}
			time.Sleep(tick)
		}
		time.Sleep(300 * time.Millisecond)
		fmt.Printf("PLAY scenario bounce sequence %s sent\n", seq.name)
		scenarioCommand(c, "scoreboard players get @s jj")
		time.Sleep(500 * time.Millisecond)
	}
}

// settleStep puts the player down at a spot and lets the server take it in,
// the way a client that has just been teleported stands still for a moment.
func settleStep(c *protocol.Conn, x, y, z float64) {
	movePacket(c, x, y, z, true)
	time.Sleep(400 * time.Millisecond)
	movePacket(c, x, y, z, true)
	time.Sleep(400 * time.Millisecond)
}

// teleportTo moves the scripted player with /tp, which is what puts them
// wherever a scenario's platforms are.
func teleportTo(c *protocol.Conn, x, y, z float64) {
	scenarioCommand(c, teleportCommand(x, y, z))
	time.Sleep(700 * time.Millisecond)
}

// teleportCommand is the /tp that puts the running player at a spot, the
// decimal places keeping the landing off the block edges.
func teleportCommand(x, y, z float64) string {
	return "tp " + strconv.FormatFloat(x, 'f', 2, 64) + " " +
		strconv.FormatFloat(y, 'f', 2, 64) + " " +
		strconv.FormatFloat(z, 'f', 2, 64)
}

// fallStep is one tick of a body's fall the way the game's physics make one:
// the velocity a tick leaves is the drag of one past gravity's pull.
func fallStep(v float64) float64 {
	return (v - 0.08) * 0.98
}

// replayFall walks a scripted fall one movement packet a tick: a sprint
// jump off an edge, a walk off one, a plain drop, or a jump into a ceiling.
func replayFall(c *protocol.Conn, name string) {
	const tick = 50 * time.Millisecond
	edgeX := 68.5 // the last block of every tower's top, walking east off it
	switch name {
	case "sprint2":
		// A jump off an edge two blocks above the landing floor: the apex
		// adds a block and a quarter to the fall.
		teleportTo(c, 66.5, 200, 66.5)
		settleStep(c, 66.5, 200, 66.5)
		replayJump(c, edgeX, 200, 66.5, 198, 0.30, tick)
	case "jump4":
		// A jump off an edge four blocks up: five and a quarter of fall.
		teleportTo(c, 66.5, 200, 82.5)
		settleStep(c, 66.5, 200, 82.5)
		replayJump(c, edgeX, 200, 82.5, 196, 0.30, tick)
	case "drop2":
		// A walk off the same two-block edge, no jump at all.
		teleportTo(c, 66.5, 200, 66.5)
		settleStep(c, 66.5, 200, 66.5)
		replayWalkOff(c, edgeX, 200, 66.5, 198, 0.28, tick)
	case "fall4":
		// A walk off the four-block edge.
		teleportTo(c, 66.5, 200, 82.5)
		settleStep(c, 66.5, 200, 82.5)
		replayWalkOff(c, edgeX, 200, 82.5, 196, 0.28, tick)
	case "fall6":
		// A walk off into a six-block drop.
		teleportTo(c, 66.5, 200, 114.5)
		settleStep(c, 66.5, 200, 114.5)
		replayWalkOff(c, edgeX, 200, 114.5, 194, 0.28, tick)
	case "water4":
		// A walk off into water three deep, which resets the fall
		// distance.
		teleportTo(c, 66.5, 200, 146.5)
		settleStep(c, 66.5, 200, 146.5)
		replayWalkOff(c, edgeX, 200, 146.5, 195, 0.28, tick)
	case "bump":
		// A jump straight up under a ceiling two blocks above the floor:
		// the rise is clipped, the body falls back where it started.
		teleportTo(c, 66.5, 200, 98.5)
		settleStep(c, 66.5, 200, 98.5)
		replayBump(c, 66.5, 200, 98.5, 202.0, tick)
	default:
		fmt.Printf("PLAY scenario: unknown fall %q\n", name)
		return
	}
	fmt.Printf("PLAY scenario fall %q done\n", name)
}

// replayJump walks the parabola of a sprint jump east off an edge down to a
// floor drop blocks below.
func replayJump(c *protocol.Conn, edgeX, floorY, z, landY, pace float64, tick time.Duration) {
	x, y := edgeX, floorY
	vy := 0.42
	for range 60 {
		x += pace
		y += vy
		vy = fallStep(vy)
		if y <= landY {
			movePacket(c, x, landY, z, true)
			fmt.Printf("PLAY scenario landed y=%.4f x=%.4f\n", landY, x)
			return
		}
		movePacket(c, x, y, z, false)
		time.Sleep(tick)
	}
	fmt.Printf("PLAY scenario: the jump never landed\n")
}

// replayWalkOff walks a body off an edge at a walking pace and follows the
// fall down to the floor drop blocks below where the walking was.
func replayWalkOff(c *protocol.Conn, edgeX, floorY, z, landY, pace float64, tick time.Duration) {
	x, y := edgeX-1.2, floorY
	for range 8 {
		x += pace
		movePacket(c, x, y, z, true)
		time.Sleep(tick)
	}
	vy := 0.0
	for range 60 {
		x += pace
		vy = fallStep(vy)
		y -= -vy
		if y <= landY {
			movePacket(c, x, landY, z, true)
			fmt.Printf("PLAY scenario landed y=%.4f x=%.4f\n", landY, x)
			return
		}
		movePacket(c, x, y, z, false)
		time.Sleep(tick)
	}
	fmt.Printf("PLAY scenario: the walk-off never landed\n")
}

// replayBump jumps straight up into a ceiling ceilY above the floor, rides
// the clipped rise and falls back to the floor it left.
func replayBump(c *protocol.Conn, x, floorY, z, ceilY float64, tick time.Duration) {
	y := floorY
	vy := 0.42
	headroom := ceilY - 1.8 // where the head stops the rise
	for i := range 30 {
		y += vy
		if y+1.8 > ceilY {
			y = headroom
			vy = 0 // the ceiling takes the rise
		} else {
			vy = fallStep(vy)
		}
		if i > 2 && y <= floorY {
			movePacket(c, x, floorY, z, true)
			fmt.Printf("PLAY scenario landed y=%.4f\n", floorY)
			return
		}
		movePacket(c, x, y, z, false)
		time.Sleep(tick)
	}
	fmt.Printf("PLAY scenario: the bump never landed\n")
}

// holdMeal sends a held right-click on a meal: one Use Item a meal, the next
// sent as soon as the last is done, for three meals, which shows the
// server's rhythm. The release form lets the button go before the meal is
// down (Player Action release-use).
func holdMeal(c *protocol.Conn) {
	const meal = 2000 * time.Millisecond
	if os.Getenv("EAT") == "release" {
		useItem(c)
		time.Sleep(600 * time.Millisecond)
		releaseUse(c)
		time.Sleep(1200 * time.Millisecond)
		fmt.Printf("PLAY scenario eat released\n")
		return
	}
	meals := 3
	if v, err := strconv.Atoi(os.Getenv("EAT_MEALS")); err == nil && v > 0 {
		meals = v
	}
	for i := 0; i < meals; i++ {
		useItem(c)
		time.Sleep(meal)
	}
	fmt.Printf("PLAY scenario eat done\n")
}

// useItem right-clicks with what the main hand holds, the packet a client
// sends the moment its player begins a use.
func useItem(c *protocol.Conn) {
	var b protocol.Buffer
	b.PutVarInt(protocol.HandMain)
	b.PutVarInt(1) // block change sequence
	b.PutF32(0)    // yaw
	b.PutF32(0)    // pitch
	if err := c.WritePacket(protocol.IDPlayUseItem, b.Bytes()); err != nil {
		fmt.Printf("PLAY scenario stopped: %v\n", err)
		return
	}
	fmt.Printf("PLAY scenario use-item\n")
}

// releaseUse sends the Player Action release-use status, with the zero
// position and face a release carries.
func releaseUse(c *protocol.Conn) {
	var b protocol.Buffer
	b.PutVarInt(protocol.PlayerActionReleaseUseItem)
	b.PutPosition(0, 0, 0)
	b.PutI8(0) // face
	b.PutVarInt(0)
	if err := c.WritePacket(protocol.IDPlayPlayerAction, b.Bytes()); err != nil {
		fmt.Printf("PLAY scenario stopped: %v\n", err)
		return
	}
	fmt.Printf("PLAY scenario release-use\n")
}

// sleepScenario lays the player in the bed SETUP placed and wakes them with
// leave-bed. SLEEP names the bed's head block; SLEEP_HOLD holds the sleep
// for whole seconds (8 covers the 100 ticks a night skip takes);
// SLEEP_LEAVE=no ends the transcript with the player still lying.
//
//	SETUP="fill 3 199 0 9 199 4 stone|setblock 5 200 1 minecraft:red_bed[facing=south,part=head]|time set night" SLEEP="5 200 1" ...
func sleepScenario(c *protocol.Conn) {
	var x, y, z int32
	if n, err := fmt.Sscanf(os.Getenv("SLEEP"), "%d %d %d", &x, &y, &z); err != nil || n != 3 {
		fmt.Printf("PLAY scenario stopped: SLEEP=%q is not a head position\n", os.Getenv("SLEEP"))
		return
	}
	// East of the head looking south (yaw 0): vanilla wakes a sleeper of a
	// south-facing bed on its west side.
	teleportTo(c, float64(x)+1.5, float64(y), float64(z)-0.5)
	settleStep(c, float64(x)+1.5, float64(y), float64(z)-0.5)
	useItemOnBlock(c, x, y, z, 1)
	hold := 2500 * time.Millisecond
	if v, err := strconv.Atoi(os.Getenv("SLEEP_HOLD")); err == nil && v > 0 {
		hold = time.Duration(v) * time.Second
	}
	time.Sleep(hold)
	if os.Getenv("SLEEP_LEAVE") != "no" {
		leaveBed(c)
		time.Sleep(1200 * time.Millisecond)
	}
	fmt.Printf("PLAY scenario sleep done\n")
}

// leaveBed sends the Player Command a waking client sends, status zero, the
// leave bed the sleeper's own client asks for.
func leaveBed(c *protocol.Conn) {
	var b protocol.Buffer
	b.PutVarInt(0) // entity ID, which the server takes from the connection
	b.PutVarInt(protocol.PlayerCommandLeaveBed)
	b.PutVarInt(0) // jump boost, which only a horse uses
	if err := c.WritePacket(protocol.IDPlayPlayerCommand, b.Bytes()); err != nil {
		fmt.Printf("PLAY scenario stopped: %v\n", err)
		return
	}
	fmt.Printf("PLAY scenario leave-bed\n")
}

// eatSoundNames are the bite and burp sounds, plus the enchanting table's,
// named for the transcript.
var eatSoundNames = []string{"minecraft:entity.generic.eat", "minecraft:entity.player.burp",
	"minecraft:block.enchantment_table.use"}

// soundIDs is eatSoundNames by their minecraft:sound_event IDs, so a Sound
// Effect packet's ID can be read back into the sound it means.
var soundIDs = func() map[int32]string {
	byID := map[int32]string{}
	for _, name := range eatSoundNames {
		if id, ok := registry.SoundEventID(name); ok {
			byID[id] = name
		}
	}
	return byID
}()

// traceScenarioPacket, when set, sees every packet the read loop reads, so a
// scenario can show what the ordinary transcript would bury.
var traceScenarioPacket func(id int32, data []byte)

// traceSurvivalPackets traces health, inventory, entity events, sounds and
// the player's own metadata.
func traceSurvivalPackets() {
	traceScenarioPacket = func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySetHealth:
			r := protocol.NewReader(data)
			health, food, saturation := r.ReadF32(), r.ReadVarInt(), r.ReadF32()
			fmt.Printf("PLAY set-health health=%.2f food=%d saturation=%.2f\n", health, food, saturation)
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			window, state := r.ReadVarInt(), r.ReadVarInt()
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			count := int32(0)
			if ok {
				count = stack.Count
			}
			clickState.Store(state)
			fmt.Printf("PLAY container-slot window=%d state=%d slot=%d count=%d\n", window, state, slot, count)
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			window, state := r.ReadVarInt(), r.ReadVarInt()
			clickState.Store(state)
			n := r.ReadVarInt()
			var counts []int32
			for range n {
				if stack, ok := r.ReadSlot(); ok {
					counts = append(counts, stack.Count)
				} else {
					break
				}
			}
			fmt.Printf("PLAY container-content window=%d state=%d slots=%d counts=%v\n", window, state, n, counts)
		case protocol.IDPlayContainerProperty:
			r := protocol.NewReader(data)
			window, property, value := r.ReadVarInt(), r.ReadI16(), r.ReadI16()
			fmt.Printf("PLAY container-property window=%d property=%d value=%d\n", window, property, value)
		case protocol.IDPlayOpenScreen:
			r := protocol.NewReader(data)
			window, menu := r.ReadVarInt(), r.ReadVarInt()
			openWindow.Store(window)
			fmt.Printf("PLAY open-screen window=%d menu=%d\n", window, menu)
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			fmt.Printf("PLAY entity-event entity=%d status=%d\n", r.ReadI32(), r.ReadU8())
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			soundID := r.ReadVarInt()
			category := r.ReadVarInt()
			x, y, z := r.ReadI32(), r.ReadI32(), r.ReadI32()
			volume, pitch := r.ReadF32(), r.ReadF32()
			sound := ""
			if soundID != 0 {
				sound = soundIDs[soundID-1]
			}
			fmt.Printf("PLAY sound-effect sound=%q id=%d category=%d pos=(%.2f %.2f %.2f) volume=%.2f pitch=%.3f\n",
				sound, soundID-1, category, float64(x)/8, float64(y)/8, float64(z)/8, volume, pitch)
		case protocol.IDPlayEntityMetadata:
			if entityID := protocol.NewReader(data).ReadVarInt(); entityID == selfEntityID {
				fmt.Printf("PLAY self-metadata %x\n", data)
			}
		case protocol.IDPlaySystemChat:
			fmt.Printf("PLAY system-chat %x\n", data)
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			fmt.Printf("PLAY block-update (%d %d %d) state=%d\n", x, y, z, r.ReadVarInt())
		case protocol.IDPlaySectionBlocks:
			fmt.Printf("PLAY section-blocks %x\n", data)
		case protocol.IDPlayEntityAnimation:
			r := protocol.NewReader(data)
			fmt.Printf("PLAY animation entity=%d animation=%d\n", r.ReadVarInt(), r.ReadU8())
		case protocol.IDPlayUpdateTime:
			r := protocol.NewReader(data)
			age := r.ReadI64()
			r.ReadVarInt() // the clocks the packet carries, one here
			r.ReadVarInt() // the overworld clock's registry ID
			fmt.Printf("PLAY update-time age=%d time=%d\n", age, r.ReadVarLong())
		case protocol.IDPlayRespawn, protocol.IDPlayCombatDeath, protocol.IDPlaySyncPosition:
			fmt.Printf("PLAY packet 0x%02X (%d bytes)\n", id, len(data))
		}
	}
}

// The enchanting table scenario: a table ringed with bookshelves is opened,
// a blade and lapis clicked in and an enchantment asked for. The Open
// Screen, Set Container Content, the ten Set Container Property packets and
// the corrections are what this server has to match.
//
//	ENCHANT=1 ...                       # a table with a full bookshelf ring
//	ENCHANT_ITEM=diamond_pickaxe ...    # something other than a sword
//	ENCHANT_BUTTONS=0,2 ...             # ask for two enchantments in turn
//	ENCHANT_SHELVES=0 ...               # a table with nothing around it

// clickState is the state ID the server's window packets last carried, which
// every Click Container echoes. Written by the read loop, read by the
// scenario.
var clickState atomic.Int32

// openWindow is the ID of the window the server opened last, as the Open
// Screen named it.
var openWindow atomic.Int32

// enchantScenario sets the table up, opens it, places the item and the
// lapis, and presses the buttons, spaced so every answer arrives before the
// next step.
//
// ENCHANT_SETBLOCK builds with /setblock alone, for a server without /fill.
// Either way the player teleports out first so the chunks are loaded.
func enchantScenario(c *protocol.Conn) {
	// South of the table; the opening use is answered wherever the player
	// stands.
	teleportTo(c, 66.5, 200, 96.5)
	settleStep(c, 66.5, 200, 96.5)

	var commands []string
	if os.Getenv("ENCHANT_SETBLOCK") != "" {
		// The air between the table and every shelf: a shelf counts only
		// through a replaceable block.
		for _, mid := range [][3]int32{
			{-1, 0, -1}, {0, 0, -1}, {1, 0, -1}, {-1, 0, 0}, {1, 0, 0},
			{-1, 0, 1}, {0, 0, 1}, {1, 0, 1},
			{-1, 1, -1}, {0, 1, -1}, {1, 1, -1}, {-1, 1, 0}, {1, 1, 0},
			{-1, 1, 1}, {0, 1, 1}, {1, 1, 1},
		} {
			commands = append(commands,
				fmt.Sprintf("setblock %d %d %d minecraft:air", 66+mid[0], 200+mid[1], 100+mid[2]))
		}
		commands = append(commands, "setblock 66 200 100 minecraft:enchanting_table")
		if os.Getenv("ENCHANT_SHELVES") != "0" {
			for _, off := range shelfOffsets {
				commands = append(commands,
					fmt.Sprintf("setblock %d %d %d minecraft:bookshelf", 66+off[0], 200+off[1], 100+off[2]))
			}
		}
	} else {
		// Air over the ground, the table, and none or all of the 32-shelf
		// ring (16 positions on two levels; the cost caps at 15).
		commands = append(commands,
			"fill 62 200 96 70 205 104 air",
			"setblock 66 200 100 minecraft:enchanting_table",
		)
		if os.Getenv("ENCHANT_SHELVES") != "0" {
			commands = append(commands,
				"fill 64 200 98 68 201 98 minecraft:bookshelf",
				"fill 64 200 102 68 201 102 minecraft:bookshelf",
				"fill 64 200 99 64 201 101 minecraft:bookshelf",
				"fill 68 200 99 68 201 101 minecraft:bookshelf",
			)
		}
	}
	commands = append(commands,
		"xp add @s 40 levels",
		"clear @s",
		"give @s minecraft:"+itemOr("diamond_sword")+" 1",
		"give @s minecraft:lapis_lazuli 16",
	)
	for _, command := range commands {
		scenarioCommand(c, command)
		time.Sleep(400 * time.Millisecond)
	}

	// Open the table with a use on its north face.
	useItemOnBlock(c, 66, 200, 100, 2)
	time.Sleep(1200 * time.Millisecond)

	// The item and lapis are in hotbar slots 1-2, window slots 29 and 30:
	// left-click each into place. A claimless click is answered with
	// corrections for everything it changed.
	clickContainer(c, 29, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 0, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 30, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 1, 0, 0)
	time.Sleep(800 * time.Millisecond)

	// Each press costs lapis and levels and rerolls the seed, so each
	// answer shows fresh properties.
	for one := range strings.SplitSeq(buttonsOr("0"), ",") {
		button, err := strconv.Atoi(strings.TrimSpace(one))
		if err != nil {
			fmt.Printf("PLAY enchant: bad button %q\n", one)
			continue
		}
		sendContainerButton(c, int32(button))
		time.Sleep(800 * time.Millisecond)
	}

	// Leave the window with the world tidied the way it was found.
	scenarioCommand(c, "setblock 66 200 100 minecraft:air")
	scenarioCommand(c, "setblock 66 201 100 minecraft:air")
	time.Sleep(1500 * time.Millisecond)
	fmt.Printf("PLAY scenario enchant done\n")
}

// shelfOffsets is the full bookshelf ring: positions two out from the table
// on its level and one above.
var shelfOffsets = func() [][3]int32 {
	var offsets [][3]int32
	for dy := int32(0); dy <= 1; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if dx*dx != 4 && dz*dz != 4 {
					continue
				}
				offsets = append(offsets, [3]int32{dx, dy, dz})
			}
		}
	}
	return offsets
}()

// itemOr is the item ENCHANT_ITEM names, or the one given.
func itemOr(fallback string) string {
	if item := strings.TrimSpace(os.Getenv("ENCHANT_ITEM")); item != "" {
		return item
	}
	return fallback
}

// buttonsOr is the buttons ENCHANT_BUTTONS names, or the one given.
func buttonsOr(fallback string) string {
	if buttons := os.Getenv("ENCHANT_BUTTONS"); buttons != "" {
		return buttons
	}
	return fallback
}

// useItemOnBlock right-clicks the middle of a face: 1 top, 2 north, 3 south,
// 4 west, 5 east.
func useItemOnBlock(c *protocol.Conn, x, y, z int32, face int32) {
	var b protocol.Buffer
	b.PutVarInt(protocol.HandMain)
	b.PutPosition(x, y, z)
	b.PutVarInt(face)
	b.PutF32(0.5)
	b.PutF32(0.5)
	b.PutF32(0.5)
	b.PutBool(false) // the player's head is not inside a block
	b.PutBool(false) // no world border was hit
	b.PutVarInt(1)   // block change sequence
	must(c.WritePacket(protocol.IDPlayUseItemOn, b.Bytes()))
	fmt.Printf("PLAY scenario use-item-on (%d %d %d) face=%d\n", x, y, z, face)
}

// clickContainer clicks one slot of the open window with empty claims and an
// empty cursor: this client predicts nothing, so the transcript is the
// server's answers alone.
func clickContainer(c *protocol.Conn, slot int32, button int8, mode int32) {
	var b protocol.Buffer
	b.PutVarInt(openWindow.Load())
	b.PutVarInt(clickState.Load())
	b.PutI16(int16(slot))
	b.PutI8(button)
	b.PutVarInt(mode)
	b.PutVarInt(0) // no claimed slot changes
	b.PutU8(0)     // carried item: nothing
	must(c.WritePacket(protocol.IDPlayClickContainer, b.Bytes()))
	fmt.Printf("PLAY scenario click slot=%d button=%d mode=%d state=%d\n",
		slot, button, mode, clickState.Load())
}

// sendContainerButton presses one of the window's buttons, the packet an
// enchantment's yes button sends.
func sendContainerButton(c *protocol.Conn, button int32) {
	var b protocol.Buffer
	b.PutVarInt(openWindow.Load())
	b.PutVarInt(button)
	must(c.WritePacket(protocol.IDPlayContainerButtonClickIn, b.Bytes()))
	fmt.Printf("PLAY scenario container-button window=%d button=%d\n", openWindow.Load(), button)
}

// lidScenario opens and closes a container block twice; the Block Actions
// (lid open at one viewer, closed at none) and the sounds are compared.
//
//	LID=chest ...        # the block set down: chest, barrel, shulker_box,
//	                     # ender_chest, trapped_chest
func lidScenario(c *protocol.Conn) {
	kind := strings.TrimSpace(os.Getenv("LID"))
	if kind == "" {
		fmt.Printf("PLAY scenario stopped: LID names no block\n")
		return
	}
	// The lid's own trace: the Block Action a container's lid rides, over
	// the survival packets the ordinary scenario trace carries.
	prev := traceScenarioPacket
	traceScenarioPacket = func(id int32, data []byte) {
		if id == protocol.IDPlayBlockAction {
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			fmt.Printf("PLAY block-action (%d %d %d) action=%d param=%d block=%d\n",
				x, y, z, r.ReadU8(), r.ReadU8(), r.ReadVarInt())
			return
		}
		if prev != nil {
			prev(id, data)
		}
	}
	defer func() { traceScenarioPacket = prev }()

	// The pocket of world and the container in it, the player standing
	// south of the block the way the enchant scenario does.
	teleportTo(c, 66.5, 200, 96.5)
	settleStep(c, 66.5, 200, 96.5)
	for _, command := range []string{
		"fill 62 200 96 70 205 104 air",
		"setblock 66 200 100 minecraft:" + kind,
	} {
		scenarioCommand(c, command)
		time.Sleep(500 * time.Millisecond)
	}

	// Open and close twice, spaced far enough apart that each answer has
	// arrived before the next step: the lid's count going one and none.
	for range 2 {
		useItemOnBlock(c, 66, 200, 100, 2)
		time.Sleep(1500 * time.Millisecond)
		var b protocol.Buffer
		b.PutVarInt(openWindow.Load())
		must(c.WritePacket(protocol.IDPlayCloseContainerIn, b.Bytes()))
		fmt.Printf("PLAY scenario close window=%d\n", openWindow.Load())
		time.Sleep(1500 * time.Millisecond)
	}

	// Leave the world the way it was found.
	scenarioCommand(c, "setblock 66 200 100 minecraft:air")
	time.Sleep(1200 * time.Millisecond)
	fmt.Printf("PLAY scenario lid done\n")
}

// pistonScenario places and removes a redstone block beside a sticky
// piston, one push and one pull. The Block Action, Block Updates, Block
// Entity Data, sounds and landing updates are compared in arrival order.
//
//	PISTON=1 ...        # one push and one pull
//	PISTON=door ...     # two facing pistons, the door geometry, after the first
func pistonScenario(c *protocol.Conn) {
	// Over the survival trace: block updates named by state, block actions
	// and block entity data.
	prev := traceScenarioPacket
	traceScenarioPacket = func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockAction:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			fmt.Printf("PLAY block-action (%d %d %d) action=%d param=%d block=%d\n",
				x, y, z, r.ReadU8(), r.ReadU8(), r.ReadVarInt())
			return
		case protocol.IDPlayBlockEntityData:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			fmt.Printf("PLAY block-entity-data (%d %d %d) type=%d nbt=%s\n",
				x, y, z, r.ReadVarInt(), readNBT(r))
			return
		case protocol.IDPlaySectionBlocks:
			// Section position packed in a long, then each changed cell
			// as a varlong: the state above the packed relative offset.
			r := protocol.NewReader(data)
			packed := r.ReadI64()
			chunkX, chunkZ := int32(packed>>42)&0x3FFFFF, int32(packed>>20)&0x3FFFFF
			sectionY := int32(packed & 0xFFFFF)
			for range r.ReadVarInt() {
				entry := r.ReadVarLong()
				state, cell := uint16(entry>>12), entry&0xFFF
				fmt.Printf("PLAY batch-block (%d %d %d) state=%d %s\n",
					chunkX*16+int32(cell>>8), sectionY*16+int32(cell&0xF),
					chunkZ*16+int32(cell>>4&0xF), state, stateNameOf(state))
			}
			fmt.Printf("PLAY batch-block leftover=%d err=%v\n", r.Remaining(), r.Err())
			return
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			state := r.ReadVarInt()
			fmt.Printf("PLAY block-update (%d %d %d) state=%d %s\n", x, y, z, state, stateNameOf(uint16(state)))
			return
		}
		if prev != nil {
			prev(id, data)
		}
	}
	defer func() { traceScenarioPacket = prev }()

	// South of the row, out of the way. A movement packet every few
	// hundred milliseconds keeps vanilla's anti-flight check quiet.
	teleportTo(c, 66.5, 200, 103.5)
	settleStep(c, 66.5, 200, 103.5)
	stopBeating := make(chan struct{})
	defer close(stopBeating)
	go func() {
		tick := time.NewTicker(400 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopBeating:
				return
			case <-tick.C:
				movePacket(c, 66.5, 200, 103.5, true)
			}
		}
	}()
	for _, command := range []string{
		"fill 63 199 97 69 199 103 minecraft:stone",
		"fill 63 200 97 69 205 103 minecraft:air",
		"setblock 66 200 100 minecraft:sticky_piston[facing=east]",
		"setblock 67 200 100 minecraft:stone",
	} {
		scenarioCommand(c, command)
		time.Sleep(500 * time.Millisecond)
	}

	// The push: the redstone block beside the piston is the power, its
	// own placement the block update the move answers.
	scenarioCommand(c, "setblock 66 200 101 minecraft:redstone_block")
	time.Sleep(1500 * time.Millisecond)

	// The pull: taking the power away.
	scenarioCommand(c, "setblock 66 200 101 minecraft:air")
	time.Sleep(1500 * time.Millisecond)

	if os.Getenv("PISTON") == "door" {
		// Two sticky pistons facing each other, powered together: a
		// flush piston door, several moves in one event tick.
		scenarioCommand(c, "fill 63 200 97 69 205 103 minecraft:air")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 63 200 100 minecraft:sticky_piston[facing=east]")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 69 200 100 minecraft:sticky_piston[facing=west]")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 64 200 100 minecraft:stone")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 65 200 100 minecraft:stone")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 63 200 99 minecraft:redstone_block")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 69 200 99 minecraft:redstone_block")
		time.Sleep(1500 * time.Millisecond)
		scenarioCommand(c, "setblock 63 200 99 minecraft:air")
		time.Sleep(700 * time.Millisecond)
		scenarioCommand(c, "setblock 69 200 99 minecraft:air")
		time.Sleep(1500 * time.Millisecond)
	}
	fmt.Printf("PLAY scenario piston done\n")
}

// stateNameOf renders a block state ID as /setblock writes it.
func stateNameOf(state uint16) string {
	name, raw, ok := registry.BlockStateName(state)
	if !ok {
		return "<unknown>"
	}
	return name + "[" + raw + "]"
}

// anvilScenario opens an anvil, places two inputs, renames and takes the
// result. The Open Screen, slot contents, cost property, corrections, level
// spend and block damage are what this server's anvil has to match.
//
//	ANVIL=1 ...                                    # an anvil on open ground
//	ANVIL_GIVE1=diamond_sword[minecraft:damage=100] 1  # the first input
//	ANVIL_GIVE2=diamond 2                          # the second input
//	ANVIL_RENAME=Sharp%20Sword                     # the text the rename carries
//	ANVIL_SHIFT=1                                  # shift-take the result instead
//	ANVIL_NOTAKE=1                                 # leave the inputs in place
func anvilScenario(c *protocol.Conn) {
	// South of the anvil; the opening use is answered wherever the player
	// stands.
	teleportTo(c, 66.5, 200, 96.5)
	settleStep(c, 66.5, 200, 96.5)

	// The scenarios run high above the ground, so the pocket brings its own
	// platform. The mode is set outright because a previous scenario's
	// player file would otherwise decide it, and only survival spends and
	// wears (ANVIL_CREATIVE asks for creative).
	mode := "survival"
	if os.Getenv("ANVIL_CREATIVE") != "" {
		mode = "creative"
	}
	commands := []string{
		"fill 62 199 96 70 199 104 minecraft:stone",
		"fill 62 200 96 70 205 104 air",
		"setblock 66 200 100 minecraft:anvil",
		"gamemode " + mode,
		"xp add @s 40 levels",
		"clear @s",
		"give @s minecraft:" + giveOr("ANVIL_GIVE1", "diamond_sword[minecraft:damage=100] 1"),
		"give @s minecraft:" + giveOr("ANVIL_GIVE2", "diamond 2"),
	}
	for _, command := range commands {
		scenarioCommand(c, command)
		time.Sleep(400 * time.Millisecond)
	}

	// Open the anvil with a use on its north face.
	useItemOnBlock(c, 66, 200, 100, 2)
	time.Sleep(1200 * time.Millisecond)

	// The inputs are in hotbar slots 1-2, window slots 30 and 31.
	clickContainer(c, 30, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 0, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 31, 0, 0)
	time.Sleep(600 * time.Millisecond)
	clickContainer(c, 1, 0, 0)
	time.Sleep(800 * time.Millisecond)

	// The name, as the Rename Item packet carries it: the answer is the
	// Set Container Slot of the result and the cost property beside it.
	if rename := os.Getenv("ANVIL_RENAME"); rename != "" {
		sendRenameItem(c, strings.ReplaceAll(rename, "%20", " "))
		time.Sleep(800 * time.Millisecond)
	}

	// The take: a left-click out of slot 2, or a shift-take (quick-move)
	// with ANVIL_SHIFT; ANVIL_NOTAKE skips it.
	if os.Getenv("ANVIL_NOTAKE") == "" {
		if os.Getenv("ANVIL_SHIFT") != "" {
			clickContainer(c, 2, 0, 2)
		} else {
			clickContainer(c, 2, 0, 0)
		}
		time.Sleep(1500 * time.Millisecond)
		if os.Getenv("ANVIL_XPCHECK") != "" {
			// The level spend is read back from the next add's answer:
			// vanilla times its own experience packet, not the click.
			scenarioCommand(c, "xp add @s 1 levels")
			time.Sleep(800 * time.Millisecond)
		}
	}

	// Tidy up: remove the anvil, and put the player back on the ground
	// before the platform goes.
	scenarioCommand(c, "setblock 66 200 100 minecraft:air")
	scenarioCommand(c, "setblock 66 201 100 minecraft:air")
	scenarioCommand(c, "tp @s 0.5 -60.0 0.5")
	scenarioCommand(c, "fill 62 199 96 70 199 104 air")
	time.Sleep(1500 * time.Millisecond)
	fmt.Printf("PLAY scenario anvil done\n")
}

// giveOr is the give arguments the named environment variable holds, or the
// ones given.
func giveOr(name, fallback string) string {
	if give := strings.TrimSpace(os.Getenv(name)); give != "" {
		return give
	}
	return fallback
}

// sendRenameItem sends the Rename Item packet the anvil's name field rides
// on, with the text a client would have typed.
func sendRenameItem(c *protocol.Conn, text string) {
	var b protocol.Buffer
	b.PutString(text)
	must(c.WritePacket(protocol.IDPlayRenameItemIn, b.Bytes()))
	fmt.Printf("PLAY scenario rename-item %q\n", text)
}
