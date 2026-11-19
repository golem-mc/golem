package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// describeSystemChat parses a message the server sent on its own behalf, such
// as the announcement that a player joined.
func describeSystemChat(data []byte) {
	r := protocol.NewReader(data)
	content, raw := readComponent(data, r)
	overlay := r.ReadBool()
	fmt.Printf("PLAY system-chat content=%s overlay=%v nbt=%x leftover=%d err=%v\n",
		content, overlay, raw, r.Remaining(), r.Err())
}

// describeDisguisedChat parses an unsigned chat message, the form this
// server relays console and command broadcasts in.
func describeDisguisedChat(data []byte) {
	r := protocol.NewReader(data)
	message, raw := readComponent(data, r)
	chatType := r.ReadVarInt()
	sender, _ := readComponent(data, r)
	target := "<none>"
	if r.ReadBool() {
		target, _ = readComponent(data, r)
	}
	fmt.Printf("PLAY disguised-chat message=%s chatType=%d sender=%s target=%s nbt=%x leftover=%d err=%v\n",
		message, chatType, sender, target, raw, r.Remaining(), r.Err())
}

// describePlayerChat parses the Player Chat Message player chat and direct
// messages are relayed in.
func describePlayerChat(data []byte) {
	r := protocol.NewReader(data)
	globalIndex := r.ReadVarInt()
	sender := r.ReadUUID()
	index := r.ReadVarInt()
	signed := r.ReadBool()
	if signed {
		skip(r, 256) // the signature, which is not length-prefixed
	}
	message := r.ReadString(256)
	r.ReadI64() // timestamp
	r.ReadI64() // salt
	for range r.ReadVarInt() {
		if r.ReadVarInt() == 0 {
			skip(r, 256) // a previous message's signature
		}
	}
	unsigned := "<none>"
	if r.ReadBool() {
		unsigned, _ = readComponent(data, r)
	}
	filter := r.ReadVarInt()
	if filter == 2 { // partially filtered
		for range r.ReadVarInt() {
			r.ReadI64()
		}
	}
	chatType := r.ReadVarInt()
	senderName, _ := readComponent(data, r)
	target := "<none>"
	if r.ReadBool() {
		target, _ = readComponent(data, r)
	}

	fmt.Printf("PLAY player-chat global=%d sender=%s index=%d signed=%v message=%q unsigned=%s "+
		"filter=%d chatType=%d senderName=%s target=%s leftover=%d err=%v\n",
		globalIndex, sender, index, signed, message, unsigned,
		filter, chatType, senderName, target, r.Remaining(), r.Err())
}

// skip consumes n bytes of a payload that carries no length of its own.
func skip(r *protocol.Reader, n int) {
	for range n {
		r.ReadU8()
	}
}

// describeCommands parses the command tree, every field and parser property
// included, so the leftover count checks the node format and the parser
// table rather than just the first fields.
func describeCommands(data []byte) {
	r := protocol.NewReader(data)
	count := r.ReadVarInt()
	fmt.Printf("PLAY commands nodes=%d\n", count)
nodes:
	for i := range count {
		if r.Err() != nil {
			break
		}
		flags := r.ReadU8()
		childCount := r.ReadVarInt()
		children := make([]int32, 0, childCount)
		for range childCount {
			children = append(children, r.ReadVarInt())
		}
		redirect := int32(-1)
		if flags&0x08 != 0 { // has redirect
			redirect = r.ReadVarInt()
		}
		var name, props string
		var parser int32
		switch flags & 0x03 {
		case 1: // literal
			name = r.ReadString(32767)
		case 2: // argument
			name = r.ReadString(32767)
			parser = r.ReadVarInt()
			var ok bool
			props, ok = readParserProperties(r, parser)
			if !ok {
				// An unknown parser's properties have no known end, so the
				// rest of the packet is left unread.
				fmt.Printf("  [%d] argument name=%q parser=%d properties=unreadable\n", i, name, parser)
				r.Rest()
				break nodes
			}
		}

		typ := "root"
		switch flags & 0x03 {
		case 1:
			typ = "literal"
		case 2:
			typ = "argument"
		}
		line := fmt.Sprintf("  [%d] %s name=%q executable=%v children=%v",
			i, typ, name, flags&0x04 != 0, children)
		if redirect != -1 {
			line += fmt.Sprintf(" redirect=%d", redirect)
		}
		if flags&0x03 == 2 {
			line += fmt.Sprintf(" parser=%s", commandParserNames[parser])
		}
		if props != "" {
			line += " " + props
		}
		if flags&0x10 != 0 { // has suggestions type
			line += fmt.Sprintf(" suggests=%s", r.ReadString(32767))
		}
		fmt.Println(line)
	}
	rootIndex := r.ReadVarInt()
	fmt.Printf("PLAY commands root=%d leftover=%d err=%v\n", rootIndex, r.Remaining(), r.Err())
}

