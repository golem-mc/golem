package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// diceFor scans the seeds in order for one under which a zombie's spawn
// check at the position passes, so a variant's own rule is what decides.
func diceFor(t *testing.T, w SpawnWorld, x, y, z int32) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		if validSpawnPositionForType(w, rand.New(rand.NewSource(seed)), TypeZombie, x, y, z, 100, CategoryMonster) {
			return seed
		}
	}
	t.Fatal("no dice stand a zombie up here")
	return 0
}

// TestAHuskSpawnsOnlyUnderTheOpenSky checks Monster.
// checkSurfaceMonstersSpawnRules: the dice that stand a zombie up stand a
// husk up only where the sky light is full.
func TestAHuskSpawnsOnlyUnderTheOpenSky(t *testing.T) {
	w := newSpawnTestWorld()
	w.darken = 11
	w.groundAt(t, 0, 63, 0)
	for _, sky := range []uint8{15, 14} {
		w.sky = sky
		seed := diceFor(t, w, 0, 64, 0)
		got := validSpawnPositionForType(w, rand.New(rand.NewSource(seed)), TypeHusk, 0, 64, 0, 100, CategoryMonster)
		if want := sky == 15; got != want {
			t.Errorf("sky light %d: husk spawn = %v, want %v", sky, got, want)
		}
	}
}

// TestAStraySeesTheSkyThroughPowderSnow checks Stray.checkStraySpawnRules'
// sky test, read from the top of the powder snow stacked over the spawn.
func TestAStraySeesTheSkyThroughPowderSnow(t *testing.T) {
	w := newSpawnTestWorld()
	w.set(0, 65, 0, defaultState(t, "minecraft:powder_snow"))
	w.set(0, 66, 0, defaultState(t, "minecraft:powder_snow"))
	w.sky = 15
	w.shaded = map[[3]int32]uint8{{0, 64, 0}: 0, {0, 65, 0}: 0}
	if !seesSkyForSpawn(w, TypeStray, 0, 64, 0) {
		t.Error("a stray under powder snow open to the sky does not see it")
	}
	if seesSkyForSpawn(w, TypeHusk, 0, 64, 0) {
		t.Error("a husk under powder snow sees the sky")
	}
	w.shaded[[3]int32{0, 66, 0}] = 14
	if seesSkyForSpawn(w, TypeStray, 0, 64, 0) {
		t.Error("a stray under powder snow short of the full sky sees it")
	}
}

// TestABoggedShootsLessOften checks Bogged's attack intervals beside the
// skeleton's: 70 and 50 on hard, against 40 and 20.
func TestABoggedShootsLessOften(t *testing.T) {
	for _, c := range []struct {
		kind         MobType
		normal, hard int
	}{{TypeSkeleton, 40, 20}, {TypeStray, 40, 20}, {TypeBogged, 70, 50}} {
		m := NewMob(c.kind, 1, 0, 64, 0, 1)
		if got := m.rangedIntervalFor(rules.DifficultyNormal); got != c.normal {
			t.Errorf("%s normal interval = %d, want %d", c.kind, got, c.normal)
		}
		if got := m.rangedIntervalFor(rules.DifficultyHard); got != c.hard {
			t.Errorf("%s hard interval = %d, want %d", c.kind, got, c.hard)
		}
	}
}

// TestACaveSpidersPoisonGrowsWithTheDifficulty checks CaveSpider.
// doHurtTarget's poison by difficulty and its 0.7 by 0.5 box.
func TestACaveSpidersPoisonGrowsWithTheDifficulty(t *testing.T) {
	m := NewMob(TypeCaveSpider, 1, 0, 64, 0, 1)
	for difficulty, want := range []int32{0, 0, 140, 300} {
		_, ticks, ok := m.BlowEffect(uint8(difficulty), 0)
		if ok != (want > 0) || ticks != want {
			t.Errorf("difficulty %d: poison %d ticks (%v), want %d", difficulty, ticks, ok, want)
		}
	}
	if w, h := KindDims(TypeCaveSpider); w != 0.7 || h != 0.5 {
		t.Errorf("cave spider box %v by %v, want 0.7 by 0.5", w, h)
	}
}

