package server

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func TestMobDamageSelectionDoesNotAllocate(t *testing.T) {
	for _, kind := range []entity.MobType{entity.TypeCow, entity.TypeWolf, entity.TypeGiant, entity.TypeMooshroom} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := entity.NewMob(kind, 1, 0, 64, 0, 17)
			if allocs := testing.AllocsPerRun(100, func() {
				_ = mobSound(m, true)
				_ = mobSound(m, false)
			}); allocs != 0 {
				t.Fatalf("hurt/death selection allocated %g times per pair", allocs)
			}
		})
	}
}

func TestMobDamageHoldersRetainKindDefaults(t *testing.T) {
	for kind := range registry.EntityTypeCount {
		kind := entity.MobType(kind)
		if _, ok := entity.MobName(kind); !ok {
			continue
		}
		t.Run(kind.Name(), func(t *testing.T) {
			m := entity.NewMob(kind, 1, -1.25, 64.5, 2.25, 17)
			for _, hurt := range []bool{true, false} {
				event, suffix := m.Sounds().Death, "death"
				if hurt {
					event, suffix = m.Sounds().Hurt, "hurt"
				}
				name := event.Name()
				if name == "" {
					name = fmt.Sprintf("minecraft:entity.%s.%s", strings.TrimPrefix(kind.Name(), "minecraft:"), suffix)
				}
				named := soundEffectAt(name, mobSoundCategory(kind), m.X, m.Y, m.Z, m.SoundVolume(), 1)
				resolved := soundEffectHolderAt(mobSound(m, hurt), mobSoundCategory(kind), m.X, m.Y, m.Z, m.SoundVolume(), 1)
				if !bytes.Equal(named[:len(named)-8], resolved[:len(resolved)-8]) {
					t.Fatalf("%s holder changed", name)
				}
			}
		})
	}
}

func TestResolvedSoundEffectRetainsHolderEncoding(t *testing.T) {
	for _, name := range []string{"minecraft:entity.cow.hurt", "minecraft:entity.mooshroom.hurt", ""} {
		sound := protocol.SoundHolder{ID: -1, Name: name}
		if id, ok := registry.SoundEventID(name); ok {
			sound = protocol.SoundHolder{ID: id}
		}
		named := soundEffectAt(name, protocol.SoundCategoryNeutral, -1.25, 64.5, 2.25, 0.4, 1.2)
		resolved := soundEffectHolderAt(sound, protocol.SoundCategoryNeutral, -1.25, 64.5, 2.25, 0.4, 1.2)
		if !bytes.Equal(named[:len(named)-8], resolved[:len(resolved)-8]) {
			t.Fatalf("%q holder changed", name)
		}
	}
	// A direct holder retains its form even when its identifier is registered.
	payload := soundEffectHolderAt(protocol.SoundHolder{Name: "minecraft:entity.cow.hurt", Range: 32, HasRange: true}, protocol.SoundCategoryNeutral, 0, 64, 0, 0.4, 1)
	r := protocol.NewReader(payload)
	if r.ReadVarInt() != 0 || r.ReadString(32767) != "minecraft:entity.cow.hurt" || !r.ReadBool() || r.ReadF32() != 32 || r.Err() != nil {
		t.Fatal("direct holder or fixed range changed")
	}
}

func TestInteractionSoundSelectionDoesNotAllocate(t *testing.T) {
	if allocs := testing.AllocsPerRun(100, func() {
		_, _ = copperChestHingeSounds(registry.BlockWeatheredCopperChest)
		_ = rules.TridentSound(protocol.ItemStack{})
	}); allocs != 0 {
		t.Fatalf("hinge/riptide selection allocated %g times", allocs)
	}
}
