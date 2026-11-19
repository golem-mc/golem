package registry

import (
	"testing"
)

func TestIndexedFuelAndParticles(t *testing.T) {
	for _, tc := range []struct {
		item        int32
		burn, brews int32
	}{
		{int32(ItemOakPlanks), 300, 0}, {int32(ItemWoodenPickaxe), 200, 0},
		{int32(ItemLavaBucket), 20000, 0}, {int32(ItemBlazePowder), 0, 20},
		{int32(ItemGlassBottle), 0, 0}, {-1, 0, 0}, {ItemCount, 0, 0}, {1<<31 - 1, 0, 0},
	} {
		if burn, brews := FuelBurnTicks(tc.item), BrewingFuelUses(tc.item); burn != tc.burn || brews != tc.brews {
			t.Errorf("fuel %d = (%d, %d), want (%d, %d)", tc.item, burn, brews, tc.burn, tc.brews)
		}
	}
	for want, name := range ParticleTypes {
		id, data, ok := ParticleID(name)
		if !ok || id != int32(want) || data != particlesWithData[name] {
			t.Errorf("particle %s = %d, %v, %v", name, id, data, ok)
		}
	}
	if id, data, ok := ParticleID("test:missing"); id != 0 || data || ok {
		t.Fatalf("unknown particle = %d, %v, %v", id, data, ok)
	}
}
