package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestTropicalFishVariantSaveAndLoad(t *testing.T) {
	m := NewMob(TypeTropicalFish, 1, 0, 60, 0, 1)
	m.TropicalVariant, m.FromBucket = 117506305, true
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	out := NewMob(TypeTropicalFish, 2, 0, 60, 0, 2)
	out.LoadFrom(tag, nil, func() int64 { return 0 })
	if out.TropicalVariant != m.TropicalVariant || !out.FromBucket {
		t.Fatalf("saved fish variant=%d, fromBucket=%v", out.TropicalVariant, out.FromBucket)
	}
	for _, tc := range []struct {
		value nbt.Tag
		want  int32
	}{
		{nbt.Double(117506305.75), 117506305}, // Codec.INT truncates.
		{nbt.Int(-1), 0},
		{nbt.Int(117506305), 117506305},
		{nbt.Int(0x100f0501), 0x000f0501},
	} {
		out.LoadFrom(nbt.Compound{"Variant": tc.value}, nil, func() int64 { return 0 })
		if out.TropicalVariant != tc.want {
			t.Errorf("Variant %v = %d, want %d", tc.value, out.TropicalVariant, tc.want)
		}
	}
}
