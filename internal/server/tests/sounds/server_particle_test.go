package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// runParticle runs one /particle line and returns the Level Particles packets
// the runner was sent and the System Chat line it answered with.
func runParticle(c *servertest.Client, command string) (particles [][]byte, chat []byte) {
	c.T.Helper()
	c.ChatCommand(command)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayLevelParticles:
			particles = append(particles, data)
		case protocol.IDPlaySystemChat:
			chat = data
		}
	})
	return particles, chat
}

// TestParticleCommandSendsToNearbyPlayers checks the packet a player within
// range receives: the flame type, the count, the position and the flags.
func TestParticleCommandSendsToNearbyPlayers(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()
	pos := srv.PlayerPosition("DJ")

	particles, chat := runParticle(c, "particle minecraft:flame ~ ~ ~ 0 0 0 0 5")
	if len(particles) != 1 {
		t.Fatalf("got %d Level Particles packets, want 1", len(particles))
	}
	r := protocol.NewReader(particles[0])
	id := r.ReadVarInt()
	longDistance, alwaysVisible := r.ReadBool(), r.ReadBool()
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF32()
	r.ReadF32()
	r.ReadF32()
	speed, speedY, speedZ := r.ReadF32(), r.ReadF32(), r.ReadF32()
	count, randomization := r.ReadVarInt(), r.ReadVarInt()
	if speedY != speed || speedZ != speed || randomization != 0 {
		t.Errorf("max speeds %v %v %v, randomization %d, want one speed and 0", speed, speedY, speedZ, randomization)
	}
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		t.Fatalf("particle packet: %v, %d bytes left", err, r.Remaining())
	}
	if longDistance || alwaysVisible {
		t.Errorf("flags = %v, %v, want both false", longDistance, alwaysVisible)
	}
	if x != pos.X || y != pos.Y || z != pos.Z {
		t.Errorf("position = (%v %v %v), want the runner's (%v %v %v)", x, y, z, pos.X, pos.Y, pos.Z)
	}
	if count != 5 || speed != 0 {
		t.Errorf("count, speed = %d, %v, want 5, 0", count, speed)
	}
	if id != 39 {
		t.Errorf("particle type = %d, want 39 (minecraft:flame)", id)
	}
	if want := servertest.PlaySoundFeedback("commands.particle.success", protocol.Text{Text: "minecraft:flame"}); !bytes.Equal(chat, want) {
		t.Errorf("feedback = % X, want % X", chat, want)
	}
}

// TestParticleCommandRange checks that a player 40 blocks away gets nothing
// unless the mode is force, and that force still stops at 512 blocks.
func TestParticleCommandRange(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	for _, tc := range []struct {
		name, command string
		wantPackets   int
		wantLong      bool
		wantFeedback  string
	}{
		{"far", "particle minecraft:flame ~40 ~ ~ 0 0 0 0 1", 0, false, "commands.particle.failed"},
		{"far normal", "particle minecraft:flame ~40 ~ ~ 0 0 0 0 1 normal", 0, false, "commands.particle.failed"},
		{"far force", "particle minecraft:flame ~40 ~ ~ 0 0 0 0 1 force", 1, true, "commands.particle.success"},
		{"beyond force", "particle minecraft:flame ~600 ~ ~ 0 0 0 0 1 force", 0, false, "commands.particle.failed"},
		{"near edge", "particle minecraft:flame ~31 ~ ~ 0 0 0 0 1", 1, false, "commands.particle.success"},
	} {
		particles, chat := runParticle(c, tc.command)
		if len(particles) != tc.wantPackets {
			t.Errorf("%s: %d packets, want %d", tc.name, len(particles), tc.wantPackets)
			continue
		}
		if tc.wantPackets == 1 {
			r := protocol.NewReader(particles[0])
			r.ReadVarInt()
			if long := servertest.ReadParticleTail(t, r).LongDistance; long != tc.wantLong {
				t.Errorf("%s: long distance flag = %v, want %v", tc.name, long, tc.wantLong)
			}
		}
		if !bytes.Contains(chat, []byte(tc.wantFeedback)) {
			t.Errorf("%s: feedback = %q, want %s", tc.name, chat, tc.wantFeedback)
		}
	}
}

// particleData returns the particle type ID of a Level Particles packet and
// the options after it, for a packet whose count is under 128.
func particleData(t *testing.T, packet []byte) (int32, []byte) {
	t.Helper()
	const tail = 2 + 3*8 + 3*4 + 3*4 + 1 + 1 // flags, position, spread, speeds, count, randomization
	r := protocol.NewReader(packet)
	id := r.ReadVarInt()
	start := len(packet) - r.Remaining()
	return id, packet[start : len(packet)-tail]
}

