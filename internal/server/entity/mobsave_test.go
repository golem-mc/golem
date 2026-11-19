package entity

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: TamableAnimal.readAdditionalSaveData, AbstractHorse.readAdditionalSaveData
func TestAnimalsLoadOwnerUUIDStrings(t *testing.T) {
	t.Parallel()
	owner := uuid.MustParse("f3d28cb0-7225-3cb1-baeb-2dadd2be89ae")
	for _, kind := range []MobType{TypeWolf, TypeCat, TypeParrot, TypeNautilus, TypeZombieNautilus, TypeHorse, TypeDonkey, TypeLlama} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			m.LoadFrom(nbt.Compound{"Owner": nbt.String(owner.String())}, nil, func() int64 { return 0 })
			got := m.Tamable.Owner
			if IsEquine(kind) {
				got = m.Horse.Owner
			} else if !m.Tamable.Tame {
				t.Error("animal with an owner is not tame")
			}
			if got != owner {
				t.Errorf("owner = %v, want %v", got, owner)
			}
			tag := nbt.Compound{}
			m.SaveTo(tag, func() int64 { return 0 })
			if saved, ok := nbt.UUIDField(tag, "Owner"); !ok || saved != owner {
				t.Errorf("saved Owner = %v, want UUID int array for %v", tag["Owner"], owner)
			}
		})
	}
}

func TestAnimalOwnerUUIDFormats(t *testing.T) {
	t.Parallel()
	owner := uuid.MustParse("f3d28cb0-7225-3cb1-baeb-2dadd2be89ae")
	for _, tc := range []struct {
		name string
		tag  nbt.Tag
		ok   bool
	}{
		{"string", nbt.String(owner.String()), true},
		{"uppercase", nbt.String("F3D28CB0-7225-3CB1-BAEB-2DADD2BE89AE"), true},
		{"int array", nbt.UUIDToIntArray(owner), true},
		{"int list", nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(-204305232), nbt.Int(1915042993), nbt.Int(-1158992467), nbt.Int(-759264850)}}, true},
		{"missing", nil, false},
		{"empty", nbt.String(""), false},
		{"malformed", nbt.String("g3d28cb0-7225-3cb1-baeb-2dadd2be89ae"), false},
		{"undashed", nbt.String("f3d28cb072253cb1baeb2dadd2be89ae"), false},
		{"short array", nbt.IntArray{1, 2, 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := animalOwnerUUID(nbt.Compound{"Owner": tc.tag})
			if ok != tc.ok || ok && got != owner {
				t.Errorf("owner = %v, %v; want %v, %v", got, ok, owner, tc.ok)
			}
		})
	}
}

// TestEveryKindReadsBackWhatItSaved: once a body's save has been loaded onto
// a fresh body of the kind (which settles what a load caps or marks, such as
// a cube's health and a villager's finalized data), loading and saving again
// changes nothing.
func TestEveryKindReadsBackWhatItSaved(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 1000 }
	for i, k := range kinds {
		if k == nil {
			continue
		}
		kind := MobType(i)
		m := NewMob(kind, 1, 0.5, 64, 0.5, 7)
		m.Health = 3
		m.AirSupply = 120
		m.FallDistance = 2.5
		m.Persistence, m.NoAI, m.Silent, m.Glowing, m.LeftHanded = true, true, true, true, true
		m.DropChances[0] = 1
		if Ageable(kind) || Family(kind) == TypeZombie {
			m.SetBaby(true)
		}
		reload := func(from *Mob, seed int64) (*Mob, nbt.Compound) {
			saved := nbt.Compound{}
			from.SaveTo(saved, now)
			back := NewMob(kind, 2, 0.5, 64, 0.5, seed)
			back.LoadFrom(saved, nil, now)
			return back, saved
		}
		back, _ := reload(m, 8)
		back, saved := reload(back, 9)
		again := nbt.Compound{}
		back.SaveTo(again, now)
		if !reflect.DeepEqual(saved, again) {
			t.Errorf("%s saved\n%v\nand after a reload\n%v", kind, saved, again)
		}
	}
}

