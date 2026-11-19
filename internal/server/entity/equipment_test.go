package entity

import (
	"maps"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The spawn's equipment rolls over their seeds: the armour every zombie
// and every skeleton family inherits from Mob.populateDefaultEquipmentSlots
// — the difficulty's odds, the material the lifts raise, the head-down
// dress — the zombie's iron tool beside it, the drowned's own override
// that carries no armour at all, the share a worn piece takes off a blow
// and the wear the sun puts on a head's helmet instead of the burn. And
// the loot pickup the same finalizes set the monsters to, the take a
// CanPickUpLoot body makes of what it finds — what counts as the better
// of two pieces, where a found stack goes and how much of it leaves the
// ground.

// hard is the local difficulty a spawn reads on a hard world at an
// effective difficulty of four and up: the special multiplier whole, the
// level's own difficulty hard.
func hard() rules.LocalDifficulty { return rules.LocalDifficulty{Special: 1, Hard: true} }

// helmetIDs is the spawn's six helmets by their registry IDs.
func helmetIDs(t *testing.T) map[int32]bool {
	t.Helper()
	helmets := map[int32]bool{}
	for _, name := range []string{
		"minecraft:leather_helmet", "minecraft:copper_helmet", "minecraft:golden_helmet",
		"minecraft:chainmail_helmet", "minecraft:iron_helmet", "minecraft:diamond_helmet",
	} {
		id, ok := registry.ItemID(name)
		if !ok {
			t.Fatalf("the item registry has no %s", name)
		}
		helmets[id] = true
	}
	return helmets
}

// stackOf is one item as a stack of a piece of spawn equipment.
func stackOf(t testing.TB, name string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1}
}

// TestTheHardRollDressesSpawnedZombies checks the equipment roll over many
// seeds: on a hard world at the full special multiplier three zombies in
// twenty stand in armour — always helmeted, never wearing a piece below a
// bare one above it, of the table's six materials — and one in twenty
// holds an iron sword, an iron spear or, on four faces of the die, an
// iron shovel.
func TestTheHardRollDressesSpawnedZombies(t *testing.T) {
	const n = 20000
	helmets := helmetIDs(t)
	var armoured, tooled, swords, spears, shovels int
	for seed := int64(1); seed <= n; seed++ {
		m := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
		dressed := false
		for _, slot := range populationOrder {
			if m.Worn[slot].Empty() {
				continue
			}
			if !dressed {
				dressed = true
				armoured++
				if !helmets[m.Worn[wornHead].ItemID] {
					name, _ := registry.ItemName(m.Worn[wornHead].ItemID)
					t.Fatalf("seed %d: a spawned zombie wears %s on its head, want a helmet", seed, name)
				}
				continue
			}
			// Each piece below the head needs the ones above it worn:
			// the dress walks head down and gives nothing out of order.
			for _, above := range populationOrder {
				if above <= slot {
					continue
				}
				if m.Worn[above].Empty() {
					t.Fatalf("seed %d: a spawned zombie wears a piece with slot %d above it bare", seed, above)
				}
			}
		}
		switch {
		case m.MainHand.Empty():
		case m.MainHand.ItemID == int32(registry.ItemIronSword):
			tooled, swords = tooled+1, swords+1
		case m.MainHand.ItemID == int32(registry.ItemIronShovel):
			tooled, shovels = tooled+1, shovels+1
		case m.MainHand.ItemID == int32(registry.ItemIronSpear):
			tooled, spears = tooled+1, spears+1
		default:
			t.Fatalf("seed %d: a spawned zombie holds item %d", seed, m.MainHand.ItemID)
		}
	}
	near := func(got int, want float64) bool {
		return math.Abs(float64(got)/n-want) < want*0.15
	}
	if !near(armoured, 0.15) || !near(tooled, 0.05) {
		t.Errorf("of %d zombies %d in armour and %d with tools; want about 15%% and the hard 5%%",
			n, armoured, tooled)
	}
	if shovels < swords || shovels < spears {
		t.Errorf("of %d tools %d swords, %d spears, %d shovels; want the shovel's four faces to hold up the other two's one each",
			tooled, swords, spears, shovels)
	}
}

