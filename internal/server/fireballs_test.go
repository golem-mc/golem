package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestDangerousSkullResistanceCapsTheFluid(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	resistance := dangerousSkullResistanceOf(w)
	wet, ok := registry.WithBool(registry.BlockOakSlab.DefaultState(), registry.PropWaterlogged, true)
	if !ok {
		t.Fatal("oak slab has no waterlogged property")
	}
	for _, tc := range []struct {
		name    string
		state   uint16
		present bool
	}{
		{"cave_air", registry.BlockCaveAir.DefaultState(), false},
		{"torch", registry.BlockTorch.DefaultState(), false},
		{"bedrock", registry.BlockBedrock.DefaultState(), false},
		{"bubble_column", registry.BlockBubbleColumn.DefaultState(), true},
		{"wet_slab", wet, true},
		{"water", registry.BlockWater.DefaultState(), true},
		{"lava", registry.BlockLava.DefaultState(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !w.SetBlock(8, 80, 8, tc.state) {
				t.Fatal("block not set")
			}
			want := 0.0
			if tc.present {
				want = 0.8
			}
			if got, present := resistance(8, 80, 8); got != want || present != tc.present {
				t.Fatalf("resistance = %g, %v; want %g, %v", got, present, want, tc.present)
			}
		})
	}
}
