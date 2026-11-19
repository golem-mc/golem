package nbt

import (
	"slices"
	"strings"
	"testing"
)

func TestHashOrderedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys string
		want string
	}{
		{
			name: "armor stand",
			keys: "NoGravity Brain Invulnerable FallFlying ShowArms PortalCooldown AbsorptionAmount DisabledSlots DeathTime Pose Invisible UUID Tags Motion Small Health fall_distance Air OnGround Rotation current_impulse_context_reset_grace_time Pos Fire NoBasePlate attributes HurtTime",
			// Recorded vanilla bucket order, with ties sorted because Compound
			// cannot preserve Invulnerable/FallFlying, PortalCooldown/AbsorptionAmount
			// or fall_distance/Air/OnGround insertion order.
			want: "NoGravity Brain FallFlying Invulnerable ShowArms AbsorptionAmount PortalCooldown DisabledSlots DeathTime Pose Invisible UUID Tags Motion Small Health Air OnGround fall_distance Rotation current_impulse_context_reset_grace_time Pos Fire NoBasePlate attributes HurtTime",
		},
		{
			name: "storage at capacity 16",
			keys: "big s t v f w em k l m bo n",
			// Vanilla's v/f and m/bo share buckets; sort those ties.
			want: "big s t f v w em k l bo m n",
		},
		{
			name: "storage resized to capacity 32",
			keys: "big s t v f w em k l m bo n a",
			want: "a big f em k l bo m n s t v w",
		},
		{
			name: "UTF-16 and unsigned spread",
			keys: "a b 😀 𐀀 é 漢 NoGravity",
			want: "NoGravity a b 漢 😀 é 𐀀",
		},
		{
			name: "collisions",
			keys: "BB Aa NoGravity",
			want: "Aa BB NoGravity",
		},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compound := Compound{}
			for key := range strings.FieldsSeq(tc.keys) {
				compound[key] = Int(1)
			}
			want := strings.Fields(tc.want)
			for range 10 {
				if got := HashOrderedKeys(compound); !slices.Equal(got, want) {
					t.Fatalf("keys = %v, want %v", got, want)
				}
			}
		})
	}
}
