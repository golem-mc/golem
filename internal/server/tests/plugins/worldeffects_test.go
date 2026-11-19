package plugins_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func worldEffectsPlugin(t *testing.T) (*server.Server, plugin.Server, func(func()), *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Effects")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	return srv, h, run, c
}

// vanilla: ServerLevel.playSeededSound/levelEvent/sendParticles.
func TestPluginWorldEffects(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: p.X - .375, Y: p.Y + 1.25, Z: p.Z + .625}
	run(func() {
		w := h.Overworld()
		w.PlaySound(pos, registry.SoundBlockNoteBlockBell, plugin.SoundBlocks, .75, 1.5)
		w.SpawnParticles(plugin.Particle{Type: protocol.ParticleFlame}, pos, 7, plugin.Vec3{X: .1, Y: .2, Z: .3}, .5)
		w.LevelEvent(protocol.WorldEventBlockBreak, pos.Block(), int32(registry.BlockStone.DefaultState()))
	})
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySoundEffect))
	if r.ReadVarInt() != int32(registry.SoundBlockNoteBlockBell) || r.ReadVarInt() != protocol.SoundCategoryBlock ||
		r.ReadI32() != int32(pos.X*8) || r.ReadI32() != int32(pos.Y*8) || r.ReadI32() != int32(pos.Z*8) || r.ReadF32() != .75 || r.ReadF32() != 1.5 {
		t.Fatal("sound fields")
	}
	r.ReadI64() // A shared random seed chooses the same sound variant for all recipients.
	if r.Err() != nil || r.Remaining() != 0 {
		t.Fatal("sound layout")
	}
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayLevelParticles))
	if r.ReadVarInt() != protocol.ParticleFlame || r.ReadBool() || r.ReadBool() || r.ReadF64() != pos.X || r.ReadF64() != pos.Y || r.ReadF64() != pos.Z ||
		r.ReadF32() != .1 || r.ReadF32() != .2 || r.ReadF32() != .3 || r.ReadF32() != .5 || r.ReadF32() != .5 || r.ReadF32() != .5 || r.ReadVarInt() != 7 || r.ReadVarInt() != 0 || r.Err() != nil || r.Remaining() != 0 {
		t.Fatal("particle fields")
	}
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayWorldEvent))
	event := r.ReadI32()
	x, y, z := r.ReadPosition()
	if event != protocol.WorldEventBlockBreak || (plugin.BlockPos{X: int(x), Y: int(y), Z: int(z)}) != pos.Block() ||
		r.ReadI32() != int32(registry.BlockStone.DefaultState()) || r.ReadBool() || r.Err() != nil || r.Remaining() != 0 {
		t.Fatal("level event fields")
	}
	if got := srv.OverworldWorld().BlockAt(x, y, z); got != uint16(plugin.DefaultState(registry.BlockAir)) {
		t.Fatal("a visual block-break event changed terrain")
	}
}

// vanilla: SoundEvent.getRange, ServerLevel.sendParticles, BlockPos.closerToCenterThan.
func TestPluginWorldEffectRanges(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	for _, tc := range []struct {
		name     string
		distance float64
		volume   float32
		long     bool
		particle bool
		want     bool
	}{
		{"quiet nearby", 15.999, .25, false, false, true},
		{"quiet boundary", 16, .25, false, false, false},
		{"normal boundary", 16, 1, false, false, false},
		{"loud nearby", 31.999, 2, false, false, true},
		{"loud boundary", 32, 2, false, false, false},
		{"particle nearby", 31.999, 0, false, true, true},
		{"particle boundary", 32, 0, false, true, false},
		{"long nearby", 511.999, 0, true, true, true},
		{"long boundary", 512, 0, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pos := plugin.Vec3{X: p.X + tc.distance, Y: p.Y, Z: p.Z}
			id := int32(protocol.IDPlaySoundEffect)
			if tc.particle {
				id = protocol.IDPlayLevelParticles
				pos = plugin.Vec3{X: math.Floor(p.X) + .5 + tc.distance, Y: math.Floor(p.Y) + .5, Z: math.Floor(p.Z) + .5}
			}
			run(func() {
				if tc.particle {
					h.Overworld().SpawnParticles(plugin.Particle{Type: protocol.ParticleFlame, Long: tc.long}, pos, 1, plugin.Vec3{}, 0)
				} else {
					h.Overworld().PlaySound(pos, registry.SoundBlockNoteBlockBell, plugin.SoundMaster, tc.volume, 1)
				}
			})
			if tc.want {
				data := c.ExpectWithinTicks(srv, 0, id)
				if tc.particle {
					r := protocol.NewReader(data)
					r.ReadVarInt()
					if r.ReadBool() != tc.long || r.ReadBool() || r.Err() != nil {
						t.Fatal("particle limiter flags")
					}
				}
			} else {
				c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(id))
			}
		})
	}
	// Level events measure from the block corner, rather than the centre.
	c.MoveTo(0, 64, 0, 0, 0)
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	for _, x := range []int{63, 64} {
		run(func() { h.Overworld().LevelEvent(protocol.WorldEventBlockBreak, plugin.BlockPos{X: x, Y: 64}, 1) })
		if x == 63 {
			c.ExpectWithinTicks(srv, 0, protocol.IDPlayWorldEvent)
		} else {
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayWorldEvent))
		}
	}
}