// TestASavedMobKeepsItsKindsFields spot-checks a few kinds' own fields
// through a save and a load.
func TestASavedMobKeepsItsKindsFields(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	owner := uuid.MustParse("01020304-0506-0708-090a-0b0c0d0e0f10")

	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	wolf.Tamable.Tame, wolf.Tamable.Owner, wolf.Tamable.OrderedToSit = true, owner, true
	wolf.Collar = "blue"
	tag := nbt.Compound{}
	wolf.SaveTo(tag, now)
	back := NewMob(TypeWolf, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if !back.Tamable.Tame || back.Tamable.Owner != owner || !back.Tamable.OrderedToSit || back.Collar != "blue" || back.MaxHealth != 40 {
		t.Errorf("wolf came back tame %v owner %v sitting %v collar %q max %v",
			back.Tamable.Tame, back.Tamable.Owner, back.Tamable.OrderedToSit, back.Collar, back.MaxHealth)
	}

	sheep := NewMob(TypeSheep, 1, 0.5, 64, 0.5, 1)
	sheep.Colour, sheep.Sheared = "lime", true
	tag = nbt.Compound{}
	sheep.SaveTo(tag, now)
	if c, _ := nbt.Get[nbt.Byte](tag, "Color"); c != nbt.Byte(DyeOfColour("lime")) {
		t.Errorf("sheep saved Color %d, want lime's %d", c, DyeOfColour("lime"))
	}
	back = NewMob(TypeSheep, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.Colour != "lime" || !back.Sheared {
		t.Errorf("sheep came back %q sheared %v", back.Colour, back.Sheared)
	}

	zombie := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
	zombie.SetTraits(ZombieTraits{ReinforceBase: 0.05, LeaderHealth: 1.5})
	tag = nbt.Compound{}
	zombie.SaveTo(tag, now)
	back = NewMob(TypeZombie, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.Traits() != zombie.Traits() || back.MaxHealth != zombie.MaxHealth {
		t.Errorf("zombie came back with traits %+v max %v, want %+v max %v",
			back.Traits(), back.MaxHealth, zombie.Traits(), zombie.MaxHealth)
	}
}

// TestASavedCatKeepsItsCollar: a cat's collar goes through a save and a load
// as the CollarColor byte, and a cat saved without one comes back red.
// vanilla: Cat.addAdditionalSaveData, Cat.readAdditionalSaveData
func TestASavedCatKeepsItsCollar(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	cat := NewMob(TypeCat, 1, 0.5, 64, 0.5, 1)
	cat.Collar = "blue"
	tag := nbt.Compound{}
	cat.SaveTo(tag, now)
	if c, _ := nbt.Get[nbt.Byte](tag, "CollarColor"); c != nbt.Byte(DyeOfColour("blue")) {
		t.Errorf("cat saved CollarColor %d, want blue's %d", c, DyeOfColour("blue"))
	}
	back := NewMob(TypeCat, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.Collar != "blue" {
		t.Errorf("cat came back with collar %q, want blue", back.Collar)
	}
	delete(tag, "CollarColor")
	back.LoadFrom(tag, nil, now)
	if back.Collar != WolfDefaultCollar {
		t.Errorf("cat saved without a collar came back %q, want %q", back.Collar, WolfDefaultCollar)
	}
}

// TestADonkeysChestAndArmourSurviveASave: the chest's slots and the body
// slot come back from the file.
// vanilla: AbstractChestedHorse.addAdditionalSaveData
func TestADonkeysChestAndArmourSurviveASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	donkey := NewMob(TypeDonkey, 1, 0.5, 64, 0.5, 1)
	donkey.fitChest()
	donkey.Horse.Items[14] = stackOf(t, "minecraft:stone")
	tag := nbt.Compound{}
	donkey.SaveTo(tag, now)
	back := NewMob(TypeDonkey, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if !back.Horse.Chest || len(back.Horse.Items) != 15 || !back.Horse.Items[14].Equal(donkey.Horse.Items[14]) {
		t.Errorf("donkey came back with chest %v and items %v", back.Horse.Chest, back.Horse.Items)
	}

	horse := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
	horse.Body = stackOf(t, "minecraft:iron_horse_armor")
	tag = nbt.Compound{}
	horse.SaveTo(tag, now)
	back = NewMob(TypeHorse, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if !back.Body.Equal(horse.Body) {
		t.Errorf("horse came back wearing %+v", back.Body)
	}
}

func TestFrogPregnancySurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	for _, pregnant := range []bool{false, true} {
		m := NewMob(TypeFrog, 1, 0.5, 64, 0.5, 1)
		m.pregnant = pregnant
		tag := nbt.Compound{}
		m.SaveTo(tag, now)
		back := NewMob(TypeFrog, 2, 0.5, 64, 0.5, 2)
		back.pregnant = !pregnant
		back.LoadFrom(tag, nil, now)
		if back.pregnant != pregnant {
			t.Errorf("loaded pregnancy = %v, want %v", back.pregnant, pregnant)
		}
		delete(tag, "Brain")
		back.LoadFrom(tag, nil, now)
		if back.pregnant {
			t.Error("frog loaded without pregnancy field is pregnant")
		}
	}
}

func TestAPigsSaddleSurvivesALoad(t *testing.T) {
	t.Parallel()
	m := NewMob(TypePig, 1, 0.5, 64, 0.5, 1)
	m.Horse.Saddle = protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
	tag := nbt.Compound{}
	now := func() int64 { return 0 }
	m.SaveTo(tag, now)
	back := NewMob(TypePig, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if back.Horse.Saddle.ItemID != int32(registry.ItemSaddle) || back.Horse.Saddle.Count != 1 {
		t.Fatalf("loaded saddle = %+v", back.Horse.Saddle)
	}
	if !back.Horse.NeedsStick {
		t.Fatal("loaded pig steers without a stick")
	}
}

func TestGenericEquipmentSurvivesASave(t *testing.T) {
	t.Parallel()
	for _, kind := range []MobType{TypeZombie, TypeCopperGolem} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			stack := func(item, slot string) protocol.ItemStack {
				t.Helper()
				s, ok := registry.StoredStack(nbt.Compound{
					"id": nbt.String(item), "count": nbt.Int(1),
					"components": nbt.Compound{
						"minecraft:equippable":  nbt.Compound{"slot": nbt.String(slot)},
						"minecraft:custom_data": nbt.Compound{"marker": nbt.String(slot)},
					},
				})
				if !ok {
					t.Fatal("invalid stored stack")
				}
				return s
			}
			body := stack("minecraft:stone", "body")
			saddle := stack("minecraft:diamond_helmet", "saddle")
			m.Body = body
			if kind == TypeCopperGolem {
				m.Antenna = saddle
			} else {
				m.Horse.Saddle = saddle
			}
			tag := nbt.Compound{}
			now := func() int64 { return 0 }
			m.SaveTo(tag, now)
			back := NewMob(kind, 2, 0.5, 64, 0.5, 2)
			back.LoadFrom(tag, nil, now)
			if !back.Body.Equal(body) {
				t.Errorf("loaded body = %+v, want %+v", back.Body, body)
			}
			got := back.Horse.Saddle
			if kind == TypeCopperGolem {
				got = back.Antenna
				if !back.Horse.Saddle.Empty() {
					t.Error("copper golem's saddle slot also loaded into Horse.Saddle")
				}
			}
			if !got.Equal(saddle) {
				t.Errorf("loaded saddle = %+v, want %+v", got, saddle)
			}
			again := nbt.Compound{}
			back.SaveTo(again, now)
			if !reflect.DeepEqual(tag["equipment"], again["equipment"]) {
				t.Errorf("equipment changed after reload: %v -> %v", tag["equipment"], again["equipment"])
			}
		})
	}
}

// TestASavedWolfListsItsMaxHealthBase checks the attributes entry taming
// sets: base 8 wild and 40 tame, kept through a reload without waking a map.
func TestASavedWolfListsItsMaxHealthBase(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 0 }
	for _, tame := range []bool{false, true} {
		wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
		want := 8.0
		if tame {
			wolf.Tamable.Tame, wolf.Tamable.Owner = true, uuid.MustParse("01020304-0506-0708-090a-0b0c0d0e0f10")
			wolf.MaxHealth, want = 40, 40
		}
		tag := nbt.Compound{}
		wolf.SaveTo(tag, now)
		list, _ := nbt.Get[nbt.List](tag, "attributes")
		var bases []float64
		for _, it := range list.Items {
			c, _ := it.(nbt.Compound)
			if id, _ := nbt.Get[nbt.String](c, "id"); id == "minecraft:max_health" {
				bases = append(bases, nbt.DoubleFieldOr(c, "base", -1))
			}
		}
		if len(bases) != 1 || bases[0] != want {
			t.Errorf("tame %v: max_health bases %v in %v, want one of %v", tame, bases, list, want)
		}
		back := NewMob(TypeWolf, 2, 0.5, 64, 0.5, 2)
		back.LoadFrom(tag, nil, now)
		again := nbt.Compound{}
		back.SaveTo(again, now)
		if back.Attributes() != nil || back.MaxHealth != want || !reflect.DeepEqual(again["attributes"], tag["attributes"]) {
			t.Errorf("tame %v: reload max %v, map %v, attributes %v", tame, back.MaxHealth, back.Attributes() != nil, again["attributes"])
		}
	}
}

// TestATameWolfKeepsAnEditedMaxHealthBase checks that a tame wolf's saved
// max_health base other than 40 is an edit: it wakes the map, caps the
// health and is saved again.
func TestATameWolfKeepsAnEditedMaxHealthBase(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 0 }
	tag := nbt.Compound{
		"Owner":  nbt.UUIDToIntArray(uuid.MustParse("01020304-0506-0708-090a-0b0c0d0e0f10")),
		"Health": nbt.Float(20),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String(attrMaxHealth), "base": nbt.Double(20)},
		}},
	}
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	wolf.LoadFrom(tag, nil, now)
	if got := wolf.healthCap(); got != 20 {
		t.Fatalf("health cap %v, want 20", got)
	}
	again := nbt.Compound{}
	wolf.SaveTo(again, now)
	if wolf.Attributes() == nil {
		t.Fatal("edited base woke no map")
	}
	// The server's save writes the map over the list.
	list, _ := nbt.Get[nbt.List](again, "attributes")
	if out := wolf.Attributes().Save(&list); out != nil {
		list = *out
	}
	var bases []float64
	for _, it := range list.Items {
		c, _ := nbt.CompoundView(it)
		if id, _ := nbt.Get[nbt.String](c, "id"); id == attrMaxHealth {
			bases = append(bases, nbt.DoubleFieldOr(c, "base", -1))
		}
	}
	if len(bases) != 1 || bases[0] != 20 {
		t.Errorf("saved max_health bases %v in %v, want [20]", bases, list)
	}
}

