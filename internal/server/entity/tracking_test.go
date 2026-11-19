package entity

import "testing"

// The showing numbers are the vanilla EntityType ones, read out of the
// server jar's bytecode, and a version that moves them should fail here
// rather than quietly change who sees what from how far.
func TestTrackingNumbersMatchVanilla(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypePig, TypeSheep, TypeChicken} {
		if got := MobTrackingRange(kind); got != 10 {
			t.Errorf("%s tracking range = %d chunks, want the vanilla 10", kind, got)
		}
		if got := MobUpdateInterval(kind); got != 3 {
			t.Errorf("%s update interval = %d ticks, want the vanilla 3", kind, got)
		}
	}
	for kind, want := range map[MobType]int{TypeWolf: 10, TypeIronGolem: 10, TypeGlowSquid: 10, TypeSquid: 8, TypeSnowGolem: 8} {
		if got := MobTrackingRange(kind); got != want {
			t.Errorf("%s tracking range = %d chunks, want the vanilla %d", kind, got, want)
		}
		if got := MobUpdateInterval(kind); got != 3 {
			t.Errorf("%s update interval = %d ticks, want the vanilla 3", kind, got)
		}
	}
	// The zombie, whose EntityTypes.ZOMBIE builder carries its own range:
	// eight chunks of sight, the three ticks between updates being the
	// builder default it leaves alone.
	if got := MobTrackingRange(TypeZombie); got != 8 {
		t.Errorf("zombie tracking range = %d chunks, want the vanilla 8", got)
	}
	if got := MobUpdateInterval(TypeZombie); got != 3 {
		t.Errorf("zombie update interval = %d ticks, want the vanilla 3", got)
	}
	if got := ItemTrackingRange; got != 6 {
		t.Errorf("item tracking range = %d chunks, want the vanilla 6", got)
	}
	if got := ItemUpdateInterval; got != 20 {
		t.Errorf("item update interval = %d ticks, want the vanilla 20", got)
	}
	// The player's own registration, the peer of the furthest types:
	// the EntityTypes.PLAYER builder's clientTrackingRange(32).
	if got := PlayerTrackingRange; got != 32 {
		t.Errorf("player tracking range = %d chunks, want the vanilla 32", got)
	}
	if got := PlayerUpdateInterval; got != 2 {
		t.Errorf("player update interval = %d ticks, want the vanilla 2", got)
	}
	// A kind the table does not know falls back to the vanilla builder's
	// own defaults rather than to a zero-tick rhythm nothing could send on.
	if got := MobTrackingRange(NoMobType); got != 5 {
		t.Errorf("unknown kind's tracking range = %d chunks, want the builder default 5", got)
	}
	if got := MobUpdateInterval(NoMobType); got != 3 {
		t.Errorf("unknown kind's update interval = %d ticks, want the builder default 3", got)
	}
}

// TestSpectatableType checks the lookup /spectate reads a target's type
// through: only the marker, whose clientTrackingRange is zero, is refused.
func TestSpectatableType(t *testing.T) {
	for name, want := range map[string]bool{
		"minecraft:player": true,
		"minecraft:cow":    true,
		"minecraft:item":   true,
		"minecraft:marker": false,
	} {
		if got := SpectatableType(name); got != want {
			t.Errorf("SpectatableType(%s) = %v, want %v", name, got, want)
		}
	}
}