// describeCommandSuggestions parses a Command Suggestions Response, which
// carries the tab completions the server offered.
func describeCommandSuggestions(data []byte) {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	start := r.ReadVarInt()
	length := r.ReadVarInt()
	count := r.ReadVarInt()
	matches := make([]string, 0, count)
	for range count {
		matches = append(matches, r.ReadString(32767))
		if r.ReadBool() { // tooltip present
			readComponent(data, r) // skip the NBT text component
		}
	}
	fmt.Printf("PLAY command-suggestions id=%d start=%d length=%d matches=%v leftover=%d err=%v\n",
		id, start, length, matches, r.Remaining(), r.Err())
}

// describeLoginPlay parses Login (play) with the field order this repository
// implements. Leftover bytes mean the layout is wrong.
func describeLoginPlay(data []byte) {
	r := protocol.NewReader(data)
	entityID := r.ReadI32()
	selfEntityID = entityID
	hardcore := r.ReadBool()
	dimensions := r.ReadVarInt()
	for range dimensions {
		r.ReadString(32767)
	}
	maxPlayers := r.ReadVarInt()
	viewDistance := r.ReadVarInt()
	simulationDistance := r.ReadVarInt()
	r.ReadBool() // reduced debug info
	r.ReadBool() // respawn screen
	r.ReadBool() // limited crafting
	dimensionType := r.ReadVarInt()
	dimensionName := r.ReadString(32767)
	r.ReadI64() // hashed seed
	gameMode := uint8(r.ReadVarInt())
	prevGameMode := int8(r.ReadVarInt() - 1) // optional var int: 0 is absent
	isDebug := r.ReadBool()
	isFlat := r.ReadBool()
	if r.ReadBool() {
		r.ReadString(32767)
		r.ReadI64()
	}
	portalCooldown := r.ReadVarInt()
	seaLevel := r.ReadVarInt()
	onlineMode := r.ReadBool()
	secureChat := r.ReadBool()

	fmt.Printf("PLAY login entity=%d hardcore=%v dimensions=%d maxPlayers=%d view=%d sim=%d "+
		"dimType=%d dimName=%s gameMode=%d prev=%d debug=%v flat=%v portalCooldown=%d seaLevel=%d "+
		"online=%v secureChat=%v leftover=%d err=%v\n",
		entityID, hardcore, dimensions, maxPlayers, viewDistance, simulationDistance,
		dimensionType, dimensionName, gameMode, prevGameMode, isDebug, isFlat,
		portalCooldown, seaLevel, onlineMode, secureChat, r.Remaining(), r.Err())
}

