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