func TestPluginWorldEffectsStayInDimension(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	run(func() {
		end, _ := h.World(plugin.MinecraftKey("the_end"))
		end.PlaySound(pos, registry.SoundBlockNoteBlockBell, plugin.SoundBlocks, 100, 1)
		end.SpawnParticles(plugin.Particle{Type: protocol.ParticleFlame, Long: true}, pos, 1, plugin.Vec3{}, 0)
		end.LevelEvent(protocol.WorldEventBlockBreak, pos.Block(), 1)
	})
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySoundEffect, protocol.IDPlayLevelParticles, protocol.IDPlayWorldEvent))
}

func TestPluginWorldEffectsBroadcast(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	servertest.ReleaseTicks(t, srv)
	other := servertest.DialServer(t, srv)
	other.ViewAsk = config.MinDistance
	other.Join("OtherEffects")
	servertest.HoldTicks(t, srv)
	p := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	run(func() {
		h.Overworld().PlaySound(pos, registry.SoundBlockNoteBlockBell, plugin.SoundBlocks, 2, 1)
		h.Overworld().SpawnParticles(plugin.Particle{Type: protocol.ParticleFlame}, pos, 1, plugin.Vec3{}, 0)
		h.Overworld().LevelEvent(protocol.WorldEventBlockBreak, pos.Block(), 1)
	})
	for _, id := range []int32{protocol.IDPlaySoundEffect, protocol.IDPlayLevelParticles, protocol.IDPlayWorldEvent} {
		a, b := c.ExpectWithinTicks(srv, 0, id), other.ExpectWithinTicks(srv, 0, id)
		if !bytes.Equal(a, b) {
			t.Fatalf("packet %x differs across recipients: %x / %x", id, a, b)
		}
	}
}