// TestTheEasierRollDressesFewerZombies checks the difficulty's hand on the
// same seeds: where the special multiplier is spent, no zombie stands in
// armour at all, and on a world that is not hard the tool's odds fall to
// vanilla's one in a hundred.
func TestTheEasierRollDressesFewerZombies(t *testing.T) {
	const n = 20000
	var tooled, armoured int
	for seed := int64(1); seed <= n; seed++ {
		m := NewMob(TypeZombie, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		for _, slot := range populationOrder {
			if !m.Worn[slot].Empty() {
				armoured++
			}
		}
		if !m.MainHand.Empty() {
			tooled++
		}
	}
	if armoured > 0 {
		t.Errorf("%d of %d easy zombies wore armour, want none", armoured, n)
	}
	near := func(got int, want float64) bool {
		return math.Abs(float64(got)/n-want) < want*0.3
	}
	if !near(tooled, 0.01) {
		t.Errorf("%d of %d non-hard zombies held a tool, want about the one in a hundred", tooled, n)
	}
}

// TestSkeletonsStraysAndBoggedRollArmourBesideTheBow checks the skeleton
// family's roll: the armour of the base's own beside the bow its spawn
// always hands it — and the drowned's override, which carries no armour
// whatever the difficulty rolls.
func TestSkeletonsStraysAndBoggedRollArmourBesideTheBow(t *testing.T) {
	const n = 20000
	helmets := helmetIDs(t)
	bow, _ := registry.ItemID("minecraft:bow")
	for _, kind := range []MobType{TypeSkeleton, TypeStray, TypeBogged} {
		var armoured int
		for seed := int64(1); seed <= n; seed++ {
			m := NewMob(kind, 1, 0, 64, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
			if !m.MainHand.Empty() && m.MainHand.ItemID != bow {
				t.Fatalf("a spawned %s holds item %d, want the bow alone in its hands", kind, m.MainHand.ItemID)
			}
			if m.Worn[wornHead].Empty() {
				continue
			}
			armoured++
			if !helmets[m.Worn[wornHead].ItemID] {
				t.Fatalf("a spawned %s wears item %d, want one of the six helmets", kind, m.Worn[wornHead].ItemID)
			}
		}
		if got := float64(armoured) / n; math.Abs(got-0.15) > 0.15*0.15 {
			t.Errorf("of %d %s spawns %d wore armour, want about 15%%", n, kind, armoured)
		}
	}
	for seed := int64(1); seed <= n; seed++ {
		m := NewMob(TypeDrowned, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
		for _, slot := range populationOrder {
			if !m.Worn[slot].Empty() {
				t.Fatalf("a drowned spawned wearing a piece at slot %d, want its hands alone", slot)
			}
		}
	}
}

// TestHusksAndZombieVillagersRollTheZombiesEquipment checks the family
// share of the roll: the husk and the zombie villager stand in armour at
// the zombie's own odds.
func TestHusksAndZombieVillagersRollTheZombiesEquipment(t *testing.T) {
	const n = 4000
	for _, kind := range []MobType{TypeHusk, TypeZombieVillager} {
		var armoured int
		for seed := int64(1); seed <= n; seed++ {
			m := NewMob(kind, 1, 0, 64, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
			if !m.Worn[wornHead].Empty() {
				armoured++
			}
		}
		if got := float64(armoured) / n; math.Abs(got-0.15) > 0.15*0.2 {
			t.Errorf("of %d %s spawns %d wore armour, want about 15%%", n, kind, armoured)
		}
	}
}

// TestWornArmourTakesItsShareOfABlow checks the worn pieces' points behind
// the natural ones: the iron chestplate's six on the zombie's natural two,
// and the diamond helmet's three with its two of toughness.
func TestWornArmourTakesItsShareOfABlow(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	m.Worn[wornChest] = stackOf(t, "minecraft:iron_chestplate")
	if got, want := m.armoured(9), rules.DamageAfterArmour(9, 8, 0); got != want {
		t.Errorf("with an iron chestplate a blow of nine leaves %v, want %v (the zombie's two beside the chestplate's six)", got, want)
	}
	m.Worn[wornChest] = protocol.ItemStack{}
	m.Worn[wornHead] = stackOf(t, "minecraft:diamond_helmet")
	if got, want := m.armoured(9), rules.DamageAfterArmour(9, 5, 2); got != want {
		t.Errorf("with a diamond helmet a blow of nine leaves %v, want %v (the three points and the two toughness)", got, want)
	}
}

// TestWornArmourDropsAndPaysOnlyForItsKiller checks the drop side of a
// body in armour: the pieces drop at the eighth-and-a-half's share and
// only for a player's kill, badly worn — and the experience counts each
// piece among its own.
func TestWornArmourDropsAndPaysOnlyForItsKiller(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0, 64, 0, 7)
	m.Worn[wornFeet] = stackOf(t, "minecraft:iron_boots")
	m.Worn[wornLegs] = stackOf(t, "minecraft:iron_leggings")
	m.Worn[wornChest] = stackOf(t, "minecraft:iron_chestplate")
	m.Worn[wornHead] = stackOf(t, "minecraft:iron_helmet")

	// The experience: the monster's five of the base plus one to three a
	// piece, four pieces worn.
	if got := m.RollExperience(rules.LootKiller{}); got < 9 || got > 17 {
		t.Errorf("a zombie in four pieces of armour left %d experience, want the base five with one to three a piece (9 to 17)", got)
	}

	// Without a player's kill behind the death, no piece drops.
	for range 100 {
		for _, stack := range m.RollDrops(rules.LootKiller{}, nil) {
			name, _ := registry.ItemName(stack.ItemID)
			if name != "minecraft:rotten_flesh" {
				t.Fatalf("a zombie that died to nobody dropped %s, want the table's flesh alone", name)
			}
		}
	}

	// With one, the pieces drop within their badly worn share.
	m.playerMemoryTicks = 100
	var drops int
	for range 2000 {
		for _, stack := range m.RollDrops(rules.LootKiller{}, nil) {
			if stack.Damage > 0 {
				drops++
			}
		}
	}
	if drops == 0 {
		t.Error("no worn piece ever dropped across two thousand player kills, want the eighth-and-a-half's share of some")
	}
}

// TestTheConversionCarriesItsArmour checks the drowned the water made of a
// zombie: armour and its drop chances survive, empty slots keep defaults.
// vanilla: ConversionType.SINGLE.convert
func TestTheConversionCarriesItsArmour(t *testing.T) {
	old := NewMob(TypeZombie, 1, 0, 64, 0, 3)
	old.Worn[wornHead] = stackOf(t, "minecraft:iron_helmet")
	old.Worn[wornChest] = stackOf(t, "minecraft:iron_chestplate")
	old.DropChances = [6]float32{2, 0, 0.5, 1, 0.25, 2}
	fresh := NewMob(TypeDrowned, 2, 0, 64, 0, 3)
	fresh.ConvertedFrom(old)
	if fresh.Worn[wornHead].ItemID != old.Worn[wornHead].ItemID ||
		fresh.Worn[wornChest].ItemID != old.Worn[wornChest].ItemID {
		t.Error("the converted drowned lost its armour, want the pieces the zombie stood in carried over")
	}
	want := [6]float32{0.085, 0.085, 0.085, 0.085, 0.25, 2}
	if fresh.DropChances != want {
		t.Errorf("converted drop chances = %v, want %v (only occupied chest and head slots carry over)", fresh.DropChances, want)
	}
}

// TestTheSunWearsTheHelmetOffBeforeItBurns checks the head the burn reads:
// a zombie in a leather helmet never lights while the helmet stands, the
// helmet wearing a point or two a burn tick until it breaks — and the fire
// comes on only from the roll after the break.
func TestTheSunWearsTheHelmetOffBeforeItBurns(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 0
	floor64(w, t)

	z := hunter()
	helmet := stackOf(t, "minecraft:leather_helmet")
	helmet.Damage = registry.MaxDamage(helmet.ItemID) - 2
	z.Worn[wornHead] = helmet

	// While the helmet stands, no fire lights and the wear grows; past
	// its break the fire comes on.
	worn, lit := false, false
	for range 400 {
		z.Tick(w, nil)
		if !z.Worn[wornHead].Empty() {
			if z.FireTicks > 0 {
				t.Fatal("a zombie wearing a helmet caught fire in the sun")
			}
			if z.Worn[wornHead].Damage > helmet.Damage {
				worn = true
			}
			continue
		}
		if !worn {
			t.Fatal("the helmet vanished without taking any wear first")
		}
		if z.FireTicks > 0 {
			lit = true
			break
		}
	}
	if !worn || !lit {
		t.Error("the helmeted zombie never burned after its helmet wore through, want the fire on from the roll after the break")
	}
}

// TestAnUnbreakableHelmetTakesNoSunWear checks the damageable gate the burn
// reads: the unbreakable component spares the head the wear of every roll,
// so the piece stands whole and the fire never lights.
func TestAnUnbreakableHelmetTakesNoSunWear(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 0
	floor64(w, t)

	z := hunter()
	helmet := stackOf(t, "minecraft:leather_helmet")
	helmet.Unbreakable = true
	z.Worn[wornHead] = helmet

	for range 2000 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			t.Fatal("a zombie wearing an unbreakable helmet caught fire in the sun")
		}
		if z.Worn[wornHead].Empty() {
			t.Fatal("an unbreakable helmet vanished from the head")
		}
		if got := z.Worn[wornHead].Damage; got != 0 {
			t.Fatalf("the sun put %d wear on an unbreakable helmet, want none", got)
		}
	}
}

// TestABrokenHelmetTellsTheBreak checks the event the burn's break emits:
// the tick the head slot's piece wears through clears the slot and reports
// the break, which the server broadcasts as the head's equipment break.
func TestABrokenHelmetTellsTheBreak(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 0
	floor64(w, t)

	z := hunter()
	helmet := stackOf(t, "minecraft:leather_helmet")
	helmet.Damage = registry.MaxDamage(helmet.ItemID) - 1
	z.Worn[wornHead] = helmet

	broke := false
	for range 2000 {
		for _, ev := range z.Tick(w, nil) {
			if ev.Kind == MobArmourBroke {
				broke = true
			}
		}
		if z.Worn[wornHead].Empty() {
			break
		}
	}
	if !broke {
		t.Error("a helmet worn through by the sun never told the break")
	}
	if !z.Worn[wornHead].Empty() {
		t.Error("the helmet stayed on after its break, want the head cleared")
	}
}

// TestTheNonHardRollStopsAtTheQuarter checks the stop the non-hard body's
// dress reads: the special multiplier still rolls the armour off a hard
// world, and the walk down the body halts at vanilla's quarter chance after
// each piece, so a quarter of the dressed bodies stand in the head alone.
func TestTheNonHardRollStopsAtTheQuarter(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	dice := rand.New(rand.NewSource(0x5107))
	local := rules.LocalDifficulty{Special: 1}
	var armoured, headOnly int
	for range 400000 {
		clear(m.Worn[:])
		m.rollSpawnArmour(GoDice{dice}, local)
		if m.Worn[wornHead].Empty() {
			continue
		}
		armoured++
		if m.Worn[wornChest].Empty() && m.Worn[wornLegs].Empty() && m.Worn[wornFeet].Empty() {
			headOnly++
		}
	}
	if armoured == 0 {
		t.Fatal("no non-hard spawn wore armour, want the special multiplier's roll")
	}
	if got := float64(headOnly) / float64(armoured); math.Abs(got-0.25) > 0.01 {
		t.Errorf("of %d dressed non-hard spawns %d stand in the head alone (%.3f), want the quarter stop",
			armoured, headOnly, got)
	}
}

// TestTheSpawnArmourMaterialRisesByTheThreeLifts checks the material the
// hard roll dresses the head in against the table's own dice: the tier drawn
// from the bottom three, raised by up to three rolls at vanilla's 0.1087, so
// the six rows come out in the shape the base and the lifts make.
func TestTheSpawnArmourMaterialRisesByTheThreeLifts(t *testing.T) {
	tierOf := map[int32]int{}
	for tier, pieces := range spawnArmour {
		tierOf[int32(pieces[wornHead])] = tier
	}
	m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	dice := rand.New(rand.NewSource(0x5EED))
	var worn [6]int
	var armoured int
	for range 2000000 {
		clear(m.Worn[:])
		m.rollSpawnArmour(GoDice{dice}, hard())
		if m.Worn[wornHead].Empty() {
			continue
		}
		armoured++
		worn[tierOf[m.Worn[wornHead].ItemID]]++
	}
	if armoured == 0 {
		t.Fatal("no hard spawn wore armour, want the special multiplier's roll")
	}

	// The model: a uniform base over the bottom three, lifted by up to
	// three Bernoulli(0.1087) rolls.
	const p = 0.1087
	lifts := [4]float64{1, 3, 3, 1}
	var binom [4]float64
	for k := range binom {
		binom[k] = lifts[k] * math.Pow(p, float64(k)) * math.Pow(1-p, float64(3-k))
	}
	var want [6]float64
	for base := range 3 {
		for k := range binom {
			want[base+k] += binom[k] / 3
		}
	}
	for tier := range worn {
		if worn[tier] == 0 {
			t.Errorf("no spawn wore the tier %d material across two million rolls", tier)
		}
		got := float64(worn[tier]) / float64(armoured)
		// Three standard errors of a binomial share, floored so the rare
		// diamond row is not held to a share its count cannot meet.
		se := math.Sqrt(want[tier] * (1 - want[tier]) / float64(armoured))
		if math.Abs(got-want[tier]) > 3*se+0.0005 {
			t.Errorf("tier %d is %.4f of the dressed spawns, want %.4f (the base and the three lifts)",
				tier, got, want[tier])
		}
	}
	mean := 0.0
	for tier, count := range worn {
		mean += float64(tier) * float64(count)
	}
	mean /= float64(armoured)
	if math.Abs(mean-(1+3*p)) > 0.01 {
		t.Errorf("the average material tier is %.3f, want %.3f (the base one and the three lifts)", mean, 1+3*p)
	}
}

// The kinds a finalize gives the loot pickup to: the zombie family's and
// the skeleton family's own finalizes roll it, no other kind's does.
var lootRolling = []MobType{
	TypeZombie, TypeHusk, TypeDrowned, TypeZombieVillager,
	TypeSkeleton, TypeStray, TypeBogged,
}

// TestFinalizeSpawnRollsTheLootPickupForItsMonsters checks the roll over
// many seeds: at the whole special multiplier the kinds' finalizes give
// fifty-five in a hundred of them the pickup, at the multiplier of none —
// the peaceful share — nobody, and the kinds whose finalize rolls nothing
// carry the pickup never.
func TestFinalizeSpawnRollsTheLootPickupForItsMonsters(t *testing.T) {
	// 4000 seeds put the 3-sigma band (about 0.024) well inside the gap to a wrong share.
	const n = 4000
	for _, kind := range lootRolling {
		got := 0
		for seed := range int64(n) {
			m := NewMob(kind, 1, 0, 0, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
			if m.CanPickUpLoot {
				got++
			}
		}
		// Three standard errors of the binomial share, and a floor for a
		// kind of fewer seeds than the error covers.
		share := float64(got) / n
		if math.Abs(share-0.55) > 3*math.Sqrt(0.55*0.45/n)+0.005 {
			t.Errorf("%s's finalize gave %.4f of its mobs the loot pickup, want the 0.55 share", kind, share)
		}
	}
	for _, kind := range lootRolling {
		for seed := range int64(2000) {
			m := NewMob(kind, 1, 0, 0, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
			if m.CanPickUpLoot {
				t.Fatalf("%s's finalize gave the loot pickup at the special multiplier of none, seed %d", kind, seed)
			}
		}
	}
	for _, kind := range []MobType{
		TypeCow, TypePig, TypeSheep, TypeChicken,
		TypeSpider, TypeCaveSpider, TypeCreeper, TypeSlime, TypeMagmaCube,
	} {
		for seed := range int64(2000) {
			m := NewMob(kind, 1, 0, 0, 0, seed)
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
			if m.CanPickUpLoot {
				t.Fatalf("the %s finalize gave the loot pickup, want the kinds without an own roll", kind)
			}
		}
	}
}

// TestTheLootTakesWearAndHoldWhatTheyBeat runs the loot decisions over
// staged bodies and found stacks: a bare slot filled, an armour piece worn
// only over a worse one, a weapon taken only over a better none, a
// skeleton's bow outranking any sword, the equal of a worn piece ranked by
// its enchantments, its wear and its name, the armour the head refuses
// held in an empty hand, and the off hand filling only.
func TestTheLootTakesWearAndHoldWhatTheyBeat(t *testing.T) {
	enchanted := stackOf(t, "minecraft:iron_helmet")
	enchanted.Enchantments = map[int32]int32{1: 1}
	named := stackOf(t, "minecraft:iron_helmet")
	named.CustomName = &protocol.Text{Text: "Clanged"}
	worn := stackOf(t, "minecraft:iron_helmet")
	worn.Damage = 5

	cases := []struct {
		name  string
		kind  MobType
		holds [6]protocol.ItemStack
		found protocol.ItemStack
		want  int // the wire slot the found stack goes in, -1 for one left lying
	}{
		{
			name:  "a bare head is filled",
			kind:  TypeZombie,
			found: stackOf(t, "minecraft:iron_helmet"),
			want:  5,
		},
		{
			name:  "a better helmet is worn over the worse",
			kind:  TypeZombie,
			holds: handsAndHead(protocol.ItemStack{}, stackOf(t, "minecraft:golden_helmet")),
			found: stackOf(t, "minecraft:diamond_helmet"),
			want:  5,
		},
		{
			name:  "a worse helmet is left lying",
			kind:  TypeZombie,
			holds: handsAndHead(stackOf(t, "minecraft:iron_sword"), stackOf(t, "minecraft:diamond_helmet")),
			found: stackOf(t, "minecraft:leather_helmet"),
			want:  -1,
		},
		{
			name:  "a helmet the head refuses goes to an empty hand",
			kind:  TypeZombie,
			holds: handsAndHead(protocol.ItemStack{}, stackOf(t, "minecraft:diamond_helmet")),
			found: stackOf(t, "minecraft:iron_helmet"),
			want:  0,
		},
		{
			name:  "an enchanted helmet replaces the plain equal",
			kind:  TypeZombie,
			holds: handsAndHead(protocol.ItemStack{}, stackOf(t, "minecraft:iron_helmet")),
			found: enchanted,
			want:  5,
		},
		{
			name:  "a named helmet replaces the plain equal",
			kind:  TypeZombie,
			holds: handsAndHead(protocol.ItemStack{}, stackOf(t, "minecraft:iron_helmet")),
			found: named,
			want:  5,
		},
		{
			name:  "a less worn helmet replaces the equal",
			kind:  TypeZombie,
			holds: handsAndHead(protocol.ItemStack{}, worn),
			found: stackOf(t, "minecraft:iron_helmet"),
			want:  5,
		},
		{
			name:  "a better weapon replaces the held one",
			kind:  TypeZombie,
			holds: handsAndHead(stackOf(t, "minecraft:golden_sword"), protocol.ItemStack{}),
			found: stackOf(t, "minecraft:iron_sword"),
			want:  0,
		},
		{
			name:  "a worse weapon is left lying",
			kind:  TypeZombie,
			holds: handsAndHead(stackOf(t, "minecraft:iron_sword"), protocol.ItemStack{}),
			found: stackOf(t, "minecraft:wooden_sword"),
			want:  -1,
		},
		{
			name:  "a bare hand holds whatever it finds",
			kind:  TypeZombie,
			found: stackOf(t, "minecraft:dirt"),
			want:  0,
		},
		{
			name:  "a full hand holds the better of the two",
			kind:  TypeZombie,
			holds: handsAndHead(stackOf(t, "minecraft:iron_sword"), protocol.ItemStack{}),
			found: stackOf(t, "minecraft:dirt"),
			want:  -1,
		},
		{
			name:  "the skeleton's bow outranks a sword",
			kind:  TypeSkeleton,
			holds: handsAndHead(stackOf(t, "minecraft:bow"), protocol.ItemStack{}),
			found: stackOf(t, "minecraft:iron_sword"),
			want:  -1,
		},
		{
			name:  "a shield fills the off hand",
			kind:  TypeZombie,
			found: stackOf(t, "minecraft:shield"),
			want:  1,
		},
		{
			name:  "the off hand holds nothing over what it has",
			kind:  TypeZombie,
			holds: [6]protocol.ItemStack{protocol.ItemStack{}, stackOf(t, "minecraft:shield")},
			found: stackOf(t, "minecraft:shield"),
			want:  -1,
		},
	}
	for _, tc := range cases {
		m := NewMob(tc.kind, 1, 0, 0, 0, 1)
		m.CanPickUpLoot = true
		m.MainHand, m.OffHand = tc.holds[0], tc.holds[1]
		copy(m.Worn[:], tc.holds[2:])
		takes := Mobs{m}.TakeLoot([]LootItem{{Stack: tc.found}})
		if tc.want < 0 {
			if len(takes) > 0 {
				t.Errorf("%s: the mob took %+v, want the found stack left lying", tc.name, takes)
			}
			continue
		}
		if len(takes) != 1 {
			t.Fatalf("%s: the mob made %d takes, want one", tc.name, len(takes))
		}
		if got := wireSlot(takes[0].Slot); got != tc.want {
			t.Errorf("%s: the take goes to wire slot %d, want %d", tc.name, got, tc.want)
		}
		if takes[0].Count != 1 {
			t.Errorf("%s: the take claims %d of the stack, want the slot's one", tc.name, takes[0].Count)
		}
		m.TookLoot(takes[0], tc.found)
		if !m.Persistence {
			t.Errorf("%s: the take left the mob unpersistent, want the pickup's persisting", tc.name)
		}
	}

	// The reach: the scan's box grows one block past the body each way
	// sideways, none up or down.
	zombie := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	zombie.CanPickUpLoot = true
	helmet := stackOf(t, "minecraft:iron_helmet")
	for _, loot := range []LootItem{
		{X: 1.2, Y: 64, Z: 0, Stack: helmet},
		{X: -1.2, Y: 64, Z: 0, Stack: helmet},
		{X: 0, Y: 64, Z: 1.2, Stack: helmet},
		{X: 0, Y: 65.9, Z: 0, Stack: helmet},
	} {
		if takes := (Mobs{zombie}).TakeLoot([]LootItem{loot}); len(takes) != 1 {
			t.Errorf("the item at %v was not taken, want the one within the grown box", loot)
		}
	}
	for _, loot := range []LootItem{
		{X: 1.5, Y: 64, Z: 0, Stack: helmet},
		{X: 0, Y: 66.1, Z: 0, Stack: helmet},
		{X: 0, Y: 63.8, Z: 0, Stack: helmet},
	} {
		if takes := (Mobs{zombie}).TakeLoot([]LootItem{loot}); len(takes) != 0 {
			t.Errorf("the item at %v was taken, want the one out of the grown box", loot)
		}
	}

	// The takes of one body stand on what its own earlier takes dressed:
	// bare and bare-handed, one helmet goes to its head and the next to
	// its hand.
	bare := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	bare.CanPickUpLoot = true
	takes := Mobs{bare}.TakeLoot([]LootItem{
		{X: 0, Y: 64, Z: 0, Stack: stackOf(t, "minecraft:iron_helmet")},
		{X: 0.1, Y: 64, Z: 0, Stack: stackOf(t, "minecraft:iron_helmet")},
	})
	if len(takes) != 2 {
		t.Fatalf("the bare mob took %d of two helmets, want its head and its hand filled", len(takes))
	}
	for i, want := range []int{5, 0} {
		if got := wireSlot(takes[i].Slot); got != want {
			t.Errorf("take %d went to wire slot %d, want %d", i, got, want)
		}
	}

	// The claim's arithmetic the takes carry: an armour slot takes one of
	// a stack of many, a hand takes the stack whole.
	pile := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	pile.CanPickUpLoot = true
	held := stackOf(t, "minecraft:iron_helmet")
	held.Count = 5
	loose := stackOf(t, "minecraft:dirt")
	loose.Count = 7
	takes = Mobs{pile}.TakeLoot([]LootItem{
		{X: 0, Y: 64, Z: 0, Stack: held},
		{X: 0.1, Y: 64, Z: 0, Stack: loose},
	})
	if len(takes) != 2 {
		t.Fatalf("the pile-taking mob made %d takes, want its head and its hand", len(takes))
	}
	if takes[0].Count != 1 {
		t.Errorf("the armour take claims %d of the five, want the slot's one", takes[0].Count)
	}
	if takes[1].Count != 7 {
		t.Errorf("the hand take claims %d of the seven, want the stack whole", takes[1].Count)
	}
}

// handsAndHead arranges a main hand's stack and a helmet into the wire-slot
// order the cases' holds are laid out in.
func handsAndHead(mainhand, head protocol.ItemStack) [6]protocol.ItemStack {
	return [6]protocol.ItemStack{mainhand, {}, {}, {}, {}, head}
}

// wireSlot numbers a take's slot the way the Set Equipment packet does: the
// two hands, then the feet, the legs, the chest and the head.
func wireSlot(slot string) int {
	switch slot {
	case "mainhand":
		return 0
	case "offhand":
		return 1
	}
	for i, name := range ArmorSlotNames {
		if name == slot {
			return 2 + i
		}
	}
	return -1
}

// TestTheLootTakeReturnsThePieceItDisplaces checks the replacement a take
// makes: the piece a mob already wears or holds comes back off the slot it
// is swapped out of — always for a piece the body picked up itself, whose
// drop chance the pickup raised to a guaranteed two, and about the
// eighth-and-a-half-plus-tenth for one it spawned with. The same holds
// within one tick for two takes staged onto one slot.
func TestTheLootTakeReturnsThePieceItDisplaces(t *testing.T) {
	found := stackOf(t, "minecraft:diamond_helmet")
	head := lootSlotIndex("head")

	picked := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	picked.CanPickUpLoot = true
	picked.Worn[wornHead] = stackOf(t, "minecraft:iron_helmet")
	picked.DropChances[head] = 2
	takes := Mobs{picked}.TakeLoot([]LootItem{{Stack: found}})
	if len(takes) != 1 {
		t.Fatalf("the mob made %d takes of the better helmet, want one", len(takes))
	}
	if dropped := picked.TookLoot(takes[0], found); dropped.ItemID != stackOf(t, "minecraft:iron_helmet").ItemID {
		t.Errorf("the take dropped item %d, want the iron helmet it displaced", dropped.ItemID)
	}
	if got := picked.Worn[wornHead].ItemID; got != found.ItemID {
		t.Errorf("the head holds item %d, want the diamond helmet that displaced it", got)
	}

	// A spawn piece keeps the default chance: max(rand-0.1, 0) < 0.085,
	// which the roll clears about 18.5% of the time.
	spawn := NewMob(TypeZombie, 1, 0, 0, 0, 2)
	spawn.CanPickUpLoot = true
	iron := stackOf(t, "minecraft:iron_helmet")
	const n = 20000
	returned := 0
	for range n {
		spawn.Worn[wornHead] = iron
		spawn.DropChances[head] = equipmentDropChance
		takes := Mobs{spawn}.TakeLoot([]LootItem{{Stack: found}})
		if len(takes) != 1 {
			t.Fatalf("the spawn-geared mob made %d takes, want one", len(takes))
		}
		if !spawn.TookLoot(takes[0], found).Empty() {
			returned++
		}
	}
	if rate := float64(returned) / n; math.Abs(rate-0.185) > 0.02 {
		t.Errorf("a spawn piece came back %.3f of the time, want about 0.185", rate)
	}

	// Two takes staged onto one slot in a tick: the first piece is dropped
	// when the second takes its place.
	bare := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	bare.CanPickUpLoot = true
	takes = Mobs{bare}.TakeLoot([]LootItem{
		{X: 0, Y: 0, Z: 0, Stack: iron},
		{X: 0.1, Y: 0, Z: 0, Stack: found},
	})
	if len(takes) != 2 {
		t.Fatalf("the bare mob made %d takes of two helmets, want both staged for its head", len(takes))
	}
	if dropped := bare.TookLoot(takes[0], iron); !dropped.Empty() {
		t.Errorf("the first take dropped %+v, want the empty head's nothing", dropped)
	}
	if dropped := bare.TookLoot(takes[1], found); dropped.ItemID != iron.ItemID {
		t.Errorf("the second take dropped item %d, want the first helmet it displaced", dropped.ItemID)
	}
}

// TestAPickedUpPieceAlwaysDropsFromTheDeadBody checks the guarantee a
// pickup leaves on its slot: a piece the body took off the ground comes
// back off its death whatever killed it and keeps the wear it had, where a
// spawn piece would need a player's kill and roll its share.
func TestAPickedUpPieceAlwaysDropsFromTheDeadBody(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	zombie.CanPickUpLoot = true
	sword := stackOf(t, "minecraft:diamond_sword")
	sword.Damage = 123
	takes := Mobs{zombie}.TakeLoot([]LootItem{{Stack: sword}})
	if len(takes) != 1 {
		t.Fatalf("the mob made %d takes of the sword, want one", len(takes))
	}
	zombie.TookLoot(takes[0], sword)
	if zombie.DropChances[0] <= 1 {
		t.Fatal("a picked-up sword left its slot unguaranteed, want the drop the pickup marks")
	}
	for range 100 {
		found := false
		for _, drop := range zombie.RollDrops(rules.LootKiller{}, nil) {
			if drop.ItemID != sword.ItemID {
				continue
			}
			found = true
			if drop.Damage != sword.Damage {
				t.Errorf("the dropped sword has wear %d, want the %d it was picked up with", drop.Damage, sword.Damage)
			}
		}
		if !found {
			t.Fatal("a picked-up sword never dropped from a body that died to nobody")
		}
	}
}

// TestTheLootWishesKeepTheKindsOwnStacksOut checks the refusals the kinds
// put ahead of the slot questions: the skeleton family and the drowned pass
// on the spears, the zombie family on the glow ink sac, and a baby zombie
// riding something on the eggs — while a plain zombie keeps taking what the
// refusals leave.
func TestTheLootWishesKeepTheKindsOwnStacksOut(t *testing.T) {
	cases := []struct {
		name  string
		kind  MobType
		baby  bool
		rides bool
		item  string
		want  bool
	}{
		{"a skeleton refuses a spear", TypeSkeleton, false, false, "minecraft:iron_spear", false},
		{"a stray refuses a spear", TypeStray, false, false, "minecraft:iron_spear", false},
		{"a bogged refuses a spear", TypeBogged, false, false, "minecraft:iron_spear", false},
		{"a drowned refuses a spear", TypeDrowned, false, false, "minecraft:iron_spear", false},
		{"a zombie takes a spear", TypeZombie, false, false, "minecraft:iron_spear", true},
		{"a zombie refuses the glow ink sac", TypeZombie, false, false, "minecraft:glow_ink_sac", false},
		{"a baby riding refuses an egg", TypeZombie, true, true, "minecraft:egg", false},
		{"a baby afoot takes an egg", TypeZombie, true, false, "minecraft:egg", true},
		{"a grown rider takes an egg", TypeZombie, false, true, "minecraft:egg", true},
	}
	for _, tc := range cases {
		m := NewMob(tc.kind, 1, 0, 0, 0, 1)
		m.CanPickUpLoot = true
		m.RidesVehicle = tc.rides
		if tc.baby {
			m.SetBaby(true)
		}
		takes := Mobs{m}.TakeLoot([]LootItem{{Stack: stackOf(t, tc.item)}})
		if got := len(takes) > 0; got != tc.want {
			t.Errorf("%s: taken=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestTheDrownedPrefersTheTridentAndKeepsItsShell checks the drowned's own
// half of the weapon ranking and its shell's guard: a trident outranks a
// harder-hitting axe, the trident it already holds is not swapped for a
// sword, and the nautilus shell it carries is never replaced, even in the
// main hand where its preference would otherwise hand the slot to a sword.
func TestTheDrownedPrefersTheTridentAndKeepsItsShell(t *testing.T) {
	// The netherite axe hits harder than the trident, so only the kind's
	// own preference puts the trident ahead of it.
	axed := NewMob(TypeDrowned, 1, 0, 0, 0, 1)
	axed.CanPickUpLoot = true
	axed.MainHand = stackOf(t, "minecraft:netherite_axe")
	takes := Mobs{axed}.TakeLoot([]LootItem{{Stack: stackOf(t, "minecraft:trident")}})
	if len(takes) != 1 || wireSlot(takes[0].Slot) != 0 {
		t.Fatalf("the drowned made %d takes of the trident over the axe, want it in the hand", len(takes))
	}

	armed := NewMob(TypeDrowned, 1, 0, 0, 0, 2)
	armed.CanPickUpLoot = true
	armed.MainHand = stackOf(t, "minecraft:trident")
	if takes := (Mobs{armed}).TakeLoot([]LootItem{{Stack: stackOf(t, "minecraft:iron_sword")}}); len(takes) != 0 {
		t.Errorf("a trident-holding drowned took a sword, want the trident kept")
	}

	// The shell sits in the main hand so the sword's damage would win the
	// slot without the guard; the off hand is never replaced anyway.
	shelled := NewMob(TypeDrowned, 1, 0, 0, 0, 3)
	shelled.CanPickUpLoot = true
	shelled.MainHand = stackOf(t, "minecraft:nautilus_shell")
	if takes := (Mobs{shelled}).TakeLoot([]LootItem{{Stack: stackOf(t, "minecraft:iron_sword")}}); len(takes) != 0 {
		t.Errorf("a drowned swapped its nautilus shell for a sword, want the shell kept")
	}
}

// TestAMobRefusesTheSlotsItCannotWear checks the equippable gate: a piece
// whose component names an animal's body or saddle slot is refused — a
// dropped carpet or saddle stays on the ground — while a piece for a slot
// the mob has, and a plain item, are still taken.
func TestAMobRefusesTheSlotsItCannotWear(t *testing.T) {
	cases := []struct {
		item string
		want bool
	}{
		{"minecraft:white_carpet", false},
		{"minecraft:saddle", false},
		{"minecraft:iron_horse_armor", false},
		{"minecraft:wolf_armor", false},
		{"minecraft:carved_pumpkin", true},
		{"minecraft:dirt", true},
	}
	for _, tc := range cases {
		m := NewMob(TypeZombie, 1, 0, 0, 0, 1)
		m.CanPickUpLoot = true
		takes := Mobs{m}.TakeLoot([]LootItem{{Stack: stackOf(t, tc.item)}})
		if got := len(takes) > 0; got != tc.want {
			t.Errorf("a zombie taking a %s: taken=%v, want %v", tc.item, got, tc.want)
		}
	}
}

// TestAnArmourPieceUnderTheBindingCurseIsNotSwapped checks the curse's
// guard: a worn piece carrying binding_curse stays put though a better one
// is found — the found piece falling back to an empty hand alone — where a
// plain piece of the same make is swapped.
func TestAnArmourPieceUnderTheBindingCurseIsNotSwapped(t *testing.T) {
	cursed := stackOf(t, "minecraft:iron_helmet")
	curse, _ := registry.EnchantmentID("minecraft:binding_curse")
	cursed.Enchantments = map[int32]int32{curse: 1}
	found := stackOf(t, "minecraft:diamond_helmet")

	bound := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	bound.CanPickUpLoot = true
	bound.Worn[wornHead] = cursed
	bound.MainHand = stackOf(t, "minecraft:iron_sword")
	if takes := (Mobs{bound}).TakeLoot([]LootItem{{Stack: found}}); len(takes) != 0 {
		t.Errorf("a cursed helmet was swapped, want the piece kept")
	}

	plain := NewMob(TypeZombie, 1, 0, 0, 0, 2)
	plain.CanPickUpLoot = true
	plain.Worn[wornHead] = stackOf(t, "minecraft:iron_helmet")
	plain.MainHand = stackOf(t, "minecraft:iron_sword")
	takes := Mobs{plain}.TakeLoot([]LootItem{{Stack: found}})
	if len(takes) != 1 || wireSlot(takes[0].Slot) != 5 {
		t.Errorf("a plain helmet made %d takes, want the diamond one worn", len(takes))
	}
}

// TestAnEquipmentSlotDropsByItsOwnChance checks the per-slot chance a death
// rolls: a chance above one drops the piece undamaged without a player's
// kill, a chance of nought never drops, and the default drops nothing
// without one.
func TestAnEquipmentSlotDropsByItsOwnChance(t *testing.T) {
	helmet := stackOf(t, "minecraft:diamond_helmet")
	drops := func(chance float32, slot int) int {
		m := NewMob(TypeZombie, 1, 0, 0, 0, 1)
		m.setSlotStack(slot, helmet)
		m.DropChances[slot] = chance
		m.Dead = true
		n := 0
		for _, d := range m.RollDrops(rules.LootKiller{}, nil) {
			if d.ItemID == helmet.ItemID {
				if d.Damage != 0 {
					t.Errorf("the guaranteed helmet dropped with damage %d, want none", d.Damage)
				}
				n++
			}
		}
		return n
	}
	head := lootSlotIndex("head")
	if got := drops(2, head); got != 1 {
		t.Errorf("a head chance of 2 dropped %d helmets, want 1", got)
	}
	if got := drops(2, lootSlotIndex("mainhand")); got != 1 {
		t.Errorf("a mainhand chance of 2 dropped %d pieces, want 1", got)
	}
	if got := drops(equipmentDropChance, head); got != 0 {
		t.Errorf("the default chance dropped %d helmets without a player kill, want 0", got)
	}
	if got := drops(0, head); got != 0 {
		t.Errorf("a chance of 0 dropped %d helmets, want 0", got)
	}
}

// TestSpawnEquipmentIsEnchantedOnTheSpecialMultiplier checks the enchant
// roll over many seeds: a skeleton's bow is enchanted at a quarter and its
// armour at a half of the special multiplier, only from the
// on_mob_spawn_equipment pool, and a spawn at nought enchants nothing.
func TestSpawnEquipmentIsEnchantedOnTheSpecialMultiplier(t *testing.T) {
	const n = 20000
	pool := map[int32]bool{}
	for _, id := range registry.EnchantmentTagIDs("minecraft:on_mob_spawn_equipment") {
		pool[id] = true
	}
	if len(pool) == 0 {
		t.Fatal("the on_mob_spawn_equipment tag is empty")
	}
	var bows, bowsEnchanted, armour, armourEnchanted int
	for seed := int64(1); seed <= n; seed++ {
		m := NewMob(TypeSkeleton, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, hard())
		bows++
		if len(m.MainHand.Enchantments) > 0 {
			bowsEnchanted++
		}
		stacks := []protocol.ItemStack{m.MainHand}
		stacks = append(stacks, m.Worn[:]...)
		for i, st := range stacks {
			if i > 0 && !st.Empty() {
				armour++
				if len(st.Enchantments) > 0 {
					armourEnchanted++
				}
			}
			for id := range st.Enchantments {
				if !pool[id] {
					t.Fatalf("seed %d: enchantment %d is not in on_mob_spawn_equipment", seed, id)
				}
			}
		}
		calm := NewMob(TypeSkeleton, 1, 0, 64, 0, seed)
		calm.FinalizeSpawn(GoDice{calm.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		for _, st := range append([]protocol.ItemStack{calm.MainHand}, calm.Worn[:]...) {
			if len(st.Enchantments) > 0 {
				t.Fatalf("seed %d: enchanted %v at a special multiplier of nought", seed, st.Enchantments)
			}
		}
	}
	if got := float64(bowsEnchanted) / float64(bows); got < 0.23 || got > 0.27 {
		t.Errorf("%d of %d bows enchanted, want about 25%%", bowsEnchanted, bows)
	}
	if got := float64(armourEnchanted) / float64(armour); got < 0.47 || got > 0.53 {
		t.Errorf("%d of %d armour pieces enchanted, want about 50%%", armourEnchanted, armour)
	}
}

// TestSpawnEnchantmentRollsArePinnedForASeed pins one seeded roll: the
// roll of an iron sword and helmet on the dice is the draw below.
func TestSpawnEnchantmentRollsArePinnedForASeed(t *testing.T) {
	roll := func() map[int32]int32 {
		m := NewMob(TypeZombie, 1, 0, 64, 0, 1)
		m.MainHand = stackOf(t, "minecraft:iron_sword")
		m.Worn[wornHead] = stackOf(t, "minecraft:iron_helmet")
		m.rollSpawnEnchantments(GoDice{rand.New(rand.NewSource(7))}, hard())
		all := map[int32]int32{}
		maps.Copy(all, m.MainHand.Enchantments)
		for id, lvl := range m.Worn[wornHead].Enchantments {
			all[1000+id] = lvl
		}
		return all
	}
	// The head's ids are offset by 1000.
	want := map[int32]int32{1000: 1, 1028: 2}
	got := roll()
	if len(got) != len(want) {
		t.Fatalf("seed 7 rolled %v, want %v", got, want)
	}
	for id, lvl := range want {
		if got[id] != lvl {
			t.Errorf("seed 7 rolled %v, want %v", got, want)
		}
	}
}

// TestHorseArmourTakesItsShareOfABlow checks a horse's body armour: the iron
// horse armour's five points, which a donkey's body slot never holds.
func TestHorseArmourTakesItsShareOfABlow(t *testing.T) {
	m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 3)
	m.Body = stackOf(t, "minecraft:iron_horse_armor")
	if got, want := m.armoured(9), rules.DamageAfterArmour(9, 5, 0); got != want {
		t.Errorf("with iron horse armour a blow of nine leaves %v, want %v", got, want)
	}
	if !m.Wears("body", m.Body) || NewMob(TypeDonkey, 2, 0, 64, 0, 3).Wears("body", m.Body) {
		t.Error("horse armour fits the horse and not the donkey")
	}
}

// TestAPackProviderReplacesMobSpawnEquipment installs a pack's
// mob_spawn_equipment as a single sharpness 5 and enchants an iron sword with
// the spawn roll: the sword gets sharpness 5 where the built-in cost roll
// would give it whatever the dice pick. The table is process-wide, so the
// test puts it back.
// vanilla: VanillaEnchantmentProviders.MOB_SPAWN_EQUIPMENT, SingleEnchantment.enchant
func TestAPackProviderReplacesMobSpawnEquipment(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackEnchantmentProviders(nil) })
	registry.ApplyPackEnchantmentProviders(map[string]registry.EnchantmentProvider{
		"minecraft:mob_spawn_equipment": {
			Kind: registry.EnchProviderSingle, Names: []string{"minecraft:sharpness"},
			Level: registry.IntRange{Min: 5, Max: 5},
		},
	})
	sharpness, _ := registry.EnchantmentID("minecraft:sharpness")
	stack := protocol.ItemStack{ItemID: int32(registry.ItemIronSword), Count: 1}
	enchantForSpawn(rules.NewJavaRandom(7), &stack, rules.LocalDifficulty{Special: 1})
	if got := stack.Enchantments[sharpness]; got != 5 || len(stack.Enchantments) != 1 {
		t.Fatalf("enchantments = %v, want only sharpness 5", stack.Enchantments)
	}
}
