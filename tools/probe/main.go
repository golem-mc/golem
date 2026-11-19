// Command probe logs in to a Minecraft server and reports what it receives,
// to compare the packet streams of two servers (usually vanilla and this).
//
//	go run ./tools/probe 127.0.0.1:25565            # transcribe a join
//	DUMP=registries.json go run ./tools/probe ...   # also save every registry and tag
//	CONFIG_ONLY=1 go run ./tools/probe ...         # capture reference configuration before porting play
//	PLAY_SECONDS=70 go run ./tools/probe ...        # stay in the world longer
//	PLAYER=Watcher go run ./tools/probe ...         # log in under another name
//	CHAT="hello" go run ./tools/probe ...           # say something once in the world
//	COMMAND="list" go run ./tools/probe ...         # run a command once in the world
//	COMMAND_DELAY=5 COMMAND=kill ...                # ... that many seconds after joining
//	SUGGEST="/l" go run ./tools/probe ...           # ask for tab completions
//	WALK=3 go run ./tools/probe ...                 # walk that many chunks east
//	ORDER_LIMIT=n go run ./tools/probe ...          # print more of the packet order
//	GIVE=36:926:5 go run ./tools/probe ...          # put items in slots, as a creative client
//	USE=1 go run ./tools/probe ...                  # right-click with whatever is in hand
//	RESPAWN=1 COMMAND=kill go run ./tools/probe ... # die, respawn, and log what follows
//	HEXDUMP=0x12,0x14 go run ./tools/probe ...      # print those packets as raw hex
//	TIMES=1 go run ./tools/probe ...                # timestamp every packet sent and received
//	DEBUG_SAMPLES=1 go run ./tools/probe ...        # subscribe to the F3 TPS chart's samples (ops only)
//
// The scripted scenarios, in scenarios.go:
//
//	SETUP="fill 64 199 64 68 199 68 stone|fill 70 197 64 98 197 68 stone"
//	FALL=sprint2 go run ./tools/probe ...           # replay a jump off an edge
//	BOUNCE=1 go run ./tools/probe ...               # land on a slime block, print the jump statistic
//	EAT=1 go run ./tools/probe ...                  # hold a meal, three uses in a row
//	ENCHANT=1 go run ./tools/probe ...              # stand a table in 30 shelves, enchant
//	PISTON=1 go run ./tools/probe ...               # push and pull a block with a sticky piston
//	PISTON=door go run ./tools/probe ...            # two facing pistons, the door geometry
//	ENCHANT_ITEM=golden_pickaxe ENCHANT_BUTTONS="2,1" ... # a different item and presses
//	ENCHANT_SHELVES=0 ...                           # a table with nothing around it
//
// Packets are parsed with internal/protocol/'s layouts and every line reports the
// bytes left over: anything but leftover=0 means our layout disagrees with
// the server.
package main

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: probe HOST:PORT")
	}
	profile, err := profileFromEnv()
	must(err)
	addr := os.Args[1]
	dump.Version, dump.Protocol = profile.version, profile.protocol
	timing.start = time.Now()
	timing.on = os.Getenv("TIMES") != ""
	timing.phase = "login"
	defer writeDump()
	traceEntitySetup()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		fail("dial: %v", err)
	}
	c := protocol.NewConn(conn)
	defer c.Close()
	stamp("connected")

	// Handshake, intent login.
	handshake := protocol.Handshake{
		ProtocolVersion: profile.protocol,
		ServerAddress:   "localhost",
		ServerPort:      25565,
		NextState:       protocol.NextStateLogin,
	}
	must(handleLogin(c, handshake))
	must(handleConfiguration(c, profile))
	if !profile.configuration {
		handlePlay(c)
	}
}