// describeJoinPacket parses the smaller join packets with the layouts this// repository writes them with, so that a mismatch shows up as leftover bytes.
func describeJoinPacket(id int32, data []byte) {
	r := protocol.NewReader(data)
	name := ""

	switch id {
	case protocol.IDPlaySetExperience:
		// Its own line, since the fields are the whole of the packet and
		// the shared tail below would only repeat them.
		progress := r.ReadF32()
		level := r.ReadVarInt()
		total := r.ReadVarInt()
		fmt.Printf("PLAY set-experience progress=%g level=%d total=%d leftover=%d err=%v\n",
			progress, level, total, r.Remaining(), r.Err())
		return

	case protocol.IDPlayPlayerInfoUpdate:
		name = "player-info-update"
		actions := r.ReadU8()
		players := r.ReadVarInt()
		fmt.Printf("PLAY player-info actions=%#02x players=%d\n", actions, players)
		for range players {
			fmt.Printf("  uuid=%s\n", r.ReadUUID())
			if actions&0x01 != 0 { // add player
				fmt.Printf("  name=%s\n", r.ReadString(16))
				props := r.SkipProfileProperties()
				fmt.Printf("  properties=%d\n", props)
			}
			if actions&0x02 != 0 { // initialize chat
				if r.ReadBool() {
					r.ReadUUID()
					r.ReadI64()
					r.ReadBytes(512)
					r.ReadBytes(4096)
				}
			}
			if actions&0x04 != 0 {
				fmt.Printf("  gameMode=%d\n", r.ReadVarInt())
			}
			if actions&0x08 != 0 {
				fmt.Printf("  listed=%v\n", r.ReadBool())
			}
			if actions&0x10 != 0 {
				fmt.Printf("  latency=%d\n", r.ReadVarInt())
			}
			if actions&0x20 != 0 { // display name
				if r.ReadBool() {
					r.Rest() // NBT, which we cannot skip
					return
				}
			}
			if actions&0x40 != 0 {
				fmt.Printf("  priority=%d\n", r.ReadVarInt())
			}
			if actions&0x80 != 0 {
				fmt.Printf("  hat=%v\n", r.ReadBool())
			}
		}

	case protocol.IDPlaySpawnPosition:
		name = "spawn-position"
		dim := r.ReadString(32767)
		pos := r.ReadI64()
		yaw, pitch := r.ReadF32(), r.ReadF32()
		fmt.Printf("PLAY spawn-position dim=%s pos=%#x yaw=%.1f pitch=%.1f\n", dim, pos, yaw, pitch)

	case protocol.IDPlayDifficulty:
		name = "difficulty"
		fmt.Printf("PLAY difficulty=%d locked=%v\n", r.ReadU8(), r.ReadBool())

	case protocol.IDPlayAbilities:
		name = "abilities"
		fmt.Printf("PLAY abilities flags=%#02x flySpeed=%.3f fov=%.3f\n",
			r.ReadI8(), r.ReadF32(), r.ReadF32())

	case protocol.IDPlayGameEvent:
		name = "game-event"
		fmt.Printf("PLAY game-event event=%d value=%v\n", r.ReadU8(), r.ReadF32())

	case protocol.IDPlayUpdateTime:
		name = "update-time"
		worldAge := r.ReadI64()
		clocks := r.ReadVarInt()
		fmt.Printf("PLAY update-time worldAge=%d clocks=%d\n", worldAge, clocks)
		for range clocks {
			clockID := r.ReadVarInt()
			time := r.ReadVarLong()
			frac := r.ReadF32()
			rate := r.ReadF32()
			fmt.Printf("  clock=%d time=%d frac=%.3f rate=%.3f\n", clockID, time, frac, rate)
		}

	case protocol.IDPlayCenterChunk:
		name = "center-chunk"
		fmt.Printf("PLAY center-chunk x=%d z=%d\n", r.ReadVarInt(), r.ReadVarInt())

	case protocol.IDPlayMapData:
		name = "map-item-data"
		id := r.ReadVarInt()
		scale := r.ReadU8()
		locked := r.ReadBool()
		icons := "none"
		if r.ReadBool() {
			count := r.ReadVarInt()
			icons = fmt.Sprintf("x%d", count)
			for range count {
				r.ReadVarInt() // the icon's type
				r.ReadU8()     // where it sits on the map, x and z
				r.ReadU8()
				r.ReadU8() // the direction it points
				if r.ReadBool() {
					protocol.ReadText(r) // a display name, network NBT
				}
			}
		}
		patch := "none"
		if columns := r.ReadU8(); columns > 0 {
			rows := r.ReadU8()
			x, z := r.ReadU8(), r.ReadU8()
			n := r.ReadVarInt()
			r.Skip(int(n))
			patch = fmt.Sprintf("%dx%d at (%d, %d), %d bytes", columns, rows, x, z, n)
		}
		fmt.Printf("PLAY map-item-data id=%d scale=%d locked=%v icons=%s patch=%s\n",
			id, scale, locked, icons, patch)

	case protocol.IDPlayUnloadChunk:
		name = "unload-chunk"
		// Z comes first: the client reads both coordinates as one long.
		z, x := r.ReadI32(), r.ReadI32()
		fmt.Printf("PLAY unload-chunk x=%d z=%d\n", x, z)

	case protocol.IDPlayServerData:
		name = "server-data"
		motd, _ := readComponent(data, r)
		icon := "<none>"
		if r.ReadBool() {
			n := r.ReadVarInt()
			icon = fmt.Sprintf("%d bytes", n)
			for range n {
				r.ReadU8()
			}
		}
		fmt.Printf("PLAY server-data motd=%s icon=%s leftover=%d err=%v\n",
			motd, icon, r.Remaining(), r.Err())

	case protocol.IDPlayInitializeBorder:
		name = "initialize-border"
		centerX, centerZ := r.ReadF64(), r.ReadF64()
		oldSize, newSize := r.ReadF64(), r.ReadF64()
		speed := r.ReadVarLong()
		portalBoundary := r.ReadVarInt()
		warningBlocks := r.ReadVarInt()
		warningTime := r.ReadVarInt()
		fmt.Printf("PLAY initialize-border center=(%.1f %.1f) old=%.1f new=%.1f speed=%d "+
			"portalBoundary=%d warningBlocks=%d warningTime=%d leftover=%d err=%v\n",
			centerX, centerZ, oldSize, newSize, speed, portalBoundary,
			warningBlocks, warningTime, r.Remaining(), r.Err())

	case protocol.IDPlayUpdateRecipes:
		name = "update-recipes"
		sets := r.ReadVarInt()
		var setNames []string
		for range sets {
			setNames = append(setNames, r.ReadString(32767))
			for range r.ReadVarInt() {
				r.ReadVarInt()
			}
		}
		recipes := r.ReadVarInt()
		first := "<none>"
		for i := range recipes {
			input := readIDSet(r)
			result, ok := readSlotDisplay(r)
			if !ok || r.Err() != nil {
				break
			}
			if i == 0 {
				first = fmt.Sprintf("input=%s result=%s", input, result)
			}
		}
		fmt.Printf("PLAY update-recipes sets=%d %v stonecutter=%d first{%s} leftover=%d err=%v\n",
			sets, setNames, recipes, first, r.Remaining(), r.Err())

	case protocol.IDPlayRecipeBookAdd:
		name = "recipe-book-add"
		entries := r.ReadVarInt()
		shown := 0
		for range entries {
			id := r.ReadVarInt()
			display, ok := readRecipeDisplay(r)
			group := "<none>"
			if ok {
				if n := r.ReadVarInt(); n != 0 {
					group = fmt.Sprintf("%d", n-1)
				}
				category := r.ReadVarInt()
				reqs := "<none>"
				if r.ReadBool() {
					count := r.ReadVarInt()
					for range count {
						readIDSet(r)
					}
					reqs = fmt.Sprintf("%d", count)
				}
				flags := r.ReadU8()
				if shown < 4 {
					fmt.Printf("  recipe id=%d display=%s group=%s category=%d reqs=%s flags=%#02x\n",
						id, display, group, category, reqs, flags)
					shown++
				}
			}
			if r.Err() != nil {
				break
			}
		}
		replace := r.ReadBool()
		fmt.Printf("PLAY recipe-book-add entries=%d replace=%v leftover=%d err=%v\n",
			entries, replace, r.Remaining(), r.Err())

	case protocol.IDPlayRecipeBookSettings:
		name = "recipe-book-settings"
		open := [4]bool{r.ReadBool(), r.ReadBool(), r.ReadBool(), r.ReadBool()}
		filter := [4]bool{r.ReadBool(), r.ReadBool(), r.ReadBool(), r.ReadBool()}
		fmt.Printf("PLAY recipe-book-settings open=%v filter=%v leftover=%d err=%v\n",
			open, filter, r.Remaining(), r.Err())

	case protocol.IDPlayTickingState:
		name = "ticking-state"
		fmt.Printf("PLAY ticking-state tickRate=%.1f frozen=%v leftover=%d err=%v\n",
			r.ReadF32(), r.ReadBool(), r.Remaining(), r.Err())

	case protocol.IDPlayTickingStep:
		name = "ticking-step"
		fmt.Printf("PLAY ticking-step steps=%d leftover=%d err=%v\n",
			r.ReadVarInt(), r.Remaining(), r.Err())
	}

	fmt.Printf("PLAY %s leftover=%d err=%v\n", name, r.Remaining(), r.Err())
}