// TestADrownedsEquipmentOdds checks Drowned.populateDefaultEquipmentSlots
// and finalizeSpawn over many spawns: a tenth hold something, ten sixteenths
// of those a trident, and three in a hundred a nautilus shell they always
// drop.
func TestADrownedsEquipmentOdds(t *testing.T) {
	const n = 20000
	var armed, tridents, shells int
	for seed := int64(1); seed <= n; seed++ {
		m := NewMob(TypeDrowned, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:river"), nil, rules.LocalDifficulty{})
		if !m.MainHand.Empty() {
			armed++
			if m.MainHand.ItemID == int32(registry.ItemTrident) {
				tridents++
			} else if m.MainHand.ItemID != int32(registry.ItemFishingRod) {
				t.Fatalf("a drowned holds item %d", m.MainHand.ItemID)
			}
		}
		if !m.OffHand.Empty() {
			shells++
			if m.OffHand.ItemID != int32(registry.ItemNautilusShell) || m.DropChances[1] <= 1 {
				t.Fatalf("a drowned's off hand is %+v (guaranteed %v), want a guaranteed nautilus shell", m.OffHand, m.DropChances[1] > 1)
			}
		}
	}
	near := func(got int, want float64) bool { return math.Abs(float64(got)/n-want) < want*0.15 }
	if !near(armed, 0.1) || !near(tridents, 0.1*10/16) || !near(shells, 0.03) {
		t.Errorf("of %d drowned %d armed, %d with tridents, %d with shells; want about 10%%, 6.25%%, 3%%", n, armed, tridents, shells)
	}
}

// TestADrownedSpawnsInDeepWaterOrRivers checks the IN_WATER placement and
// Drowned.checkDrownedSpawnRules: never on dry ground nor in shallow sea,
// and by the dice in a river or deeper than five under the sea.
func TestADrownedSpawnsInDeepWaterOrRivers(t *testing.T) {
	w := newSpawnTestWorld()
	w.darken = 11
	water := defaultState(t, "minecraft:water")
	for y := int32(40); y <= 62; y++ {
		w.set(0, y, 0, water)
	}
	stands := func(biome string, y int32) int {
		w.biomeName = biome
		n := 0
		for seed := int64(1); seed <= 2000; seed++ {
			if validSpawnPositionForType(w, rand.New(rand.NewSource(seed)), TypeDrowned, 0, y, 0, 100, CategoryMonster) {
				n++
			}
		}
		return n
	}
	if n := stands("minecraft:ocean", 50); n == 0 {
		t.Error("no dice stand a drowned up thirteen under the sea")
	}
	if n := stands("minecraft:ocean", 60); n != 0 {
		t.Errorf("%d dice stand a drowned up three under the sea, want none", n)
	}
	if n := stands("minecraft:river", 60); n == 0 {
		t.Error("no dice stand a drowned up in a river")
	}
	if n := stands("minecraft:river", 63); n != 0 {
		t.Errorf("%d dice stand a drowned up out of the water, want none", n)
	}
}

// TestAZombieVillagersDataComesFromItsBiome checks ZombieVillager's
// VillagerData: a profession drawn from the whole registry at level one,
// the type left at plains until finalizeSpawn reads it off the biome
// (VillagerType.byBiome), and a type read from a tag never re-read.
func TestAZombieVillagersDataComesFromItsBiome(t *testing.T) {
	professions := map[string]bool{}
	for seed := int64(1); seed <= 2000; seed++ {
		m := NewMob(TypeZombieVillager, 1, 0, 64, 0, seed)
		if m.Villager.Type != "minecraft:plains" || m.Villager.Level != 1 || m.Villager.Finalized {
			t.Fatalf("a fresh zombie villager's data is %+v, want plains, level 1, not finalized", m.Villager)
		}
		professions[m.Villager.Profession] = true
	}
	if len(professions) != 15 {
		t.Errorf("%d professions drawn, want all 15 of the registry", len(professions))
	}
	for biome, want := range map[string]string{
		"minecraft:desert": "minecraft:desert", "minecraft:swamp": "minecraft:swamp",
		"minecraft:snowy_plains": "minecraft:snow", "minecraft:forest": "minecraft:plains",
	} {
		m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome(biome), nil, rules.LocalDifficulty{})
		if m.Villager.Type != want || !m.Villager.Finalized {
			t.Errorf("finalized in %s: %+v, want type %s, finalized", biome, m.Villager, want)
		}
	}
	m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
	m.Villager = VillagerData{Type: "minecraft:taiga", Profession: "minecraft:mason", Level: 3, Finalized: true}
	m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:desert"), nil, rules.LocalDifficulty{})
	if m.Villager.Type != "minecraft:taiga" {
		t.Errorf("a finalized type became %s, want taiga kept", m.Villager.Type)
	}
}

