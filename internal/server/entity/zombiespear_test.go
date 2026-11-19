package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The mount's displacement supplies speed; turning in the seat cannot create it.
// vanilla: KineticWeapon.getMotion, damageEntities; ProjectileUtil.getHitEntitiesAlong
func TestMountedSpearContactUsesTheMountsSpeed(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		stride, targetStride float64
		yaw                  float32
		delay, wall, miss    bool
		want                 bool
	}{
		{name: "charging", stride: 0.3, want: true},
		{name: "stationary mount"},
		{name: "below relative speed threshold", stride: 0.04},
		{name: "target outruns charge", stride: 0.3, targetStride: 0.4},
		{name: "body faces away from motion", stride: 0.3, yaw: 180},
		{name: "before weapon delay", stride: 0.3, delay: true},
		{name: "blocked contact", stride: 0.3, wall: true},
		{name: "outside contact ray", stride: 0.3, miss: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := spearWorld(t)
			camel := NewMob(TypeCamelHusk, 1, 0.5, 64, 0.5, 1)
			camel.NoAI = true
			camel.Tick(w, nil)
			camel.Z += tc.stride
			camel.Tick(w, nil)
			husk := NewMob(TypeHusk, 2, 0.5, 64, 0.5, 2)
			Ride(camel, husk)
			husk.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemIronSpear), Count: 1}
			husk.Yaw = tc.yaw
			// A horizontal head ray at the rider's height isolates speed from aim.
			p := Player{EntityID: 7, X: husk.X, Y: husk.Y, Z: husk.Z + 2, Huntable: true, KnownVZ: tc.targetStride}
			if tc.miss {
				p.X += 2
			}
			if tc.wall {
				w.set(0, int32(husk.Y+husk.eyeHeight()), int32(husk.Z+1), defaultState(t, "minecraft:stone"))
			}
			k := registry.StackKineticWeapon(husk.MainHand)
			used := int(k.Delay)
			if tc.delay {
				used--
			}
			view := tickView{w: w, players: []Player{p}, others: []*Mob{camel, husk}}
			var state spearState
			husk.stab(&view, &state, k, used)
			var damaged bool
			for _, ev := range view.events {
				if ev.Kind == MobAttackedPlayer && ev.Player.EntityID == p.EntityID && ev.Stab.DealsDamage {
					damaged = true
					// floor(6 blocks/s * iron's 0.95) + the husk's base 3.
					if ev.Damage != 8 {
						t.Errorf("damage = %v, want 8", ev.Damage)
					}
				}
			}
			if damaged != tc.want {
				t.Fatalf("damaging contact = %v, want %v (events %+v)", damaged, tc.want, view.events)
			}
			view.events = nil
			husk.Age += int(k.ContactCooldown) - 1
			husk.stab(&view, &state, k, used+1)
			if tc.want && len(view.events) != 0 {
				t.Fatal("the same contact hit again before its cooldown")
			}
			if tc.want {
				husk.Age++
				husk.stab(&view, &state, k, used+int(k.ContactCooldown))
				if len(view.events) != 1 || !view.events[0].Stab.DealsDamage {
					t.Fatal("contact did not hurt again after its cooldown")
				}
			}
		})
	}
}