// readIDSet reads a set of registry IDs, which is either a tag's name or an
// inline list whose length the type carries.
func readIDSet(r *protocol.Reader) string {
	if t := r.ReadVarInt(); t == 0 {
		return "tag " + r.ReadString(32767)
	} else if t == 2 {
		return fmt.Sprintf("item %d", r.ReadVarInt())
	} else {
		for range t - 1 {
			r.ReadVarInt()
		}
		return fmt.Sprintf("ids x%d", t-1)
	}
}

// readSlotDisplay reads one ingredient or result display, reporting whether
// it could: an unknown type has no known length, so the packet cannot be
// read past it.
func readSlotDisplay(r *protocol.Reader) (string, bool) {
	switch t := r.ReadVarInt(); t {
	case 0: // empty
		return "empty", true
	case 1: // any fuel
		return "fuel", true
	case 2, 3: // potion or component-filtered display
		source, ok := readSlotDisplay(r)
		if t == 3 {
			return fmt.Sprintf("component(%s,%d)", source, r.ReadVarInt()), ok
		}
		return "potion(" + source + ")", ok
	case 4: // item
		return fmt.Sprintf("item %d", r.ReadVarInt()), true
	case 5: // item stack
		stack, ok := r.ReadItemTemplate()
		return fmt.Sprintf("stack(item=%d count=%d stew=%v)", stack.ItemID, stack.Count, stack.SuspiciousStewEffectsOf()), ok
	case 6: // tag
		return readIDSet(r), true
	case 7, 8, 9: // dye demo, smithing trim demo or crafting remainder
		first, ok1 := readSlotDisplay(r)
		second, ok2 := readSlotDisplay(r)
		if t == 8 {
			return fmt.Sprintf("trim(%s,%s,%d)", first, second, r.ReadVarInt()), ok1 && ok2
		}
		if t == 9 {
			return fmt.Sprintf("remainder(%s,%s)", first, second), ok1 && ok2
		}
		return fmt.Sprintf("dyed(%s,%s)", first, second), ok1 && ok2
	case 10: // composite
		n := r.ReadVarInt()
		parts := make([]string, 0, n)
		for range n {
			part, ok := readSlotDisplay(r)
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		return "composite(" + strings.Join(parts, " ") + ")", true
	default:
		return fmt.Sprintf("unreadable type %d", t), false
	}
}

// readRecipeDisplay reads one recipe display, dispatched on the
// minecraft:recipe_display registry.
func readRecipeDisplay(r *protocol.Reader) (string, bool) {
	read := func() (string, bool) { return readSlotDisplay(r) }
	switch t := r.ReadVarInt(); t {
	case 0: // crafting shapeless
		n := r.ReadVarInt()
		parts := make([]string, 0, n)
		for range n {
			part, ok := read()
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		result, ok1 := read()
		station, ok2 := read()
		if !ok1 || !ok2 {
			return "", false
		}
		return fmt.Sprintf("shapeless(%s)=%s@%s", strings.Join(parts, " "), result, station), true
	case 1: // crafting shaped
		width, height := r.ReadVarInt(), r.ReadVarInt()
		n := r.ReadVarInt()
		parts := make([]string, 0, n)
		for range n {
			part, ok := read()
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		result, ok1 := read()
		station, ok2 := read()
		if !ok1 || !ok2 {
			return "", false
		}
		return fmt.Sprintf("shaped %dx%d(%s)=%s@%s", width, height, strings.Join(parts, " "), result, station), true
	case 2, 3: // furnace and stonecutter
		input, ok1 := read()
		if t == 2 {
			_, ok := read() // fuel
			ok1 = ok1 && ok
		}
		result, ok2 := read()
		station, ok3 := read()
		if !ok1 || !ok2 || !ok3 {
			return "", false
		}
		kind := "furnace"
		if t == 3 {
			kind = "stonecutter"
		} else {
			r.ReadVarInt() // duration
			r.ReadF32()    // experience
		}
		return fmt.Sprintf("%s(%s)=%s@%s", kind, input, result, station), true
	case 4: // smithing: template, base and addition in, one result out
		template, ok1 := read()
		base, ok2 := read()
		addition, ok3 := read()
		result, ok4 := read()
		station, ok5 := read()
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			return "", false
		}
		return fmt.Sprintf("smithing(%s+%s+%s)=%s@%s", template, base, addition, result, station), true
	default:
		return fmt.Sprintf("unreadable type %d", t), false
	}
}

// lastOrbID is the experience orb the last Spawn Entity put in the world, so
// its metadata is described amid the mobs'.
var lastOrbID int32

// lastTNTID is the primed TNT the last Spawn Entity put in the world, so its
// fuse and fall can be compared tick by tick.
var lastTNTID int32

// describeEntityPacket parses the packets that put other players into the world
// and move them around, with the layouts this repository writes them with.
func describeEntityPacket(id int32, data []byte) {
	r := protocol.NewReader(data)
	name := ""

	switch id {
	case protocol.IDPlaySpawnEntity:
		name = "spawn-entity"
		entityID := r.ReadVarInt()
		uuid := r.ReadUUID()
		entityType := r.ReadVarInt()
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		vx, vy, vz := r.ReadLpVec3()
		pitch, yaw, headYaw := r.ReadAngle(), r.ReadAngle(), r.ReadAngle()
		extra := r.ReadVarInt()
		fmt.Printf("PLAY spawn-entity id=%d uuid=%s type=%d pos=(%.2f %.2f %.2f) "+
			"velocity=(%.2f %.2f %.2f) yaw=%.1f pitch=%.1f headYaw=%.1f data=%d\n",
			entityID, uuid, entityType, x, y, z, vx, vy, vz, yaw, pitch, headYaw, extra)
		if entityType == protocol.EntityTypeExperienceOrb {
			lastOrbID = entityID
		}
		if entityType == protocol.EntityTypeTnt {
			lastTNTID = entityID
		}

	case protocol.IDPlayEntityAnimation:
		name = "entity-animation"
		fmt.Printf("PLAY entity-animation id=%d animation=%d\n", r.ReadVarInt(), r.ReadU8())

	case protocol.IDPlayEntityMetadata:
		name = "entity-metadata"
		fmt.Printf("PLAY entity-metadata id=%d%s\n", r.ReadVarInt(), describeMetadata(r))

	case protocol.IDPlayPlayerInfoRemove:
		name = "player-info-remove"
		count := r.ReadVarInt()
		for range count {
			fmt.Printf("PLAY player-info-remove uuid=%s\n", r.ReadUUID())
		}

	case protocol.IDPlayRemoveEntities:
		name = "remove-entities"
		count := r.ReadVarInt()
		ids := make([]int32, 0, count)
		for range count {
			ids = append(ids, r.ReadVarInt())
		}
		fmt.Printf("PLAY remove-entities ids=%v\n", ids)

	case protocol.IDPlayEntityPosition:
		name = "entity-position"
		id, props := r.ReadVarInt(), r.ReadVarInt()
		fmt.Printf("PLAY entity-position id=%d onGround=%v delta=%s\n",
			id, props&1 != 0, readVecDelta(r, props>>1))

	case protocol.IDPlayEntityPositionRot:
		name = "entity-position-rotation"
		id, props := r.ReadVarInt(), r.ReadVarInt()
		delta := readVecDelta(r, props>>1)
		fmt.Printf("PLAY entity-position-rotation id=%d onGround=%v delta=%s yaw=%.1f pitch=%.1f\n",
			id, props&1 != 0, delta, r.ReadAngle(), r.ReadAngle())

	case protocol.IDPlayEntityRotation:
		name = "entity-rotation"
		id, onGround := r.ReadVarInt(), r.ReadBool()
		fmt.Printf("PLAY entity-rotation id=%d onGround=%v yaw=%.1f pitch=%.1f\n",
			id, onGround, r.ReadAngle(), r.ReadAngle())

	case protocol.IDPlayHeadRotation:
		name = "head-rotation"
		fmt.Printf("PLAY head-rotation id=%d headYaw=%.1f\n", r.ReadVarInt(), r.ReadAngle())

	case protocol.IDPlayWorldEvent:
		name = "world-event"
		event := r.ReadI32()
		x, y, z := r.ReadPosition()
		fmt.Printf("PLAY world-event event=%d pos=(%d %d %d) data=%d disableVolume=%v\n",
			event, x, y, z, r.ReadI32(), r.ReadBool())

	case protocol.IDPlayTeleportEntity:
		name = "teleport-entity"
		entityID := r.ReadVarInt()
		path := readPositionPath(r)
		yaw, pitch := r.ReadF32(), r.ReadF32()
		fmt.Printf("PLAY teleport-entity id=%d path=%s yaw=%.1f pitch=%.1f onGround=%v\n",
			entityID, path, yaw, pitch, r.ReadBool())

	case protocol.IDPlayTeleportEntityRelative:
		name = "teleport-entity-relative"
		entityID := r.ReadVarInt()
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		vx, vy, vz := r.ReadF64(), r.ReadF64(), r.ReadF64()
		yaw, pitch := r.ReadF32(), r.ReadF32()
		flags, ground := r.ReadI32(), r.ReadBool()
		fmt.Printf("PLAY teleport-entity-relative id=%d pos=(%.2f %.2f %.2f) velocity=(%.2f %.2f %.2f) "+
			"yaw=%.1f pitch=%.1f flags=0x%x onGround=%v leftover=%d err=%v\n",
			entityID, x, y, z, vx, vy, vz, yaw, pitch, flags, ground, r.Remaining(), r.Err())

	case protocol.IDPlayUpdateAttributes:
		name = "update-attributes"
		entityID := r.ReadVarInt()
		fmt.Printf("PLAY update-attributes id=%d", entityID)
		for range r.ReadVarInt() {
			id := r.ReadVarInt()
			value := r.ReadF64()
			mods := make([]string, 0, r.ReadVarInt())
			for range cap(mods) {
				// The modifier: identifier, double amount, varint operation.
				// vanilla: AttributeModifier.STREAM_CODEC
				name := r.ReadString(32767)
				amount := r.ReadF64()
				operation := r.ReadVarInt()
				mods = append(mods, fmt.Sprintf("%s=%.6f/op%d", name, amount, operation))
			}
			fmt.Printf(" attr=%d:%.6f", id, value)
			if len(mods) > 0 {
				fmt.Printf(" mods=%v", mods)
			}
		}

	case protocol.IDPlayDamageEvent:
		name = "damage-event"
		victim := r.ReadVarInt()
		damageType := r.ReadVarInt()
		cause := r.ReadVarInt()
		direct := r.ReadVarInt()
		position := "<none>"
		if r.ReadBool() {
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			position = fmt.Sprintf("(%.2f %.2f %.2f)", x, y, z)
		}
		fmt.Printf("PLAY damage-event victim=%d type=%d cause=%d direct=%d pos=%s",
			victim, damageType, cause, direct, position)

	case protocol.IDPlayHurtAnimation:
		name = "hurt-animation"
		fmt.Printf("PLAY hurt-animation victim=%d yaw=%.1f", r.ReadVarInt(), r.ReadF32())

	case protocol.IDPlayEntityEquipment:
		name = "entity-equipment"
		entityID := r.ReadVarInt()
		var pieces []string
		for {
			raw := r.ReadU8()
			item, _ := r.ReadSlot()
			piece := fmt.Sprintf("slot%d=%d/%d", raw&0x7F, item.ItemID, item.Count)
			if item.Damage != 0 {
				piece += fmt.Sprintf("/dmg%d", item.Damage)
			}
			pieces = append(pieces, piece)
			if raw&0x80 == 0 {
				break
			}
		}
		fmt.Printf("PLAY entity-equipment id=%d %s", entityID, strings.Join(pieces, " "))

	case protocol.IDPlayPickupItem:
		name = "pickup-item"
		fmt.Printf("PLAY pickup-item collected=%d collector=%d count=%d",
			r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt())

	case protocol.IDPlayEntityEvent:
		name = "entity-event"
		// The entity is an Int here, not the VarInt the rest of the play
		// packets carry it by.
		fmt.Printf("PLAY entity-event entity=%d status=%d", r.ReadI32(), r.ReadU8())

	case protocol.IDPlayExplosion:
		name = "explosion"
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		radius := r.ReadF32()
		blockCount := r.ReadI32()
		push := "<none>"
		if r.ReadBool() {
			push = fmt.Sprintf("(%.3f %.3f %.3f)", r.ReadF64(), r.ReadF64(), r.ReadF64())
		}
		particle := r.ReadVarInt()
		// The blast particle and the block particles are all simple
		// types, which carry no data of their own on the wire.
		var sound string
		if soundID := r.ReadVarInt(); soundID != 0 {
			// A registry ID, which the wire carries one above the
			// minecraft:sound_event protocol ID.
			sound = fmt.Sprintf("%d", soundID-1)
		} else {
			sound = r.ReadString(32767)
			if r.ReadBool() {
				r.ReadF32()
			}
		}
		blocks := make([]string, 0, r.ReadVarInt())
		for range cap(blocks) {
			p := r.ReadVarInt()
			r.ReadF32() // scaling
			r.ReadF32() // speed
			w := r.ReadVarInt()
			blocks = append(blocks, fmt.Sprintf("%d/1.0/%d", p, w))
		}
		playSound := r.ReadBool()
		fmt.Printf("PLAY explosion pos=(%.2f %.2f %.2f) radius=%.2f blocks=%d push=%s "+
			"particle=%d sound=%s blockParticles=%v playSound=%v leftover=%d err=%v",
			x, y, z, radius, blockCount, push, particle, sound, blocks, playSound, r.Remaining(), r.Err())

	case protocol.IDPlaySetEntityMotion:
		name = "set-entity-motion"
		entityID := r.ReadVarInt()
		vx, vy, vz := r.ReadLpVec3()
		fmt.Printf("PLAY set-entity-motion id=%d velocity=(%.3f %.3f %.3f)",
			entityID, vx, vy, vz)

	case protocol.IDPlaySoundEffect:
		name = "sound-effect"
		if soundID := r.ReadVarInt(); soundID == 0 {
			// An inline sound event: the name, the fixed-range flag and
			// the range a fixed one carries.
			soundName := r.ReadString(32767)
			if r.ReadBool() {
				r.ReadF32()
			}
			fmt.Printf("PLAY sound-effect sound=%q", soundName)
		} else {
			// A registry ID, which the wire carries one above the
			// minecraft:sound_event protocol ID.
			fmt.Printf("PLAY sound-effect sound-id=%d", soundID-1)
		}
		category := r.ReadVarInt()
		x, y, z := r.ReadI32(), r.ReadI32(), r.ReadI32()
		fmt.Printf("PLAY category=%d pos=(%.2f %.2f %.2f) volume=%.2f pitch=%.2f seed=%d",
			category, float64(x)/8, float64(y)/8, float64(z)/8,
			r.ReadF32(), r.ReadF32(), r.ReadI64())
	}

	fmt.Printf("PLAY %s leftover=%d err=%v\n", name, r.Remaining(), r.Err())
}

// describeMetadata reads an entity metadata array as "index=type:value"
// pairs. Only fixed, simple layouts are read; anything else ends the array
// with "unreadable", since its length is unknown.
func describeMetadata(r *protocol.Reader) string {
	var fields []string
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		value, ok := readMetadataValue(r, fieldType)
		if !ok {
			fields = append(fields, fmt.Sprintf("%d=type%d:unreadable", index, fieldType))
			r.Rest()
			break
		}
		fields = append(fields, fmt.Sprintf("%d=type%d:%s", index, fieldType, value))
	}
	return " " + strings.Join(fields, " ")
}

// readMetadataValue reads one metadata value, reporting whether its type is one
// this can find the end of.
func readMetadataValue(r *protocol.Reader, fieldType int32) (string, bool) {
	switch fieldType {
	case 0: // Byte
		return fmt.Sprintf("%#02x", r.ReadU8()), true
	case 1, 12, 14, 15, 19, 20, 35, 36, 37, 38, 42: // VarInt, and the enums stored as one
		return fmt.Sprintf("%d", r.ReadVarInt()), true
	case 2: // VarLong
		return fmt.Sprintf("%d", r.ReadI64()), true
	case 3: // Float
		return fmt.Sprintf("%.2f", r.ReadF32()), true
	case 4: // String
		return fmt.Sprintf("%q", r.ReadString(32767)), true
	case 8: // Boolean
		return fmt.Sprintf("%v", r.ReadBool()), true
	case 9, 39: // Rotations, Vector3
		return fmt.Sprintf("(%.2f %.2f %.2f)", r.ReadF32(), r.ReadF32(), r.ReadF32()), true
	case 10: // Position
		return fmt.Sprintf("%#x", r.ReadI64()), true
	case 18: // Villager Data
		return fmt.Sprintf("(%d %d %d)", r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()), true
	case 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32: // registry variants
		return fmt.Sprintf("%d", r.ReadVarInt()), true
	case 40: // Quaternion
		return fmt.Sprintf("(%.2f %.2f %.2f %.2f)", r.ReadF32(), r.ReadF32(), r.ReadF32(), r.ReadF32()), true
	default:
		return "", false
	}
}

// describeChunk parses a chunk packet with the structure this repository
// implements.
func describeChunk(data []byte) {
	r := protocol.NewReader(data)
	chunkX, chunkZ := r.ReadI32(), r.ReadI32()

	heightmaps := r.ReadVarInt()
	for range heightmaps {
		r.ReadVarInt() // type
		for range r.ReadVarInt() {
			r.ReadI64()
		}
	}

	sections := r.ReadBytes(1 << 24)
	sr := protocol.NewReader(sections)
	sectionCount := 0
	for sr.Remaining() > 0 && sr.Err() == nil {
		blocks := sr.ReadI16()
		fluids := sr.ReadI16()
		readPalettedContainer(sr, 4096)
		readPalettedContainer(sr, 64)
		if sectionCount == 0 {
			fmt.Printf("PLAY chunk section[0] blocks=%d fluids=%d\n", blocks, fluids)
		}
		sectionCount++
	}

	blockEntities := r.ReadVarInt()
	for range blockEntities {
		r.ReadU8()
		r.ReadI16()
		r.ReadVarInt()
		break // NBT follows, which we cannot skip
	}

	skyMask := r.ReadBitSet()
	blockMask := r.ReadBitSet()
	r.ReadBitSet() // empty sky mask
	r.ReadBitSet() // empty block mask
	skyArrays := r.ReadVarInt()
	for range skyArrays {
		r.ReadBytes(4096)
	}
	blockArrays := r.ReadVarInt()
	for range blockArrays {
		r.ReadBytes(4096)
	}

	fmt.Printf("PLAY chunk (%d,%d) heightmaps=%d sections=%d sectionBytes=%d blockEntities=%d "+
		"skyMask=%#x blockMask=%#x skyArrays=%d blockArrays=%d sectionLeftover=%d leftover=%d err=%v/%v\n",
		chunkX, chunkZ, heightmaps, sectionCount, len(sections), blockEntities,
		skyMask, blockMask, skyArrays, blockArrays, sr.Remaining(), r.Remaining(), sr.Err(), r.Err())
}

// readPalettedContainer skips a paletted container of the given entry count.
func readPalettedContainer(r *protocol.Reader, entries int) {
	bpe := r.ReadU8()
	switch {
	case bpe == 0:
		v := r.ReadVarInt() // the single value
		fmt.Printf("PALETTE entries=%d bpe=0 value=%d\n", entries, v)
		return
	case entries == 4096 && bpe <= 8, entries == 64 && bpe <= 3:
		var palette []int32
		for range r.ReadVarInt() { // the palette
			palette = append(palette, r.ReadVarInt())
		}
		fmt.Printf("PALETTE entries=%d bpe=%d values=%v\n", entries, bpe, palette)
	}
	perLong := 64 / int(bpe)
	longs := (entries + perLong - 1) / perLong
	for range longs {
		r.ReadI64()
	}
}

// traceEntity is the entity ID the TRACE_ENTITY environment variable names,
// read once at startup: a zero means no entity is being followed.
var traceEntity int32

// traceEntitySetup reads TRACE_ENTITY, the entity whose every move the run
// is to follow.
func traceEntitySetup() {
	if v := os.Getenv("TRACE_ENTITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			traceEntity = int32(n)
		}
	}
}