// TestACubeScalesWithItsSize checks AbstractCubeMob.setSize and the
// builder's 0.52 box: the box and eyes scaled by the size, the max health
// its square, the pace 0.2 + 0.1 a size, the magma cube's three points of
// armour a size, and the experience of the size.
func TestACubeScalesWithItsSize(t *testing.T) {
	m := NewMob(TypeSlime, 1, 0, 64, 0, 1)
	m.SetSize(4, true)
	if m.BbWidth() != 0.52*4 || m.BbHeight() != 0.52*4 || m.eyeHeight() != 0.325*4 {
		t.Errorf("size 4 slime box %v by %v, eyes %v; want 2.08, 2.08, 1.3", m.BbWidth(), m.BbHeight(), m.eyeHeight())
	}
	if m.Health != 16 || m.MaxHealth != 16 {
		t.Errorf("size 4 slime health %v of %v, want 16", m.Health, m.MaxHealth)
	}
	if got, want := m.pace(), float64(float32(0.2)+float32(0.1)*4); got != want {
		t.Errorf("size 4 slime pace %v, want %v", got, want)
	}
	if got := m.RollExperience(rules.LootKiller{}); got != 4 {
		t.Errorf("size 4 slime experience %d, want 4", got)
	}
	magma := NewMob(TypeMagmaCube, 1, 0, 64, 0, 1)
	magma.SetSize(2, true)
	if got, want := magma.armoured(10), rules.DamageAfterArmour(10, 6, 0); got != want {
		t.Errorf("size 2 magma cube takes %v of 10, want %v behind six points of armour", got, want)
	}
	if magma.cubeAttackDamage() != 4 || !magma.cubeDealsDamage() {
		t.Errorf("size 2 magma cube deals %v (%v), want 4", magma.cubeAttackDamage(), magma.cubeDealsDamage())
	}
	tiny := NewMob(TypeSlime, 1, 0, 64, 0, 1)
	if tiny.cubeDealsDamage() {
		t.Error("the smallest slime deals damage")
	}
}

// TestABigCubeSplits checks AbstractCubeMob.remove: two to four cubes of
// half the size at the corners of a square half the width across, half a
// block up; the smallest leaves nothing.
func TestABigCubeSplits(t *testing.T) {
	counts := map[int]bool{}
	for seed := int64(1); seed <= 200; seed++ {
		m := NewMob(TypeSlime, 1, 10, 64, 10, seed)
		m.SetSize(4, true)
		splits := m.Splits()
		counts[len(splits)] = true
		for _, s := range splits {
			if s.Size != 2 || s.Y != 64.5 || math.Abs(math.Abs(s.X-10)-0.52) > 1e-6 || math.Abs(math.Abs(s.Z-10)-0.52) > 1e-6 {
				t.Fatalf("split %+v, want size 2 at a corner 0.52 off (10, 64.5, 10)", s)
			}
		}
	}
	if len(counts) != 3 || !counts[2] || !counts[3] || !counts[4] {
		t.Errorf("split counts %v, want 2, 3 and 4", counts)
	}
	if s := NewMob(TypeSlime, 1, 0, 64, 0, 1).Splits(); s != nil {
		t.Errorf("the smallest slime split into %v", s)
	}
}

