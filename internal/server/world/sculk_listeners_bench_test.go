package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkSensorPhaseWrites flips every sculk sensor of one section through
// a phase change, as a vibration heard by all of them at once does.
func BenchmarkSensorPhaseWrites(b *testing.B) {
	const sensors = 64
	idle := registry.MustState("minecraft:sculk_sensor", nil)
	active := registry.MustState("minecraft:sculk_sensor", map[string]string{"sculk_sensor_phase": "active"})
	var s section
	s.setAll(airState)
	for i := range sensors {
		s.set(i*8, idle, false)
	}
	b.ReportAllocs()
	for b.Loop() {
		for i := range sensors {
			s.set(i*8, active, false)
		}
		for i := range sensors {
			s.set(i*8, idle, false)
		}
	}
}

// BenchmarkLoadChunkWithSensors decodes a saved chunk whose sections each hold
// sculk sensors among stone and air.
func BenchmarkLoadChunkWithSensors(b *testing.B) {
	c := newChunk(0, 0)
	sensor := registry.MustState("minecraft:sculk_sensor", nil)
	stone := registry.BlockStone.DefaultState()
	for y := int32(0); y < 128; y += 4 {
		for x := int32(0); x < 16; x += 3 {
			c.SetBlock(x, y, 5, sensor)
			c.SetBlock(x, y+1, 6, stone)
		}
	}
	root, err := encodeChunkNBTWith(c, nil, chunkTicks{})
	if err != nil {
		b.Fatal(err)
	}
	enc, err := nbt.AppendEncode(nil, "", root)
	if err != nil {
		b.Fatal(err)
	}
	_, decoded, err := nbt.Decode(enc)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decodeChunkNBT(decoded.(nbt.Compound)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClearListenerSection fills a section with sculk sensors and clears
// it cell by cell, as a batched replacement does.
func BenchmarkClearListenerSection(b *testing.B) {
	sensor := registry.MustState("minecraft:sculk_sensor", nil)
	var s section
	b.ReportAllocs()
	for b.Loop() {
		s.setAll(sensor)
		for i := range blocksPerSection {
			s.set(i, airState, false)
		}
	}
}
