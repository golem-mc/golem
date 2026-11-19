package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func TestZombifiedPiglinFirstAngerSoundDelay(t *testing.T) {
	for _, baby := range []bool{false, true} {
		for _, delay := range []int{0, 1, 20} {
			var seed int64
			for rand.New(rand.NewSource(seed)).Intn(21) != delay {
				seed++
			}
			m := NewMob(TypeZombifiedPiglin, 1, 0, 64, 0, 1)
			m.SetBaby(baby)
			m.SetAnger(100, uuid.Nil, false)
			m.TargetID = 2
			m.Rand = rand.New(rand.NewSource(seed))
			count := 0
			for tick := 1; tick <= 40; tick++ {
				view := &tickView{}
				m.piglinAlertStep(view)
				for _, ev := range view.events {
					if ev.Kind != MobSounded {
						continue
					}
					count++
					if tick != delay || ev.SoundID.Name() != "minecraft:entity.zombified_piglin.angry" || ev.Volume != 2 {
						t.Errorf("baby=%v delay=%d: tick %d sound %+v", baby, delay, tick, ev)
					}
					base := float32(1)
					if baby {
						base = 1.5
					}
					if ev.Pitch < (base-0.2)*1.8 || ev.Pitch > (base+0.2)*1.8 {
						t.Errorf("baby=%v: pitch %v outside its voice range times 1.8", baby, ev.Pitch)
					}
				}
			}
			want := 1
			if delay == 0 {
				want = 0
			}
			if count != want {
				t.Errorf("baby=%v delay=%d: heard %d sounds, want %d", baby, delay, count, want)
			}
		}
	}
}