// TestCubeSpawnSizes checks AbstractCubeMob.setSpawnSize: one, two or four
// in thirds with no special multiplier, and the larger more often with one.
func TestCubeSpawnSizes(t *testing.T) {
	for _, special := range []float32{0, 1} {
		sizes := map[int]int{}
		for seed := int64(1); seed <= 3000; seed++ {
			m := NewMob(TypeSlime, 1, 0, 64, 0, seed)
			m.SetSpawnSize(special)
			sizes[m.Size]++
		}
		if len(sizes) != 3 || sizes[1] == 0 || sizes[2] == 0 || sizes[4] == 0 {
			t.Fatalf("special %v: sizes %v, want 1, 2 and 4", special, sizes)
		}
		if special == 0 && (sizes[1] < 900 || sizes[4] > 1100) {
			t.Errorf("special 0: sizes %v, want about a thousand each", sizes)
		}
		if special == 1 && sizes[1] > 600 {
			t.Errorf("special 1: sizes %v, want the smallest half as often", sizes)
		}
	}
}

// TestAZombieUnderWaterConverts checks Zombie.tick's timing: the
// conversion starts on the 600th tick with the eyes in water and ends 300
// ticks later; eyes out of the water reset the count, and a drowned or a
// zombie villager never converts.
func TestAZombieUnderWaterConverts(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	m.waterFluid.eyesIn = true
	for i := 1; i < 600; i++ {
		if into := m.waterConversionStep(); into != NoMobType || m.DrownedConverting {
			t.Fatalf("tick %d: converting %v into %q", i, m.DrownedConverting, into)
		}
	}
	m.waterConversionStep()
	if !m.DrownedConverting || m.DrownedConversionTime != 300 {
		t.Fatalf("after 600 ticks converting %v with %d left, want 300", m.DrownedConverting, m.DrownedConversionTime)
	}
	for i := range 300 {
		if into := m.waterConversionStep(); into != NoMobType {
			t.Fatalf("converted early, %d ticks in", i)
		}
	}
	if into := m.waterConversionStep(); into != TypeDrowned {
		t.Errorf("converted into %q, want a drowned", into)
	}
	m = NewMob(TypeZombie, 1, 0, 64, 0, 1)
	m.waterFluid.eyesIn = true
	for range 599 {
		m.waterConversionStep()
	}
	m.waterFluid.eyesIn = false
	m.waterConversionStep()
	if m.InWaterTime != -1 {
		t.Errorf("eyes out of the water left the count at %d, want -1", m.InWaterTime)
	}
	for _, kind := range []MobType{TypeDrowned, TypeZombieVillager} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		m.waterFluid.eyesIn = true
		for range 1000 {
			if m.waterConversionStep() != NoMobType || m.DrownedConverting {
				t.Fatalf("a %s converts under water", kind)
			}
		}
	}
}

// TestAHuskHoldingSomethingGivesNoHunger checks Husk.doHurtTarget's empty
// main hand.
func TestAHuskHoldingSomethingGivesNoHunger(t *testing.T) {
	m := NewMob(TypeHusk, 1, 0, 64, 0, 1)
	if _, ticks, ok := m.BlowEffect(rules.DifficultyHard, 2.25); !ok || ticks != 280 {
		t.Errorf("an empty-handed husk gives %d ticks (%v), want 280", ticks, ok)
	}
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemBow), Count: 1}
	if _, _, ok := m.BlowEffect(rules.DifficultyHard, 2.25); ok {
		t.Error("a husk holding a bow gave hunger")
	}
}

