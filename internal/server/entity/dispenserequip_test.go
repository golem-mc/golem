package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestArmorStandDispenserEquip(t *testing.T) {
	t.Parallel()
	stack := func(name string) protocol.ItemStack {
		return protocol.ItemStack{ItemID: registry.MustItem(name), Count: 1}
	}
	stand := func() *ArmorStand { return NewArmorStand(1, 0, 0, 0) }

	bare := stand()
	if !bare.DispenserEquip(stack("minecraft:iron_helmet")) || bare.StackOf(StandHead).Empty() {
		t.Error("a bare head refused a helmet")
	}
	if bare.DispenserEquip(stack("minecraft:iron_helmet")) {
		t.Error("a worn head took a second helmet")
	}

	locked := stand()
	locked.DisabledSlots = 1 << standFilterBit(StandChest)
	if locked.DispenserEquip(stack("minecraft:iron_chestplate")) {
		t.Error("a disabled chest took a chestplate")
	}

	dead := stand()
	dead.Dead = true
	if dead.DispenserEquip(stack("minecraft:iron_boots")) {
		t.Error("a dead stand took boots")
	}

	// The off hand is disabled until the stand shows arms.
	if stand().DispenserEquip(stack("minecraft:shield")) {
		t.Error("an armless stand took a shield")
	}
	armed := stand()
	armed.ShowArms = true
	if !armed.DispenserEquip(stack("minecraft:shield")) || armed.StackOf(StandOffHand).Empty() {
		t.Error("an armed stand refused a shield")
	}
	if stand().DispenserEquip(stack("minecraft:saddle")) {
		t.Error("a stand took a saddle")
	}
}

// vanilla: Equippable.equipOnTarget, LivingEntity.isEquippableInSlot
func TestEquipOnInteractSlotRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		kind                 MobType
		slot                 string
		occupied, restricted bool
		want                 bool
	}{
		{name: "zombie saddle", kind: TypeZombie, slot: "saddle", want: true},
		{name: "copper golem antenna", kind: TypeCopperGolem, slot: "saddle", want: true},
		{name: "occupied saddle", kind: TypeZombie, slot: "saddle", occupied: true},
		{name: "occupied body", kind: TypeZombie, slot: "body", occupied: true},
		{name: "restricted head", kind: TypeZombie, slot: "head", restricted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(tc.kind, 1, 0, 64, 0, 1)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemDiamondHelmet), Count: 2}
			tag := nbt.Compound{"slot": nbt.String(tc.slot), "equip_on_interact": nbt.Byte(1)}
			if tc.restricted {
				tag["allowed_entities"] = nbt.String("minecraft:cat")
			}
			if !registry.ReadComponent(&stack, "minecraft:equippable", tag) {
				t.Fatal("equippable component rejected")
			}
			previous := protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1}
			if tc.occupied {
				if tc.slot == "saddle" {
					m.Horse.Saddle = previous
				} else {
					m.Body = previous
				}
			}
			_, equipped := m.EquipOnInteract(stack)
			if equipped != tc.want {
				t.Fatalf("equipped = %v, want %v", equipped, tc.want)
			}
			if tc.slot == "head" {
				return
			}
			got := m.Body
			if tc.slot == "saddle" {
				got = m.SaddleSlot()
			}
			want := previous
			if tc.want {
				want = stack
				want.Count = 1
			}
			if !got.Equal(want) {
				t.Errorf("slot = %+v, want %+v", got, want)
			}
			if tc.want {
				gotChance := m.BodyDropChance
				if tc.slot == "saddle" {
					gotChance = m.SaddleDropChance
				}
				if gotChance != guaranteedDropChance {
					t.Errorf("%s drop chance = %v, want %v", tc.slot, gotChance, guaranteedDropChance)
				}
			}
		})
	}
}

func TestSaddleAndBodyEquipGuaranteeDropChance(t *testing.T) {
	t.Parallel()
	saddle := protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
	horseArmor := protocol.ItemStack{ItemID: int32(registry.ItemIronHorseArmor), Count: 1}
	wolfArmor := protocol.ItemStack{ItemID: int32(registry.ItemWolfArmor), Count: 1}
	harness := protocol.ItemStack{ItemID: int32(registry.ItemBlueHarness), Count: 1}

	t.Run("dispenser saddle and body", func(t *testing.T) {
		h := NewMob(TypeHorse, 1, 0, 64, 0, 1)
		h.Horse.Tame = true
		if !h.DispenserEquip(saddle) || h.SaddleDropChance != guaranteedDropChance {
			t.Fatalf("horse dispenser saddle chance = %v, want %v", h.SaddleDropChance, guaranteedDropChance)
		}
		if !h.DispenserEquip(horseArmor) || h.BodyDropChance != guaranteedDropChance {
			t.Fatalf("horse dispenser body chance = %v, want %v", h.BodyDropChance, guaranteedDropChance)
		}
	})

	t.Run("horse clicks", func(t *testing.T) {
		h := NewMob(TypeHorse, 1, 0, 64, 0, 1)
		h.Horse.Tame = true
		if click, _ := h.UseItem(saddle, false); click != HorseClickSaddle || h.SaddleDropChance != guaranteedDropChance {
			t.Fatalf("horse click saddle: click=%v chance=%v", click, h.SaddleDropChance)
		}
		if click, _ := h.UseItem(horseArmor, false); click != HorseClickArmor || h.BodyDropChance != guaranteedDropChance {
			t.Fatalf("horse click armor: click=%v chance=%v", click, h.BodyDropChance)
		}
	})

	t.Run("wolf armor", func(t *testing.T) {
		w := NewMob(TypeWolf, 1, 0, 64, 0, 1)
		if !w.EquipWolfArmor(wolfArmor, true) || w.BodyDropChance != guaranteedDropChance {
			t.Fatalf("wolf armor chance = %v, want %v", w.BodyDropChance, guaranteedDropChance)
		}
	})

	t.Run("happy ghast harness", func(t *testing.T) {
		g := NewMob(TypeHappyGhast, 1, 0, 64, 0, 1)
		if !g.EquipHarness(harness) || g.BodyDropChance != guaranteedDropChance {
			t.Fatalf("happy ghast harness chance = %v, want %v", g.BodyDropChance, guaranteedDropChance)
		}
	})

	t.Run("sulfur cube absorb", func(t *testing.T) {
		c := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
		stone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
		if _, ok := c.AbsorbSulfurBlock(stone); !ok || c.BodyDropChance != guaranteedDropChance {
			t.Fatalf("sulfur cube body chance = %v, want %v", c.BodyDropChance, guaranteedDropChance)
		}
	})
}