// traceEntityMatches reports whether a movement packet is about the traced
// entity; only relative move and teleport carry the id first.
func traceEntityMatches(data []byte) bool {
	if traceEntity == 0 {
		return false
	}
	id := protocol.NewReader(data).ReadVarInt()
	return id == traceEntity
}

// describeDebugSample parses one Debug Sample: a prefixed array of longs and
// the sample type.
func describeDebugSample(data []byte) {
	r := protocol.NewReader(data)
	n := r.ReadVarInt()
	sample := make([]int64, 0, max(n, 0))
	for i := int32(0); i < n && r.Err() == nil; i++ {
		sample = append(sample, r.ReadI64())
	}
	sampleType := r.ReadVarInt()
	fmt.Printf("PLAY debug-sample type=%d sample=%v leftover=%d err=%v\n",
		sampleType, sample, r.Remaining(), r.Err())
}

// readVecDelta reads a move_entity VecDelta: three i16 deltas when steps is 0,
// otherwise steps of (varint ticks, three i16).
// vanilla: VecDelta.read
func readVecDelta(r *protocol.Reader, steps int32) string {
	if steps == 0 {
		return fmt.Sprintf("(%d %d %d)", r.ReadI16(), r.ReadI16(), r.ReadI16())
	}
	out := make([]string, 0, steps)
	for range steps {
		ticks := r.ReadVarInt()
		out = append(out, fmt.Sprintf("t%d(%d %d %d)", ticks, r.ReadI16(), r.ReadI16(), r.ReadI16()))
		if r.Err() != nil {
			break
		}
	}
	return fmt.Sprintf("%v", out)
}

// readPositionPath reads an entity_position_sync PositionPath: type 0 is a
// position, type 1 a list of (position, varint tick offset) steps.
// vanilla: PositionPath.STREAM_CODEC
func readPositionPath(r *protocol.Reader) string {
	switch kind := r.ReadVarInt(); kind {
	case 0:
		return fmt.Sprintf("linear(%.2f %.2f %.2f)", r.ReadF64(), r.ReadF64(), r.ReadF64())
	case 1:
		n := r.ReadVarInt()
		out := make([]string, 0, n)
		for range n {
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			out = append(out, fmt.Sprintf("t%d(%.2f %.2f %.2f)", r.ReadVarInt(), x, y, z))
			if r.Err() != nil {
				break
			}
		}
		return fmt.Sprintf("stepped%v", out)
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}