// TestACubeHopsEveryDelayAndOne checks CubeMobMoveControl's
// if (jumpDelay-- <= 0): a countdown of three hops on the fourth wanted
// tick on the ground.
func TestACubeHopsEveryDelayAndOne(t *testing.T) {
	m := NewMob(TypeSlime, 1, 0, 64, 0, 1)
	m.OnGround = true
	m.cube.jumpDelay = 3
	for i := 1; i <= 4; i++ {
		m.cube.wanted, m.cube.speedMod = true, 1
		if hopped := m.cubeMoveStep(); hopped != (i == 4) {
			t.Errorf("wanted tick %d: hopped %v", i, hopped)
		}
	}
}

// TestACubeIdlesAtTheMobsPace checks that the cubes and the other enemies
// outside Monster (ghast, shulker, hoglin) take no Monster idleness share in
// bright light while a zombie does.
func TestACubeIdlesAtTheMobsPace(t *testing.T) {
	w := newTestWorld()
	w.block = 15
	for kind, want := range map[MobType]int{
		TypeSlime: 1, TypeMagmaCube: 1, TypeGhast: 1, TypeShulker: 1, TypeHoglin: 1, TypeZombie: 3,
	} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		m.idleStep(w)
		if m.NoActionTicks != want {
			t.Errorf("%s idled %d in bright light, want %d", kind, m.NoActionTicks, want)
		}
	}
}

// TestAMagmaCubesFireGoesOut checks Entity.baseTick's clearFire for a
// fire-immune body.
func TestAMagmaCubesFireGoesOut(t *testing.T) {
	m := NewMob(TypeMagmaCube, 1, 0, 64, 0, 1)
	m.FireTicks = 100
	m.fireStep(newTestWorld(), nil)
	if m.FireTicks != 0 || m.Health != m.MaxHealth || m.OnFire() {
		t.Errorf("fire %d, health %v, on fire %v; want the fire cleared and nothing hurt", m.FireTicks, m.Health, m.OnFire())
	}
}

// TestADrownedSpeaksWithTheWater checks Drowned's water sounds.
func TestADrownedSpeaksWithTheWater(t *testing.T) {
	m := NewMob(TypeDrowned, 1, 0, 64, 0, 1)
	if s := m.Sounds(); s.Ambient.Name() != "minecraft:entity.drowned.ambient" || s.Hurt.Name() != "minecraft:entity.drowned.hurt" {
		t.Errorf("a dry drowned speaks %+v", s)
	}
	m.waterFluid.height = 1
	if s := m.Sounds(); s.Ambient.Name() != "minecraft:entity.drowned.ambient_water" ||
		s.Hurt.Name() != "minecraft:entity.drowned.hurt_water" || s.Death.Name() != "minecraft:entity.drowned.death_water" {
		t.Errorf("a drowned in water speaks %+v", s)
	}
}

// TestASwimmingDrownedFallsNot checks Drowned.travelInWater's swim: no
// gravity and nine tenths of drag, the swim pushing it forward.
func TestASwimmingDrownedFallsNot(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for y := int32(60); y <= 70; y++ {
		w.set(0, y, 0, water)
	}
	m := NewMob(TypeDrowned, 1, 0.5, 64, 0.5, 1)
	m.Swimming, m.swimSpeed = true, 0.23
	m.waterFluid.height = 1
	m.VY = 0.1
	m.drownedSwimTravel(w)
	if math.Abs(m.VY-0.09) > 1e-9 || m.VZ <= 0 {
		t.Errorf("a swimming drowned's motion after a tick is %v, %v; want 0.09 up and forward", m.VY, m.VZ)
	}
}

