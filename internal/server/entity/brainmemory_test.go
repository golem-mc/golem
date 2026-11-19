package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestFrogAndGoatCooldownsSurviveSaveAndLoad(t *testing.T) {
	t.Parallel()
	for _, typ := range []MobType{TypeFrog, TypeGoat} {
		m := NewMob(typ, 1, 0, 64, 0, 1)
		m.longJump().cooldown = 77
		if typ == TypeGoat {
			m.Goat.ram.cooldown = 55
		}
		saved := nbt.Compound{}
		m.SaveTo(saved, nil)
		brain, _ := nbt.Get[nbt.Compound](saved, "Brain")
		memories, _ := nbt.Get[nbt.Compound](brain, "memories")
		if brainTicks(memories, "minecraft:long_jump_cooling_down") != 77 {
			t.Fatalf("%v saved memories = %v", typ, memories)
		}
		back := NewMob(typ, 2, 0, 64, 0, 2)
		back.LoadFrom(saved, nil, nil)
		if back.longJump().cooldown != 77 || (typ == TypeGoat && back.Goat.ram.cooldown != 55) {
			t.Fatalf("%v cooldowns lost on reload", typ)
		}
		m.longJump().cooldown = 0
		m.Goat.ram.cooldown = 0
		saved = nbt.Compound{}
		m.SaveTo(saved, nil)
		brain, _ = nbt.Get[nbt.Compound](saved, "Brain")
		if memories, _ = nbt.Get[nbt.Compound](brain, "memories"); len(memories) != 0 {
			t.Fatalf("%v saved expired memories %v", typ, memories)
		}
	}
}
