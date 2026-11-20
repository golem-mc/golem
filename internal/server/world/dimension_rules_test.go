package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestDimensionTypeRulesByType checks that a custom-named dimension built from
// a built-in dimension type inherits that type's rules rather than the
// overworld's.
// vanilla: DimensionType, EnvironmentAttributes
func TestDimensionTypeRulesByType(t *testing.T) {
	t.Parallel()
	netherShape, err := ShapeOfType(NetherType, nil)
	if err != nil {
		t.Fatalf("ShapeOfType(the_nether): %v", err)
	}
	nether := NewCustomDimensionWorld(t.TempDir(), "test:nether", netherShape, NewFlatGenerator())
	t.Cleanup(func() { _ = nether.Close() })
	if !nether.FastLava() || !nether.WaterEvaporates() || !nether.RespawnAnchorWorks() {
		t.Errorf("nether-type world: fastLava=%v waterEvap=%v anchor=%v, want all true",
			nether.FastLava(), nether.WaterEvaporates(), nether.RespawnAnchorWorks())
	}
	if nether.PiglinsZombify() || nether.HasSkyLight() || !nether.HasCeiling() || nether.CanStartRaid() {
		t.Errorf("nether-type world: piglins=%v skyLight=%v ceiling=%v raid=%v, want false false true false",
			nether.PiglinsZombify(), nether.HasSkyLight(), nether.HasCeiling(), nether.CanStartRaid())
	}
	if got := nether.AmbientLight(); got != 0.1 {
		t.Errorf("nether-type AmbientLight = %v, want 0.1", got)
	}
	if !nether.SnowGolemMelts(0, 64, 0) {
		t.Error("nether-type SnowGolemMelts = false, want true")
	}
	if bl, lo, hi := nether.MonsterSpawnLight(); bl != 15 || lo != 7 || hi != 7 {
		t.Errorf("nether-type MonsterSpawnLight = (%d, %d, %d), want (15, 7, 7)", bl, lo, hi)
	}

	overShape, err := ShapeOfType(DimensionType, nil)
	if err != nil {
		t.Fatalf("ShapeOfType(overworld): %v", err)
	}
	over := NewCustomDimensionWorld(t.TempDir(), "test:over", overShape, NewFlatGenerator())
	t.Cleanup(func() { _ = over.Close() })
	if over.FastLava() || over.WaterEvaporates() || over.RespawnAnchorWorks() {
		t.Errorf("overworld-type world: fastLava=%v waterEvap=%v anchor=%v, want all false",
			over.FastLava(), over.WaterEvaporates(), over.RespawnAnchorWorks())
	}
	if !over.PiglinsZombify() || !over.HasSkyLight() || over.HasCeiling() || !over.CanStartRaid() {
		t.Errorf("overworld-type world: piglins=%v skyLight=%v ceiling=%v raid=%v, want true true false true",
			over.PiglinsZombify(), over.HasSkyLight(), over.HasCeiling(), over.CanStartRaid())
	}
	if got := over.AmbientLight(); got != 0 {
		t.Errorf("overworld-type AmbientLight = %v, want 0", got)
	}
	if bl, lo, hi := over.MonsterSpawnLight(); bl != 0 || lo != 0 || hi != 7 {
		t.Errorf("overworld-type MonsterSpawnLight = (%d, %d, %d), want (0, 0, 7)", bl, lo, hi)
	}

	endShape, err := ShapeOfType(EndType, nil)
	if err != nil {
		t.Fatalf("ShapeOfType(the_end): %v", err)
	}
	end := NewCustomDimensionWorld(t.TempDir(), "test:end", endShape, NewFlatGenerator())
	t.Cleanup(func() { _ = end.Close() })
	if !end.HasSkyLight() || end.HasCeiling() || end.AmbientLight() != 0 {
		t.Errorf("end-type world: skyLight=%v ceiling=%v ambient=%v, want true false 0",
			end.HasSkyLight(), end.HasCeiling(), end.AmbientLight())
	}
	if bl, lo, hi := end.MonsterSpawnLight(); bl != 0 || lo != 15 || hi != 15 {
		t.Errorf("end-type MonsterSpawnLight = (%d, %d, %d), want (0, 15, 15)", bl, lo, hi)
	}
}