// vanilla: the ParticleOptions stream codecs and ClientboundLevelParticlesPacket.
func TestPluginWorldParticleOptions(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	stairs, _ := registry.WithProp(registry.BlockOakStairs.DefaultState(), registry.PropFacing, "west")
	item := plugin.NewStack(registry.ItemDiamond, 2)
	item.Patch.CustomName = new(plugin.Plain("Particle diamond"))
	for _, tc := range []struct {
		particle plugin.Particle
		options  func(*protocol.Buffer)
	}{
		{plugin.Particle{Type: protocol.ParticleFlame}, func(*protocol.Buffer) {}},
		{plugin.Particle{Type: protocol.ParticleDust, Color: 0x123456, Size: 2}, func(b *protocol.Buffer) { b.PutI32(0x123456); b.PutF32(2) }},
		{plugin.Particle{Type: protocol.ParticleDustColorTransition, Color: 0x123456, To: 0xabcdef, Size: .5}, func(b *protocol.Buffer) { b.PutI32(0x123456); b.PutI32(0xabcdef); b.PutF32(.5) }},
		{plugin.Particle{Type: protocol.ParticleEntityEffect, Color: 0x80123456}, func(b *protocol.Buffer) { b.PutI32(-2146290602) }},
		{plugin.Particle{Type: protocol.ParticleTintedLeaves, Color: 0x123456}, func(b *protocol.Buffer) { b.PutI32(0x123456) }},
		{plugin.Particle{Type: protocol.ParticleFlash, Color: 0x123456}, func(b *protocol.Buffer) { b.PutI32(0x123456) }},
		{plugin.Particle{Type: protocol.ParticleEffect, Color: 0x123456, Power: new(float32(2))}, func(b *protocol.Buffer) { b.PutI32(0x123456); b.PutF32(2) }},
		{plugin.Particle{Type: protocol.ParticleInstantEffect, Color: 0x123456}, func(b *protocol.Buffer) { b.PutI32(0x123456); b.PutF32(1) }},
		{plugin.Particle{Type: protocol.ParticleDragonBreath}, func(b *protocol.Buffer) { b.PutF32(1) }},
		{plugin.Particle{Type: protocol.ParticleDragonBreath, Power: new(float32(.25))}, func(b *protocol.Buffer) { b.PutF32(.25) }},
		{plugin.Particle{Type: protocol.ParticleSculkCharge, Roll: .25}, func(b *protocol.Buffer) { b.PutF32(.25) }},
		{plugin.Particle{Type: protocol.ParticleShriek, Delay: 4}, func(b *protocol.Buffer) { b.PutVarInt(4) }},
		{plugin.Particle{Type: protocol.ParticleTrail, Target: plugin.Vec3{X: 2, Y: 3, Z: 4}, Color: 123, Duration: 20}, func(b *protocol.Buffer) { b.PutF64(2); b.PutF64(3); b.PutF64(4); b.PutI32(123); b.PutVarInt(20) }},
		{plugin.Particle{Type: protocol.ParticleVibration, Target: plugin.Vec3{X: -2.25, Y: 3.75, Z: 4.5}, ArrivalTicks: 5}, func(b *protocol.Buffer) { b.PutVarInt(0); b.PutPosition(-3, 3, 4); b.PutVarInt(5) }},
		{plugin.Particle{Type: protocol.ParticleGeyser, WaterBlocks: 3}, func(b *protocol.Buffer) { b.PutI32(3) }},
		{plugin.Particle{Type: protocol.ParticleGeyserPlume, WaterBlocks: 3}, func(b *protocol.Buffer) { b.PutI32(3) }},
		{plugin.Particle{Type: protocol.ParticleGeyserBase, WaterBlocks: 3, BurstImpulse: 1.5}, func(b *protocol.Buffer) { b.PutI32(3); b.PutF32(1.5) }},
		{plugin.Particle{Type: protocol.ParticleGeyserPoof, WaterBlocks: 3, BurstImpulse: 1.5}, func(b *protocol.Buffer) { b.PutI32(3); b.PutF32(1.5) }},
		{plugin.Particle{Type: protocol.ParticleItem, Item: &item}, func(b *protocol.Buffer) {
			b.PutVarInt(int32(registry.ItemDiamond))
			b.PutVarInt(2)
			b.PutVarInt(1)
			b.PutVarInt(0)
			b.PutVarInt(protocol.DataComponentCustomName)
			b.PutText(protocol.Text{Text: "Particle diamond"})
		}},
	} {
		run(func() { h.Overworld().SpawnParticles(tc.particle, pos, 0, plugin.Vec3{X: .1, Y: .2, Z: .3}, .75) })
		got := c.ExpectWithinTicks(srv, 0, protocol.IDPlayLevelParticles)
		var want protocol.Buffer
		want.PutVarInt(int32(tc.particle.Type))
		tc.options(&want)
		want.PutBool(false)
		want.PutBool(false)
		want.PutF64(pos.X)
		want.PutF64(pos.Y)
		want.PutF64(pos.Z)
		want.PutF32(.1)
		want.PutF32(.2)
		want.PutF32(.3)
		for range 3 {
			want.PutF32(.75)
		}
		want.PutVarInt(0)
		want.PutVarInt(0)
		if !bytes.Equal(got, want.Bytes()) {
			t.Fatalf("particle %d = %x, want %x", tc.particle.Type, got, want.Bytes())
		}
	}
	for _, id := range []int32{protocol.ParticleBlock, protocol.ParticleBlockMarker, protocol.ParticleFallingDust, protocol.ParticleDustPillar, protocol.ParticleBlockCrumble} {
		run(func() {
			h.Overworld().SpawnParticles(plugin.Particle{Type: plugin.ParticleType(id), State: plugin.BlockState(stairs)}, pos, 2, plugin.Vec3{}, 0)
		})
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayLevelParticles))
		if r.ReadVarInt() != id || r.ReadVarInt() != int32(stairs) || r.Err() != nil {
			t.Fatalf("block particle %d lost its state properties", id)
		}
	}
}

