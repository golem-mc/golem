package entity

import "testing"

// TestACamelHuskStepsQuieterThanACamel: the camel husk steps at 0.4, a camel at 1.
func TestACamelHuskStepsQuieterThanACamel(t *testing.T) {
	for _, c := range []struct {
		typ  MobType
		want float32
	}{{TypeCamel, 1}, {TypeCamelHusk, 0.4}} {
		m := NewMob(c.typ, 1, 0.5, 64, 0.5, 1)
		if _, vol, _, ok := m.stepVoice(nil, 0, 0, 0, 0); !ok || vol != c.want {
			t.Errorf("%v: volume %v ok %v, want %v", c.typ, vol, ok, c.want)
		}
	}
}