// TestParticleCommandWritesOptions checks the data each option shape writes
// after the particle type ID.
func TestParticleCommandWritesOptions(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	stone, _ := registry.FindState("minecraft:stone", nil)
	furnace, _ := registry.FindState("minecraft:furnace", map[string]string{"facing": "east", "lit": "true"})
	stoneItem, _ := registry.ItemID("minecraft:stone")
	argb := func(v uint32) int32 { return int32(v) }
	build := func(f func(b *protocol.Buffer)) []byte {
		var b protocol.Buffer
		f(&b)
		return b.Bytes()
	}
	for _, tc := range []struct {
		name, options string
		want          []byte
	}{
		{"block name", `block{block_state:"stone"}`, build(func(b *protocol.Buffer) { b.PutVarInt(int32(stone)) })},
		{"block_marker state", `block_marker{block_state:{id:"minecraft:furnace",properties:{facing:"east",lit:"true"}}}`,
			build(func(b *protocol.Buffer) { b.PutVarInt(int32(furnace)) })},
		{"falling_dust spaced", `falling_dust{ block_state : "stone" }`, build(func(b *protocol.Buffer) { b.PutVarInt(int32(stone)) })},
		{"dust ints", `dust{color:16711680,scale:2f}`, build(func(b *protocol.Buffer) { b.PutI32(0xFF0000); b.PutF32(2) })},
		{"dust floats", `dust{color:[1,0.5,0],scale:1}`, build(func(b *protocol.Buffer) { b.PutI32(argb(0xFFFF7F00)); b.PutF32(1) })},
		{"dust_color_transition", `dust_color_transition{from_color:255,to_color:[0,1,0],scale:0.5}`,
			build(func(b *protocol.Buffer) { b.PutI32(255); b.PutI32(argb(0xFF00FF00)); b.PutF32(0.5) })},
		{"entity_effect", `entity_effect{color:[1,0,0,0.5]}`, build(func(b *protocol.Buffer) { b.PutI32(argb(0x7FFF0000)) })},
		{"flash int", `flash{color:-1}`, build(func(b *protocol.Buffer) { b.PutI32(-1) })},
		{"effect default", `effect`, build(func(b *protocol.Buffer) { b.PutI32(-1); b.PutF32(1) })},
		{"effect floats", `effect{color:[1,0,0]}`, build(func(b *protocol.Buffer) { b.PutI32(argb(0xFFFF0000)); b.PutF32(1) })},
		{"flame ignores options", `flame{a:1}`, nil},
		{"effect", `effect{color:65280,power:0.25}`, build(func(b *protocol.Buffer) { b.PutI32(65280); b.PutF32(0.25) })},
		{"dragon_breath default", `dragon_breath`, build(func(b *protocol.Buffer) { b.PutF32(1) })},
		{"dragon_breath", `dragon_breath{power:3}`, build(func(b *protocol.Buffer) { b.PutF32(3) })},
		{"geyser", `geyser{water_blocks:4}`, build(func(b *protocol.Buffer) { b.PutI32(4) })},
		{"geyser_base", `geyser_base{water_blocks:2,burst_impulse_base:1.5}`, build(func(b *protocol.Buffer) { b.PutI32(2); b.PutF32(1.5) })},
		{"item name", `item{item:"stone"}`, build(func(b *protocol.Buffer) { b.PutVarInt(stoneItem); b.PutVarInt(1); b.PutVarInt(0); b.PutVarInt(0) })},
		{"item stack", `item{item:{id:"minecraft:stone",count:3}}`, build(func(b *protocol.Buffer) { b.PutVarInt(stoneItem); b.PutVarInt(3); b.PutVarInt(0); b.PutVarInt(0) })},
		{"sculk_charge", `sculk_charge{roll:1.5f}`, build(func(b *protocol.Buffer) { b.PutF32(1.5) })},
		{"shriek", `shriek{delay:300}`, build(func(b *protocol.Buffer) { b.PutVarInt(300) })},
		{"trail", `trail{target:[1.5d,64,-2],color:255,duration:40}`,
			build(func(b *protocol.Buffer) { b.PutF64(1.5); b.PutF64(64); b.PutF64(-2); b.PutI32(255); b.PutVarInt(40) })},
		{"vibration", `vibration{destination:{type:"block",pos:[1,64,-3]},arrival_in_ticks:20}`,
			build(func(b *protocol.Buffer) { b.PutVarInt(0); b.PutPosition(1, 64, -3); b.PutVarInt(20) })},
	} {
		name := tc.options
		for i := range name {
			if name[i] == '{' {
				name = name[:i]
				break
			}
		}
		particles, chat := runParticle(c, "particle "+tc.options+" ~ ~ ~ 0 0 0 0 1")
		if len(particles) != 1 {
			t.Errorf("%s: %d packets, want 1 (feedback %q)", tc.name, len(particles), chat)
			continue
		}
		id, data := particleData(t, particles[0])
		wantID, _, _ := registry.ParticleID("minecraft:" + name)
		if id != wantID || !bytes.Equal(data, tc.want) {
			t.Errorf("%s: type %d data % X, want %d % X", tc.name, id, data, wantID, tc.want)
		}
	}
}

// TestParticleCommandRefusesBadOptions checks that options the codec
// rejects, and a type that takes some but gets none, send nothing.
func TestParticleCommandRefusesBadOptions(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	for _, command := range []string{
		"particle minecraft:block",
		`particle block{block_state:"nothing"}`,
		`particle block{block_state:{id:"furnace",properties:{lit:"maybe"}}}`,
		`particle block{block_state:{Name:"minecraft:furnace",Properties:{facing:"east",lit:"true"}}}`,
		"particle dust{color:1,scale:9}",
		"particle dust{color:1,scale:0}",
		"particle dust{color:[1,0],scale:1}",
		"particle geyser{water_blocks:0}",
		"particle trail{target:[1,2,3],color:1,duration:0}",
		`particle vibration{destination:{type:"entity"},arrival_in_ticks:1}`,
		`particle item{item:"air"}`,
		`particle item{item:{id:"stone",count:100}}`,
		"particle shriek{}",
		"particle effect{color:[1,0,0,1]}",
	} {
		particles, chat := runParticle(c, command+" ~ ~ ~ 0 0 0 0 1")
		if len(particles) != 0 || !bytes.Contains(chat, []byte("particle.invalidOptions")) {
			t.Errorf("%s: %d packets, feedback %q, want 0 and particle.invalidOptions", command, len(particles), chat)
		}
	}
	_, chat := runParticle(c, `particle item{item:{id:"stone",count:100}} ~ ~ ~ 0 0 0 0 1`)
	if !bytes.Contains(chat, []byte("Value must be within range [1;99]: 100")) {
		t.Errorf("count 100: feedback %q, want the range error", chat)
	}
}