func TestPluginWorldEffectsRejectInvalidIDs(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	pos := plugin.Vec3{X: p.X, Y: p.Y, Z: p.Z}
	run(func() {
		w := h.Overworld()
		for _, sound := range []plugin.Sound{registry.SoundNone, -1, plugin.Sound(len(registry.SoundEvents) + 1)} {
			w.PlaySound(pos, sound, plugin.SoundMaster, 1, 1)
		}
		w.PlaySound(pos, registry.SoundBlockNoteBlockBell, plugin.SoundCategory(255), 1, 1)
		for _, particle := range []plugin.Particle{
			{Type: -1}, {Type: plugin.ParticleType(len(registry.ParticleTypes))},
			{Type: protocol.ParticleDust, Size: 0}, {Type: protocol.ParticleGeyser, WaterBlocks: 0},
			{Type: protocol.ParticleItem}, {Type: protocol.ParticleItem, Item: new(plugin.ItemStack{})},
		} {
			w.SpawnParticles(particle, pos, 1, plugin.Vec3{}, 0)
		}
		wrapped := pos.Block()
		wrapped.X += 1 << 32
		w.LevelEvent(protocol.WorldEventBlockBreak, wrapped, 1)
	})
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySoundEffect, protocol.IDPlayLevelParticles, protocol.IDPlayWorldEvent))
}

// vanilla: ServerLevel.levelEvent broadcasts; JukeboxSongPlayer.tick emits the separate game event.
func TestPluginWorldLevelEventDoesNotEmitGameEvent(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	srv.SetBlock(3, 64, 0, registry.BlockJukebox.DefaultState())
	id := srv.SpawnMob("minecraft:allay", 2.5, 65, .5)
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	run(func() { h.Overworld().LevelEvent(protocol.WorldEventJukeboxPlay, plugin.BlockPos{X: 3, Y: 64}, 0) })
	srv.TickMobs(1)
	sawEvent := false
	c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlayWorldEvent {
			r := protocol.NewReader(data)
			sawEvent = r.ReadI32() == protocol.WorldEventJukeboxPlay
		}
		if packet == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			entries, ok := servertest.ReadMetadata(r)
			if !ok {
				t.Fatal("malformed allay metadata")
			}
			if dancing := servertest.MetaOf(entries, protocol.MetadataIndexAllayDancing); dancing != nil && dancing.FlagVal {
				t.Error("a client level event made an allay dance without a game event")
			}
		}
		return false
	})
	if !sawEvent {
		t.Fatal("jukebox level event was not broadcast")
	}
}

// explodeSeen runs f on the tick and reports whether the client was sent an
// explosion packet and a block change in that tick.
func explodeSeen(srv *server.Server, c *servertest.Client, run func(func()), f func()) (explosion, blocks bool) {
	run(f)
	c.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlayExplosion:
			explosion = true
		case protocol.IDPlaySectionBlocks, protocol.IDPlayBlockUpdate:
			blocks = true
		}
		return false
	})
	return explosion, blocks
}

// fillCube sets the cube x0..x0+4, y0..y0+4, z0..z0+4 to state.
func fillCube(srv *server.Server, x0, y0, z0 int32, state uint16) {
	for x := x0; x < x0+5; x++ {
		for y := y0; y < y0+5; y++ {
			for z := z0; z < z0+5; z++ {
				srv.SetBlock(x, y, z, state)
			}
		}
	}
}

func countBlocks(w *world.World, x0, y0, z0 int32, state uint16) (n int) {
	for x := x0; x < x0+5; x++ {
		for y := y0; y < y0+5; y++ {
			for z := z0; z < z0+5; z++ {
				if w.BlockAt(x, y, z) == state {
					n++
				}
			}
		}
	}
	return n
}

func droppedDirt(srv *server.Server) (n int) {
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		n += int(it.Count)
	}
	return n
}

