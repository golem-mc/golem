package plugins_test

import (
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
