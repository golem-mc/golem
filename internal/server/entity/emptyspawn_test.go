package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestEmptySpawnBlockRefusesOnlyAFullCollision: a mob spawns inside a cell
// whose collision is not the whole cube, so a carpet, a lily pad and a slab
// pass, and a coral block does not; a cell holding fluid or a signal source
// is refused whatever its shape.
// vanilla: NaturalSpawner.isValidEmptySpawnBlock
func TestEmptySpawnBlockRefusesOnlyAFullCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block string
		props map[string]string
		want  bool
	}{
		{"minecraft:air", nil, true},
		{"minecraft:white_carpet", nil, true},
		{"minecraft:lily_pad", nil, true},
		{"minecraft:stone_slab", map[string]string{"type": "bottom"}, true},
		{"minecraft:tube_coral_block", nil, false},
		{"minecraft:stone", nil, false},
		{"minecraft:rail", nil, false},
		{"minecraft:water", nil, false},
		{"minecraft:stone_slab", map[string]string{"type": "bottom", "waterlogged": "true"}, false},
		{"minecraft:oak_fence", map[string]string{"waterlogged": "true"}, false},
		{"minecraft:oak_fence", map[string]string{"waterlogged": "false"}, true},
		{"minecraft:repeater", nil, false},
		{"minecraft:comparator", nil, false},
		{"minecraft:daylight_detector", nil, false},
		{"minecraft:lectern", nil, false},
		{"minecraft:trapped_chest", nil, false},
		{"minecraft:sculk_sensor", nil, false},
		{"minecraft:lightning_rod", nil, false},
		{"minecraft:waxed_oxidized_lightning_rod", nil, false},
	} {
		if got := emptySpawnBlock(TypeZombie, registry.MustState(tc.block, tc.props)); got != tc.want {
			t.Errorf("emptySpawnBlock(TypeZombie, %s %v) = %v, want %v", tc.block, tc.props, got, tc.want)
		}
	}
}

// vanilla: EntityType.isBlockDangerous, EntityTypes builders' immuneTo tags
func TestEmptySpawnBlockHonoursTheMobsHazardImmunity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ   MobType
		block registry.Block
		want  bool
	}{
		{TypeHusk, registry.BlockWitherRose, false},
		{TypeBogged, registry.BlockSweetBerryBush, false},
		{TypeCaveSpider, registry.BlockCactus, false},
		{TypeZombieVillager, registry.BlockPowderSnow, false},
		{TypeStray, registry.BlockPowderSnow, true},
		{TypeStray, registry.BlockFire, false},
		{TypeMagmaCube, registry.BlockFire, true},
		{TypeMagmaCube, registry.BlockSoulFire, true},
		{TypeMagmaCube, registry.BlockSweetBerryBush, false},
		{TypeMagmaCube, registry.BlockPowderSnow, false},
		{TypeMagmaCube, registry.BlockLava, false},
		{TypeSlime, registry.BlockFire, false},
		{TypeWitch, registry.BlockWitherRose, false},
		{TypeWitherSkeleton, registry.BlockWitherRose, true},
		{TypeFox, registry.BlockSweetBerryBush, true},
		{TypePolarBear, registry.BlockPowderSnow, true},
		{TypeSnowGolem, registry.BlockPowderSnow, true},
	} {
		t.Run(tc.typ.Name()+"/"+tc.block.Name(), func(t *testing.T) {
			if got := emptySpawnBlock(tc.typ, tc.block.DefaultState()); got != tc.want {
				t.Errorf("empty = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSpawnPlacementChecksHazardsAtFeetAndHead(t *testing.T) {
	t.Parallel()
	w := newSpawnTestWorld()
	w.groundAt(t, 0, 63, 0)
	for _, y := range []int32{64, 65} {
		w.set(0, y, 0, registry.BlockPowderSnow.DefaultState())
		if spawnPositionOk(w, TypeZombie, 0, 64, 0) {
			t.Errorf("zombie accepted powder snow at y=%d", y)
		}
		if !spawnPositionOk(w, TypeStray, 0, 64, 0) {
			t.Errorf("stray refused powder snow at y=%d", y)
		}
		w.set(0, y, 0, registry.BlockFire.DefaultState())
		if spawnPositionOk(w, TypeSlime, 0, 64, 0) {
			t.Errorf("slime accepted fire at y=%d", y)
		}
		if !spawnPositionOk(w, TypeMagmaCube, 0, 64, 0) {
			t.Errorf("magma cube refused fire at y=%d", y)
		}
		w.set(0, y, 0, registry.BlockAir.DefaultState())
	}
}

// TestSpawnBlockDangerousCountsLitCampfiresAndLavaCauldrons: a lit campfire
// and a lava cauldron burn a mob that is not fire immune; an unlit campfire
// does not. vanilla: NodeEvaluator.isBurningBlock
func TestSpawnBlockDangerousCountsLitCampfiresAndLavaCauldrons(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		typ   MobType
		block string
		props map[string]string
		want  bool
	}{
		{TypeZombie, "minecraft:campfire", map[string]string{"lit": "true"}, true},
		{TypeZombie, "minecraft:soul_campfire", map[string]string{"lit": "true"}, true},
		{TypeZombie, "minecraft:campfire", map[string]string{"lit": "false"}, false},
		{TypeZombie, "minecraft:lava_cauldron", nil, true},
		{TypeZombie, "minecraft:water_cauldron", nil, false},
		{TypeMagmaCube, "minecraft:campfire", map[string]string{"lit": "true"}, false},
		{TypeMagmaCube, "minecraft:lava_cauldron", nil, false},
	} {
		state := registry.MustState(tc.block, tc.props)
		if got := spawnBlockDangerous(tc.typ, state); got != tc.want {
			t.Errorf("spawnBlockDangerous(%s, %s %v) = %v, want %v", tc.typ.Name(), tc.block, tc.props, got, tc.want)
		}
		if got := emptySpawnBlock(tc.typ, state); got == tc.want {
			t.Errorf("emptySpawnBlock(%s, %s %v) = %v, want %v", tc.typ.Name(), tc.block, tc.props, got, !tc.want)
		}
	}
}
