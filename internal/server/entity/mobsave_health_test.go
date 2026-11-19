package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: LivingEntity.readAdditionalSaveData, setHealth.
func TestMobLoadsClampedHealth(t *testing.T) {
	t.Parallel()
	for _, kind := range []MobType{TypeZombie, TypeCow, TypeWolf, TypeSlime, TypeEnderDragon} {
		t.Run(kind.Name(), func(t *testing.T) {
			for _, tc := range []struct {
				health float32
				want   float64
			}{{-5, 0}, {0, 0}, {1, 1}} {
				m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
				m.LoadFrom(nbt.Compound{"Health": nbt.Float(tc.health)}, nil, func() int64 { return 0 })
				if m.Health != tc.want || m.Alive() != (tc.want > 0) {
					t.Errorf("Health:%vf: health=%v alive=%v, want %v/%v", tc.health, m.Health, m.Alive(), tc.want, tc.want > 0)
				}
				tag := nbt.Compound{}
				m.SaveTo(tag, func() int64 { return 0 })
				back := NewMob(kind, 2, 0.5, 64, 0.5, 1)
				back.LoadFrom(tag, nil, func() int64 { return 0 })
				if back.Health != tc.want {
					t.Errorf("reloaded Health:%vf = %v, want %v", tc.health, back.Health, tc.want)
				}
			}
		})
	}
}