// TestASkeletonsBowComesAtFinalize checks AbstractSkeleton: no bow from the
// constructor, one from finalizeSpawn.
func TestASkeletonsBowComesAtFinalize(t *testing.T) {
	for _, kind := range []MobType{TypeSkeleton, TypeStray, TypeBogged} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		if !m.MainHand.Empty() {
			t.Errorf("a fresh %s holds %+v, want nothing", kind, m.MainHand)
		}
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if m.MainHand.ItemID != int32(registry.ItemBow) || m.MainHand.Count != 1 {
			t.Errorf("a finalized %s holds %+v, want a bow", kind, m.MainHand)
		}
	}
}

// TestInheritIntoCarriesTheParentsVariant holds what an egg's offspring takes
// from the parent bred with itself: a cow's, pig's or chicken's variant, a
// sheep's colour, and a mooshroom's variant.
func TestInheritIntoCarriesTheParentsVariant(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypePig, TypeChicken} {
		parent := NewMob(kind, 1, 0, 64, 0, 1)
		parent.Variant = "minecraft:warm"
		baby := NewMob(kind, 2, 0, 64, 0, 1)
		baby.Variant = "minecraft:cold"
		parent.InheritInto(baby)
		if baby.Variant != "minecraft:warm" {
			t.Errorf("a bred %s's variant = %q, want the parent's minecraft:warm", kind, baby.Variant)
		}
	}
	sheep := NewMob(TypeSheep, 1, 0, 64, 0, 1)
	sheep.Colour = "black"
	lamb := NewMob(TypeSheep, 2, 0, 64, 0, 1)
	sheep.InheritInto(lamb)
	if lamb.Colour != "black" {
		t.Errorf("a bred sheep's colour = %q, want the parent's black", lamb.Colour)
	}
	mooshroom := NewMob(TypeMooshroom, 1, 0, 64, 0, 1)
	mooshroom.Mushroom = "brown"
	calf := NewMob(TypeMooshroom, 2, 0, 64, 0, 1)
	mooshroom.InheritInto(calf)
	if calf.Mushroom != "brown" {
		t.Errorf("a bred mooshroom's variant = %q, want the parent's brown", calf.Mushroom)
	}
}

// TestMooshroomBreedingFlipsASameVariantPair holds MushroomCow's
// getOffspringVariant: a same-variant pair turns the other variant out of a
// one-in-1024 roll, and a mixed pair takes either parent's by coin.
func TestMooshroomBreedingFlipsASameVariantPair(t *testing.T) {
	brown := func() *Mob {
		m := NewMob(TypeMooshroom, 1, 0, 64, 0, 1)
		m.Mushroom = "brown"
		return m
	}
	// A seed whose first draw lands under one in 1024 turns the brown pair
	// red; the old coin alone never could.
	flip := int64(0)
	for rand.New(rand.NewSource(flip)).Intn(1024) != 0 {
		flip++
	}
	mother, mate := brown(), brown()
	mother.Rand = rand.New(rand.NewSource(flip))
	calf := NewMob(TypeMooshroom, 2, 0, 64, 0, 1)
	mother.BreedInto(calf, mate)
	if calf.Mushroom != "red" {
		t.Errorf("a brown pair's calf at the flip = %q, want the other variant red", calf.Mushroom)
	}

	// A mixed pair gives each variant by coin, never a third.
	mate.Mushroom = "red"
	seen := map[string]bool{}
	for seed := range int64(40) {
		mother.Rand = rand.New(rand.NewSource(seed))
		calf = NewMob(TypeMooshroom, 2, 0, 64, 0, 1)
		mother.BreedInto(calf, mate)
		if calf.Mushroom != "brown" && calf.Mushroom != "red" {
			t.Fatalf("a mixed pair's calf = %q, want one of the parents", calf.Mushroom)
		}
		seen[calf.Mushroom] = true
	}
	if !seen["brown"] || !seen["red"] {
		t.Errorf("a mixed pair gave variants %v, want both parents' by coin", seen)
	}
}
