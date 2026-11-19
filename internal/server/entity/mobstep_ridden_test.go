package entity

import (
	"testing"

	"github.com/google/uuid"
)

func TestRiddenVehiclesUseTheirStepSounds(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want string
	}{
		{TypePig, "minecraft:entity.pig.step"},
		{TypeHorse, "minecraft:entity.horse.step_wood"},
		{TypeSkeletonHorse, "minecraft:entity.horse.step_wood"},
		{TypeZombieHorse, "minecraft:entity.horse.step_wood"},
		{TypeDonkey, "minecraft:entity.horse.step"},
		{TypeMule, "minecraft:entity.horse.step"},
		{TypeLlama, "minecraft:entity.llama.step"},
		{TypeTraderLlama, "minecraft:entity.llama.step"},
		{TypeCamel, "minecraft:entity.camel.step"},
		{TypeCamelHusk, "minecraft:entity.camel_husk.step"},
		{TypeStrider, "minecraft:entity.strider.step"},
		{TypeNautilus, ""},
		{TypeZombieNautilus, ""},
		{TypeHappyGhast, ""},
	} {
		t.Run(tc.kind.Name(), func(t *testing.T) {
			m, w := stepper(t, tc.kind, "minecraft:stone")
			m.Board(uuid.New())
			var heard []string
			for range 4 {
				from := m.X
				m.X += 0.5
				for _, ev := range m.RiddenStepSounds(w, from, m.Y, m.Z) {
					if ev.Kind == MobSounded {
						heard = append(heard, ev.SoundID.Name())
					}
				}
			}
			if tc.want == "" {
				if len(heard) != 0 {
					t.Fatalf("silent step override produced %v", heard)
				}
			} else if len(heard) != 1 || heard[0] != tc.want {
				t.Fatalf("heard %v, want one %s", heard, tc.want)
			}
		})
	}
}
