package entity

import (
	"slices"
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

// The kinds a finalize gives the loot pickup to: the zombie family's and
// the skeleton family's own finalizes roll it, no other kind's does.
var lootRolling = []MobType{
	TypeZombie, TypeHusk, TypeDrowned, TypeZombieVillager,
	TypeSkeleton, TypeStray, TypeBogged,
}

func TestIndexedLootMatchesDirectPickupOrder(t *testing.T) {
	mobs := make(Mobs, 8)
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i+1), 16, 64, 16, int64(i+1))
		mobs[i].CanPickUpLoot = true
		mobs[i].MainHand = stackOf(t, "minecraft:iron_sword")
		if i >= 4 {
			mobs[i].X = 400
		}
	}
	cube := NewMob(TypeSulfurCube, 3, 16, 64, 32, 3)
	allay := NewMob(TypeAllay, 4, 16, 64, 48, 4)
	allay.MainHand = stackOf(t, "minecraft:stick")
	mobs[2], mobs[3] = cube, allay

	items := make([]LootItem, 32)
	copy(items, []LootItem{
		{X: 16.5, Y: 64, Z: 16.5, Stack: stackOf(t, "minecraft:leather_helmet")},
		{X: 16, Y: 64, Z: 16},
		{X: 15.5, Y: 64, Z: 15.5, Stack: stackOf(t, "minecraft:iron_helmet")},
		{X: 16, Y: 64, Z: 16, Stack: stackOf(t, "minecraft:diamond_helmet")},
		{X: 16, Y: 63.5, Z: 16, Stack: stackOf(t, "minecraft:netherite_helmet")},
		{X: 25, Y: 64, Z: 16, Stack: stackOf(t, "minecraft:netherite_helmet")},
		{X: 16, Y: 64, Z: 32, Stack: stackOf(t, "minecraft:iron_helmet")},
		{X: 16.5, Y: 64, Z: 32.5, Stack: stackOf(t, "minecraft:stone")},
		{X: 15.5, Y: 64, Z: 31.5, Stack: stackOf(t, "minecraft:dirt")},
		{X: 16, Y: 63.25, Z: 48, Stack: allay.MainHand},
		{X: 15.5, Y: 64, Z: 47.5, Stack: allay.MainHand},
	})
	// Both zombies can claim the same helmets. The caller arbitrates claims.
	var want []LootTake
	for _, m := range mobs[:2] {
		for _, i := range []int{0, 2, 3} {
			want = append(want, LootTake{Mob: m, Item: i, Slot: "head", Count: 1})
		}
	}
	want = append(want,
		LootTake{Mob: cube, Item: 7, Slot: "body", Count: 1},
		LootTake{Mob: allay, Item: 9, Slot: "inventory", Count: 1},
		LootTake{Mob: allay, Item: 10, Slot: "inventory", Count: 1},
	)
	var direct []LootTake
	for _, m := range mobs {
		direct = append(direct, (Mobs{m}).TakeLoot(items)...)
	}
	if !slices.Equal(direct, want) {
		t.Fatalf("direct takes = %+v, want %+v", direct, want)
	}
	var grid BoxGrid
	if indexed := mobs.TakeLootWithGrid(items, &grid); !slices.Equal(indexed, direct) {
		t.Fatalf("indexed takes = %+v, want %+v", indexed, direct)
	}
	// A moved helmet must leave its old cells without disturbing pickup order.
	items[3].X = 48
	want = slices.DeleteFunc(want, func(take LootTake) bool { return take.Item == 3 })
	if indexed := mobs.TakeLootWithGrid(items, &grid); !slices.Equal(indexed, want) {
		t.Fatalf("indexed takes after moving an item = %+v, want %+v", indexed, want)
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