func handleLogin(c *protocol.Conn, handshake protocol.Handshake) error {
	var b protocol.Buffer
	b.PutVarInt(handshake.ProtocolVersion)
	b.PutString(handshake.ServerAddress)
	b.PutI16(int16(handshake.ServerPort))
	b.PutVarInt(handshake.NextState)
	must(c.WritePacket(protocol.IDHandshake, b.Bytes()))

	// Not NAME: some shells already export that, and logging in as the
	// machine's hostname is a confusing way to find out.
	name := os.Getenv("PLAYER")
	if name == "" {
		name = "Probe"
	}
	b = protocol.Buffer{}
	b.PutString(name)
	b.PutUUID(protocol.OfflineUUID(name))
	must(c.WritePacket(protocol.IDLoginStart, b.Bytes()))
	stamp("sent login start")

	// Login.
	for {
		id, data, err := read(c)
		if err != nil {
			return err
		}
		switch id {
		case protocol.IDSetCompression:
			threshold := protocol.NewReader(data).ReadVarInt()
			fmt.Printf("LOGIN set-compression threshold=%d\n", threshold)
			c.EnableCompression(int(threshold))
		case protocol.IDLoginSuccess:
			r := protocol.NewReader(data)
			uuid := r.ReadUUID()
			name := r.ReadString(16)
			props := r.SkipProfileProperties()
			r.ReadUUID()
			fmt.Printf("LOGIN success uuid=%s name=%s properties=%d leftover=%d err=%v\n",
				uuid, name, props, r.Remaining(), r.Err())
			if r.Remaining() != 0 || r.Err() != nil {
				return fmt.Errorf("login success leftover=%d err=%v", r.Remaining(), r.Err())
			}
			must(c.WritePacket(protocol.IDLoginAcknowledged, nil))
			timing.phase = "config"
			stamp("sent login acknowledged")
		case protocol.IDLoginDisconnect:
			fail("disconnected during login: %s", protocol.NewReader(data).ReadString(32767))
		default:
			fmt.Printf("LOGIN packet 0x%02X (%d bytes)\n", id, len(data))
			continue
		}
		if id == protocol.IDLoginSuccess {
			break
		}
	}
	return nil
}

