package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Slime.getJumpSound/getSquishSound, MagmaCube.getJumpSound/getSquishSound,
// SulfurCube.getJumpSound/getSquishSound
func TestCubeMovementSoundVariants(t *testing.T) {
	for _, tc := range []struct {
		kind                                 entity.MobType
		jump, smallJump, squish, smallSquish string
	}{
		{entity.TypeSlime, "slime.jump", "slime.jump_small", "slime.squish", "slime.squish_small"},
		{entity.TypeMagmaCube, "magma_cube.jump", "magma_cube.jump", "magma_cube.squish", "magma_cube.squish_small"},
		{entity.TypeSulfurCube, "sulfur_cube.jump", "small_sulfur_cube.jump", "sulfur_cube.squish", "small_sulfur_cube.squish"},
	} {
		for _, size := range []int{1, 2, 4} {
			m := entity.NewMob(tc.kind, 1, 0, 64, 0, 1)
			m.SetSize(size, true)
			jump, squish := tc.jump, tc.squish
			if size == 1 {
				jump, squish = tc.smallJump, tc.smallSquish
			}
			if got := cubeSound(m, true).Name(); got != "minecraft:entity."+jump {
				t.Errorf("%s size %d jump = %s", tc.kind, size, got)
			}
			if got := cubeSound(m, false).Name(); got != "minecraft:entity."+squish {
				t.Errorf("%s size %d squish = %s", tc.kind, size, got)
			}
		}
	}
}

func TestTypedMobVoicePreservesPacketAndRouting(t *testing.T) {
	for _, kind := range []entity.MobType{entity.TypeCow, entity.TypeZombie, entity.TypeRabbit} {
		m := entity.NewMob(kind, 1, -2.125, 64.25, 3.5, 1)
		if kind == entity.TypeRabbit {
			m.SetRabbitVariant(99)
		}
		named, ok := mobVoiceSend(nil, m, registeredSoundHolder(registry.SoundBlockStoneStep), 2, 0.8)
		typed, typedOK := mobVoiceIDSend(nil, m, registry.SoundBlockStoneStep, 2, 0.8)
		if !ok || !typedOK {
			t.Fatal("voice suppressed")
		}
		// Only the global random packet seed differs between consecutive sends.
		if !bytes.Equal(named.data[:len(named.data)-8], typed.data[:len(typed.data)-8]) ||
			named.id != typed.id || named.near != typed.near || named.radius != typed.radius || named.x != typed.x || named.y != typed.y || named.z != typed.z {
			t.Errorf("%s: typed voice changed packet or routing", kind)
		}
		m.Silent = true
		if _, ok := mobVoiceIDSend(nil, m, registry.SoundBlockStoneStep, 2, 0.8); ok {
			t.Fatal("silent voice sent")
		}
		m.Silent = false
		if _, ok := mobVoiceIDSend(nil, m, registry.SoundNone, 2, 0.8); ok {
			t.Fatal("absent voice sent")
		}
	}
}