// TestShapeOfTypeCustomMonsterLightAndAmbient checks that ShapeOfType decodes
// ambient_light, monster_spawn_block_light_limit, and both constant and uniform
// monster_spawn_light_level providers from a custom pack dimension type.
// vanilla: DimensionType.DIRECT_CODEC
func TestShapeOfTypeCustomMonsterLightAndAmbient(t *testing.T) {
	t.Parallel()
	uniformData := nbt.Compound{
		"min_y":                           nbt.Int(0),
		"height":                          nbt.Int(256),
		"logical_height":                  nbt.Int(128),
		"has_skylight":                    nbt.Byte(0),
		"has_ceiling":                     nbt.Byte(1),
		"ambient_light":                   nbt.Float(0.25),
		"monster_spawn_block_light_limit": nbt.Int(11),
		"monster_spawn_light_level": nbt.Compound{
			"type":          nbt.String("minecraft:uniform"),
			"min_inclusive": nbt.Int(2),
			"max_inclusive": nbt.Int(9),
		},
	}
	shape, err := ShapeOfType("pk:uniform", uniformData)
	if err != nil {
		t.Fatalf("ShapeOfType(pk:uniform): %v", err)
	}
	w := NewCustomDimensionWorld(t.TempDir(), "pk:u", shape, NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	if w.HasSkyLight() || !w.HasCeiling() || w.AmbientLight() != 0.25 {
		t.Errorf("uniform type: sky=%v ceiling=%v ambient=%v, want false true 0.25",
			w.HasSkyLight(), w.HasCeiling(), w.AmbientLight())
	}
	if bl, lo, hi := w.MonsterSpawnLight(); bl != 11 || lo != 2 || hi != 9 {
		t.Errorf("uniform MonsterSpawnLight = (%d, %d, %d), want (11, 2, 9)", bl, lo, hi)
	}

	constData := nbt.Compound{
		"min_y":                           nbt.Int(0),
		"height":                          nbt.Int(256),
		"logical_height":                  nbt.Int(256),
		"has_skylight":                    nbt.Byte(1),
		"monster_spawn_block_light_limit": nbt.Int(5),
		"monster_spawn_light_level":       nbt.Int(6),
	}
	cShape, err := ShapeOfType("pk:const", constData)
	if err != nil {
		t.Fatalf("ShapeOfType(pk:const): %v", err)
	}
	cw := NewCustomDimensionWorld(t.TempDir(), "pk:c", cShape, NewFlatGenerator())
	t.Cleanup(func() { _ = cw.Close() })
	if bl, lo, hi := cw.MonsterSpawnLight(); bl != 5 || lo != 6 || hi != 6 {
		t.Errorf("const MonsterSpawnLight = (%d, %d, %d), want (5, 6, 6)", bl, lo, hi)
	}

	// Verify meltIce uses WaterEvaporates (not DimensionName).
	iceWorld := NewCustomDimensionWorld(t.TempDir(), "test:nether_ice", mustNetherShape(t), NewFlatGenerator())
	t.Cleanup(func() { _ = iceWorld.Close() })
	p := NewPhysics(iceWorld)
	p.w.Acquire(ChunkPos{X: 0, Z: 0})
	defer p.w.Release(ChunkPos{X: 0, Z: 0})
	ice := registry.MustState("minecraft:ice", nil)
	p.w.SetBlock(4, 10, 4, ice)
	var changes []BlockChange
	var effects []Effect
	p.meltIce(4, 10, 4, ice, &changes, &effects)
	if got := p.w.BlockAt(4, 10, 4); !IsKnownAirState(got) {
		t.Errorf("ice melted in nether-type world left state %d, want air", got)
	}
}

func mustNetherShape(t *testing.T) DimensionShape {
	t.Helper()
	s, err := ShapeOfType(NetherType, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