// vanilla: Level.explode, ServerExplosion.explode, ServerLevel.explode.
func TestPluginExplode(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	w := srv.OverworldWorld()
	dirt, air := uint16(registry.BlockDirt.DefaultState()), uint16(registry.BlockAir.DefaultState())
	const x0, y0, z0 = 8, 69, -2
	at := plugin.Vec3{X: 10.5, Y: 71.5, Z: 0.5}
	fillCube(srv, x0, y0, z0, dirt)

	explosion, blocks := explodeSeen(srv, c, run, func() { h.Overworld().Explode(at, 4, false, plugin.ExplosionKeep, nil) })
	if !explosion || blocks || countBlocks(w, x0, y0, z0, dirt) != 125 {
		t.Fatalf("keep: explosion packet %v, block packet %v, dirt %d; want a packet, no block change", explosion, blocks, countBlocks(w, x0, y0, z0, dirt))
	}
	for _, bad := range []struct {
		power  float32
		blocks plugin.ExplosionBlocks
	}{{-1, plugin.ExplosionDestroy}, {float32(math.NaN()), plugin.ExplosionDestroy}, {4, plugin.ExplosionTriggerBlocks + 1}} {
		explosion, blocks = explodeSeen(srv, c, run, func() { h.Overworld().Explode(at, bad.power, false, bad.blocks, nil) })
		if explosion || blocks || countBlocks(w, x0, y0, z0, dirt) != 125 {
			t.Fatalf("rejected blast %+v did something: packet %v, blocks %v", bad, explosion, blocks)
		}
	}

	explosion, blocks = explodeSeen(srv, c, run, func() { h.Overworld().Explode(at, 4, false, plugin.ExplosionDestroy, nil) })
	gone := 125 - countBlocks(w, x0, y0, z0, dirt)
	if !explosion || !blocks || gone < 60 || w.BlockAt(10, 71, 0) != air {
		t.Fatalf("destroy: explosion packet %v, block packet %v, %d blocks gone", explosion, blocks, gone)
	}
	if got := droppedDirt(srv); got != gone {
		t.Fatalf("destroy dropped %d dirt for %d blocks, want all", got, gone)
	}
}

// vanilla: ServerExplosion.interactWithBlocks, BlockInteraction.DESTROY_WITH_DECAY.
func TestPluginExplodeDecayDropsFewer(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	w := srv.OverworldWorld()
	dirt := uint16(registry.BlockDirt.DefaultState())
	fillCube(srv, 8, 69, -2, dirt)
	run(func() {
		h.Overworld().Explode(plugin.Vec3{X: 10.5, Y: 71.5, Z: 0.5}, 4, false, plugin.ExplosionDestroyWithDecay, nil)
	})
	gone := 125 - countBlocks(w, 8, 69, -2, dirt)
	if got := droppedDirt(srv); gone < 60 || got >= gone || got == 0 {
		t.Fatalf("decay dropped %d dirt for %d blocks, want some but not all", got, gone)
	}
}

// vanilla: ServerExplosion.canTriggerBlocks, BlockBehaviour.onExplosionHit.
func TestPluginExplodeTriggersBlocksWithoutBreaking(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	w := srv.OverworldWorld()
	lever, ok := registry.FindState("minecraft:lever", map[string]string{"face": "floor", "facing": "north", "powered": "false"})
	if !ok {
		t.Fatal("no lever state")
	}
	srv.SetBlock(10, 64, 0, lever)
	run(func() {
		h.Overworld().Explode(plugin.Vec3{X: 10.5, Y: 64.5, Z: 1.5}, 1, false, plugin.ExplosionTriggerBlocks, nil)
	})
	state := w.BlockAt(10, 64, 0)
	if registry.BlockOf(state) != registry.BlockLever || !registry.StatePowered(state) {
		t.Fatalf("lever state %d after a trigger blast, want a pulled lever", state)
	}
}

// vanilla: ServerExplosion.createFire (explode, fire true).
func TestPluginExplodeFire(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	w := srv.OverworldWorld()
	fires := func(fire bool) (n int) {
		for x := int32(6); x <= 14; x++ {
			for z := int32(-4); z <= 4; z++ {
				srv.SetBlock(x, 63, z, registry.BlockDirt.DefaultState())
			}
		}
		run(func() {
			h.Overworld().Explode(plugin.Vec3{X: 10.5, Y: 64.5, Z: 0.5}, 4, fire, plugin.ExplosionDestroy, nil)
		})
		for x := int32(4); x <= 16; x++ {
			for y := int32(60); y <= 66; y++ {
				for z := int32(-6); z <= 6; z++ {
					if registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockFire {
						n++
					}
				}
			}
		}
		return n
	}
	if n := fires(false); n != 0 {
		t.Fatalf("a blast without fire set %d fires", n)
	}
	if n := fires(true); n == 0 {
		t.Fatal("a blast with fire set no fire")
	}
}

// Remove leaves a player alone: no kill, no death.
func TestPluginRemoveLeavesPlayers(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	run(func() {
		ps := h.Players()
		if len(ps) != 1 {
			t.Fatalf("players=%d", len(ps))
		}
		ps[0].Remove()
		if !ps[0].IsValid() || srv.PlayerHealth("Effects") <= 0 {
			t.Error("Remove hurt the player")
		}
	})
}