func handleConfiguration(c *protocol.Conn, profile configurationProfile) error {
	// Configuration.
	var registries []string
	tagCounts := map[string]int{}
configuration:
	for {
		id, data, err := read(c)
		if err != nil {
			return err
		}
		switch profile.incoming(id) {
		case protocol.IDKnownPacks:
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			if count <= 0 && profile.configuration {
				return fmt.Errorf("reference offered no known packs")
			}
			var reply protocol.Buffer
			reply.PutVarInt(count)
			for range count {
				ns, name, version := r.ReadString(32767), r.ReadString(32767), r.ReadString(32767)
				if profile.configuration && (ns != "minecraft" || name != "core" || version != profile.version) {
					return fmt.Errorf("reference offered unknown pack %s:%s %s, expected minecraft:core %s", ns, name, version, profile.version)
				}
				fmt.Printf("CONFIG known-pack %s:%s %s\n", ns, name, version)
				reply.PutString(ns)
				reply.PutString(name)
				reply.PutString(version)
			}
			if r.Remaining() != 0 || r.Err() != nil {
				return fmt.Errorf("known packs leftover=%d err=%v", r.Remaining(), r.Err())
			}
			if err := profile.send(c, protocol.IDKnownPacksIn, reply.Bytes()); err != nil {
				return err
			}

			var info protocol.Buffer
			clientInformation(&info, 0x7F)
			if err := profile.send(c, protocol.IDClientInformation, info.Bytes()); err != nil {
				return err
			}
			stamp("sent known packs and client information")

		case protocol.IDRegistryData:
			r := protocol.NewReader(data)
			name := r.ReadString(32767)
			count := r.ReadVarInt()
			if count < 0 {
				return fmt.Errorf("registry %s has negative entry count %d", name, count)
			}
			withData := 0
			reg := dumpedRegistry{Name: name}
			truncated := false
			for range count {
				entry := r.ReadString(32767)
				reg.Entries = append(reg.Entries, entry)
				if r.ReadBool() {
					withData++
					if profile.configuration {
						return fmt.Errorf("reference registry %s has inline data; cannot capture its IDs", name)
					}
					// Skip the rest: NBT needs a parser we do not have here.
					truncated = true
					break
				}
			}
			if !truncated && (r.Remaining() != 0 || r.Err() != nil) {
				return fmt.Errorf("registry %s leftover=%d err=%v", name, r.Remaining(), r.Err())
			}
			dump.Registries = append(dump.Registries, reg)
			registries = append(registries, fmt.Sprintf("%s(%d)", name, count))
			if withData > 0 {
				fmt.Printf("CONFIG registry %s has inline data\n", name)
			}

		case protocol.IDConfigUpdateTags:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				reg := r.ReadString(32767)
				tags := r.ReadVarInt()
				tagCounts[reg] = int(tags)
				dumped := dumpedTagRegistry{Registry: reg}
				for range tags {
					tag := dumpedTag{Name: r.ReadString(32767)}
					for range r.ReadVarInt() {
						tag.IDs = append(tag.IDs, r.ReadVarInt())
					}
					dumped.Tags = append(dumped.Tags, tag)
				}
				dump.Tags = append(dump.Tags, dumped)
			}
			fmt.Printf("CONFIG update-tags leftover=%d err=%v\n", r.Remaining(), r.Err())
			if r.Remaining() != 0 || r.Err() != nil {
				return fmt.Errorf("update-tags leftover=%d err=%v", r.Remaining(), r.Err())
			}

		case protocol.IDConfigServerLinks:
			// Labels are Either VarInt Enum or Text Component: built-in
			// kinds ride as the boolean-true half, custom ones as text.
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				var label string
				if r.ReadBool() {
					label = strconv.Itoa(int(r.ReadVarInt()))
				} else if text, ok := protocol.ReadText(r); ok {
					label = text.Plain()
				} else {
					break
				}
				fmt.Printf("CONFIG server-link label=%s url=%s\n", label, r.ReadString(32767))
			}
			fmt.Printf("CONFIG server-links leftover=%d err=%v\n", r.Remaining(), r.Err())
			if r.Remaining() != 0 || r.Err() != nil {
				return fmt.Errorf("server-links leftover=%d err=%v", r.Remaining(), r.Err())
			}

		case protocol.IDConfigKeepAlive:
			if len(data) != 8 {
				return fmt.Errorf("configuration keep-alive has %d bytes, expected 8", len(data))
			}
			if err := profile.send(c, protocol.IDConfigKeepAliveIn, data); err != nil {
				return err
			}

		case protocol.IDConfigFinish:
			if profile.configuration && (len(dump.Registries) == 0 || len(dump.Tags) == 0) {
				return fmt.Errorf("reference finished configuration without registries and tags")
			}
			if err := profile.send(c, protocol.IDConfigFinishAck, nil); err != nil {
				return err
			}
			timing.phase = "play"
			stamp("sent finish configuration acknowledged")
			break configuration

		case protocol.IDConfigDisconnect:
			return fmt.Errorf("disconnected during configuration (%d bytes)", len(data))

		default:
			fmt.Printf("CONFIG packet 0x%02X (%d bytes)\n", id, len(data))
		}
	}

	sort.Strings(registries)
	fmt.Printf("CONFIG registries: %v\n", registries)
	fmt.Printf("CONFIG tagged registries: %v\n", tagCounts)
	return nil
}

// respawned records that RESPAWN's one respawn has been asked for.
var respawned bool

// respawnOnce asks to respawn the way the client's respawn button does, once,
// when RESPAWN is set, so what a server sends for a respawn can be compared.
func respawnOnce(c *protocol.Conn) {
	if os.Getenv("RESPAWN") == "" || respawned {
		return
	}
	respawned = true
	var b protocol.Buffer
	b.PutVarInt(protocol.ClientCommandPerformRespawn)
	must(c.WritePacket(protocol.IDPlayClientCommandIn, b.Bytes()))
	fmt.Println("PLAY sent respawn")
}

