package world

import (
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
	"testing"
)

func BenchmarkButtonSoundEffect(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockOakButton, registry.BlockCherryButton, registry.BlockStoneButton} {
		b.Run(block.Name(), func(b *testing.B) {
			for b.Loop() {
				if e := buttonEffect(block, true, 1, 64, 1); e.Volume != 1 {
					b.Fatal(e)
				}
			}
		})
	}
}

func BenchmarkPlateSoundEffect(b *testing.B) {
	for _, block := range []registry.Block{registry.BlockOakPressurePlate, registry.BlockCherryPressurePlate, registry.BlockHeavyWeightedPressurePlate} {
		b.Run(block.Name(), func(b *testing.B) {
			for b.Loop() {
				if e := plateEffect(block, true, 1, 64, 1); e.Volume != 1 {
					b.Fatal(e)
				}
			}
		})
	}
}

func BenchmarkSculkResonanceSound(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetBlock(9, GroundLevel, 8, registry.BlockAmethystBlock.DefaultState())
	p := NewPhysics(w)
	b.ReportAllocs()
	for b.Loop() {
		p.effects = p.effects[:0]
		p.sculkResonate(blockKey{8, GroundLevel, 8}, 13, uuid.Nil, uuid.Nil)
	}
}

func BenchmarkSculkGrowthPlacement(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	p.SetSeed(7)
	var changes []BlockChange
	s := p.levelSpreader(blockKey{}, &changes)
	b.ReportAllocs()
	for b.Loop() {
		w.SetBlock(8, GroundLevel+1, 8, airState)
		changes = changes[:0]
		p.effects = p.effects[:0]
		s.growth(8, GroundLevel+1, 8)
	}
}