// TestASleepingVillagerSavesItsBed: sleeping_pos round-trips the bed head.
// vanilla: LivingEntity.addAdditionalSaveData
func TestASleepingVillagerSavesItsBed(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	v := NewMob(TypeVillager, 1, 3.5, 64.6875, 0.5, 1)
	v.Sleeping = true
	v.vb.sleepPos = Waypoint{3, 64, 0}
	tag := nbt.Compound{}
	v.SaveTo(tag, now)
	if got, _ := nbt.Get[nbt.IntArray](tag, "sleeping_pos"); !reflect.DeepEqual(got, nbt.IntArray{3, 64, 0}) {
		t.Fatalf("saved sleeping_pos = %v, want [3 64 0]", tag["sleeping_pos"])
	}
	back := NewMob(TypeVillager, 2, 3.5, 64.6875, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if x, y, z, ok := back.SleepingPos(); !ok || x != 3 || y != 64 || z != 0 {
		t.Errorf("loaded SleepingPos = %d %d %d %v, want 3 64 0 true", x, y, z, ok)
	}
}

// TestADeathLootTableSurvivesASave: both keys load and save back unchanged.
func TestADeathLootTableSurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"DeathLootTable": nbt.String("minecraft:entities/cow"), "DeathLootTableSeed": nbt.Long(42)}, nil, now)
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if got, _ := nbt.Get[nbt.String](tag, "DeathLootTable"); got != "minecraft:entities/cow" {
		t.Errorf("saved DeathLootTable %q", got)
	}
	if got, _ := nbt.Get[nbt.Long](tag, "DeathLootTableSeed"); got != 42 {
		t.Errorf("saved DeathLootTableSeed %d", got)
	}
}

func TestAnAllaysLikedPlayerSurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	id := uuid.MustParse("01020304-0506-0708-090a-0b0c0d0e0f10")
	m := NewMob(TypeAllay, 1, 0.5, 64, 0.5, 7)
	m.UseAllay(protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1}, protocol.HandMain, id, 5)
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	brain, _ := nbt.Get[nbt.Compound](tag, "Brain")
	memories, _ := nbt.Get[nbt.Compound](brain, "memories")
	liked, _ := nbt.Get[nbt.Compound](memories, "minecraft:liked_player")
	if got, want := liked["value"], nbt.UUIDToIntArray(id); !reflect.DeepEqual(got, want) {
		t.Fatalf("liked_player value = %v, want %v", got, want)
	}
	back := NewMob(TypeAllay, 2, 0.5, 64, 0.5, 8)
	back.LoadFrom(tag, nil, now)
	if !back.AllayLikesPlayer(id) {
		t.Fatal("the loaded allay does not like the player")
	}
}

func TestAPiglinsCannotHuntSurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"CannotHunt": nbt.Byte(1)}, nil, now)
	if m.canHunt() {
		t.Error("a piglin loaded with CannotHunt can hunt")
	}
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if got, ok := tag["CannotHunt"].(nbt.Byte); !ok || got != 1 {
		t.Errorf("saved CannotHunt = %v, want 1b", tag["CannotHunt"])
	}
}

func TestAHoglinsCannotBeHuntedSurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	m := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"CannotBeHunted": nbt.Byte(1)}, nil, now)
	if huntableHoglin(m) {
		t.Error("a hoglin loaded with CannotBeHunted can be hunted")
	}
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if got, ok := tag["CannotBeHunted"].(nbt.Byte); !ok || got != 1 {
		t.Errorf("saved CannotBeHunted = %v, want 1b", tag["CannotBeHunted"])
	}
}

func TestAPiglinsTimeInOverworldSurvivesASave(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 500 }
	m := NewMob(TypePiglin, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{"TimeInOverworld": nbt.Int(120)}, nil, now)
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if got, ok := tag["TimeInOverworld"].(nbt.Int); !ok || got != 120 {
		t.Errorf("saved TimeInOverworld = %v, want 120", tag["TimeInOverworld"])
	}
}

// TestAGrazingHorseSavesEatingHaystack: the graze is saved and read back.
// vanilla: AbstractHorse.addAdditionalSaveData, readAdditionalSaveData
func TestAGrazingHorseSavesEatingHaystack(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 0 }
	m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
	m.Horse.Eating = true
	tag := nbt.Compound{}
	m.SaveTo(tag, now)
	if b, ok := nbt.ByteValue(tag["EatingHaystack"]); !ok || b != 1 {
		t.Fatalf("EatingHaystack = %v, want 1b", tag["EatingHaystack"])
	}
	back := NewMob(TypeHorse, 2, 0.5, 64, 0.5, 2)
	back.LoadFrom(tag, nil, now)
	if !back.Horse.Eating {
		t.Error("a reloaded horse stopped grazing")
	}
}