func handlePlay(c *protocol.Conn) {
	// Play.
	counts := map[int32]int{}
	hexDump := dumpedPackets()
	if os.Getenv("TRACE") != "" {
		// TRACE=1 keeps the scenario trace up all session, for reading a
		// death's packet run.
		traceSurvivalPackets()
		defer func() { traceScenarioPacket = nil }()
	}
	order := 0
	orderLimit := orderLimitEnv()
	deadline := time.Now().Add(playDuration())
	for time.Now().Before(deadline) {
		id, data, err := read(c)
		if err != nil {
			fmt.Printf("PLAY read error: %v\n", err)
			break
		}
		counts[id]++
		if traceScenarioPacket != nil {
			traceScenarioPacket(id, data)
		}
		if hexDump[id] {
			fmt.Printf("HEX 0x%02X %x\n", id, data)
		}
		if orderLimit == 0 || order < orderLimit {
			fmt.Printf("ORDER %02d 0x%02X (%d bytes)\n", order, id, len(data))
			order++
		}

		switch id {
		case protocol.IDPlayLogin:
			describeLoginPlay(data)
		case protocol.IDPlayChunkData:
			if counts[id] == 1 {
				describeChunk(data)
			}
		case protocol.IDPlayPlayerInfoUpdate:
			describeJoinPacket(id, data)
		case protocol.IDPlayUpdateTime:
			// All of them: placing other packets on a wall clock needs the
			// world ages they arrived between.
			describeJoinPacket(id, data)
		case protocol.IDPlayGameEvent:
			// Every one: the weather fades in one event per tick.
			describeJoinPacket(id, data)
		case protocol.IDPlayRecipeBookAdd:
			describeJoinPacket(id, data)
		case protocol.IDPlaySpawnPosition,
			protocol.IDPlayDifficulty, protocol.IDPlayAbilities,
			protocol.IDPlayServerData, protocol.IDPlayInitializeBorder,
			protocol.IDPlayUpdateRecipes,
			protocol.IDPlayRecipeBookSettings, protocol.IDPlayTickingState,
			protocol.IDPlayTickingStep:
			if counts[id] == 1 {
				describeJoinPacket(id, data)
			}
		case protocol.IDPlaySetExperience:
			// Every one of these, not only the first: the join sends one and
			// so does every change to the experience it describes.
			describeJoinPacket(id, data)
		case protocol.IDPlayChunkBatchFinished:
			// Vanilla sends no more chunks until a batch is acknowledged.
			var ack protocol.Buffer
			ack.PutF32(64) // chunks per tick, as fast as the server likes
			must(c.WritePacket(protocol.IDPlayChunkBatchReceived, ack.Bytes()))
			fmt.Printf("PLAY chunk-batch-finished size=%d\n", protocol.NewReader(data).ReadVarInt())

		case protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk:
			// Every one of these, rather than only the first: they are how a
			// server follows a player who is walking.
			describeJoinPacket(id, data)

		case protocol.IDPlayMapData:
			// A map's pixels are few in a session and worth reading whole:
			// one packet is one whole map a player came to hold.
			describeJoinPacket(id, data)

		case protocol.IDPlaySpawnEntity, protocol.IDPlayPlayerInfoRemove,
			protocol.IDPlayRemoveEntities, protocol.IDPlayEntityAnimation,
			protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent, protocol.IDPlayUpdateAttributes,
			protocol.IDPlayWorldEvent:
			// The packets that make other players come, go and swing an
			// arm, and the ones a fight produces: few enough to print all.
			describeEntityPacket(id, data)

		case protocol.IDPlayExplosion:
			// An explosion is one packet in a session's worth of nothing
			// else like it; every one of them is worth its line.
			describeEntityPacket(id, data)

		case protocol.IDPlaySoundEffect, protocol.IDPlaySetEntityMotion:
			// Sent many times a second, so only the first few are printed
			// (the counts at the end show all); the traced TNT's motion is
			// printed every time.
			if counts[id] <= 3 || (id == protocol.IDPlaySetEntityMotion &&
				protocol.NewReader(data).ReadVarInt() == lastTNTID) {
				describeEntityPacket(id, data)
			}

		case protocol.IDPlayEntityMetadata:
			// Only the first few entities' metadata is printed, but always
			// our own (how a client learns to draw itself), and the
			// orb's and primed TNT's that follow their spawn.
			if entityID := protocol.NewReader(data).ReadVarInt(); entityID == selfEntityID {
				fmt.Printf("PLAY (self) ")
				describeEntityPacket(id, data)
			} else if counts[id] <= 8 || entityID == lastOrbID || entityID == lastTNTID {
				describeEntityPacket(id, data)
			}

		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity:
			// Only the first few are printed, but every move of
			// TRACE_ENTITY, to compare an orb's drift between servers.
			if counts[id] <= 3 || traceEntityMatches(data) {
				describeEntityPacket(id, data)
			}
		case protocol.IDPlayTeleportEntityRelative:
			describeEntityPacket(id, data)
		case protocol.IDPlaySyncPosition:
			r := protocol.NewReader(data)
			teleportID := r.ReadVarInt()
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			r.ReadF64()
			yaw, pitch := r.ReadF32(), r.ReadF32()
			flags := r.ReadI32()
			fmt.Printf("PLAY sync-position id=%d pos=(%.1f %.1f %.1f) yaw=%.1f pitch=%.1f flags=%d leftover=%d err=%v\n",
				teleportID, x, y, z, yaw, pitch, flags, r.Remaining(), r.Err())

			// Where the server says the player is, which is also where a walk
			// carries on from after being teleported back.
			position.Lock()
			position.x, position.y, position.z = x, y, z
			position.Unlock()

			var ack protocol.Buffer
			ack.PutVarInt(teleportID)
			ack.PutF64(x)
			ack.PutF64(y)
			ack.PutF64(z)
			ack.PutF32(yaw)
			ack.PutF32(pitch)
			must(c.WritePacket(protocol.IDPlayConfirmTeleport, ack.Bytes()))
			if counts[id] == 1 {
				must(c.WritePacket(protocol.IDPlayLoaded, nil))
				sendChat(c)
				subscribeDebugSamples(c)
				go sendCommand(c)
				sendCommandSuggestion(c)
				sendGive(c)
				sendUse(c)
				go walk(c)
				go act(c)
				go scenario(c)
			}

		case protocol.IDPlayCombatDeath:
			fmt.Printf("PLAY combat-death (%d bytes)\n", len(data))
			respawnOnce(c)
		case protocol.IDPlaySetHealth:
			// A player who joins dead is only told so by an empty health
			// bar; the client shows its death screen off that alone.
			if health := protocol.NewReader(data).ReadF32(); health <= 0 {
				fmt.Println("PLAY set-health health=0")
				respawnOnce(c)
			}
		case protocol.IDPlayRespawn:
			fmt.Printf("PLAY respawn (%d bytes)\n", len(data))
			if os.Getenv("RESPAWN") != "" {
				must(c.WritePacket(protocol.IDPlayLoaded, nil))
			}

		case protocol.IDPlaySystemChat:
			describeSystemChat(data)
		case protocol.IDPlayDebugSample:
			describeDebugSample(data)
		case protocol.IDPlayDisguisedChat:
			describeDisguisedChat(data)
		case protocol.IDPlayPlayerChat:
			describePlayerChat(data)
		case protocol.IDPlayCommands:
			describeCommands(data)
		case protocol.IDPlayCommandSuggestions:
			describeCommandSuggestions(data)
		case protocol.IDPlayKeepAlive:
			must(c.WritePacket(protocol.IDPlayKeepAliveIn, data))
			fmt.Printf("PLAY keep-alive answered\n")
		case protocol.IDPlayDisconnect:
			fmt.Printf("PLAY disconnected (%d bytes)\n", len(data))
			deadline = time.Now()
		}
	}

	ids := make([]int, 0, len(counts))
	for id := range counts {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	fmt.Printf("PLAY packets received:\n")
	for _, id := range ids {
		fmt.Printf("  0x%02X x%d\n", id, counts[int32(id)])
	}
}

// playDuration is how long to stay in the world before reporting.
func playDuration() time.Duration {
	if v := os.Getenv("PLAY_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return 20 * time.Second
}

// orderLimitEnv is ORDER_LIMIT, how many play packets the ORDER lines cover:
// default 40 (a whole join), 0 unlimited.
func orderLimitEnv() int {
	if v := os.Getenv("ORDER_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 40
}

// timing is what TIMES=1 stamps its lines with: the time since the probe
// started, and the phase the packets belong to.
var timing struct {
	on    bool
	start time.Time
	phase string
}

// stamp prints a TIME line for a packet sent or received, when TIMES is set.
func stamp(what string) {
	if timing.on {
		fmt.Printf("TIME %9.1fms %-6s %s\n", float64(time.Since(timing.start).Microseconds())/1000, timing.phase, what)
	}
}

func read(c *protocol.Conn) (int32, []byte, error) {
	if err := c.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return 0, nil, err
	}
	id, data, err := c.ReadPacket()
	if err == nil {
		stamp(fmt.Sprintf("recv 0x%02X (%d bytes)", id, len(data)))
	}
	return id, data, err
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "probe: "+format+"\n", args...)
	os.Exit(1)
}
