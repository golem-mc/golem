package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestASplashPotionShatterSendsTheBreakSoundEvent checks that a shatter's
// particle event is followed by the glass-break event at the same block:
// 2007 then 1054 for an instant potion, 2002 then 1053 for a bottle o'
// enchanting. The client plays the sound only from 1053/1054.
// vanilla: AbstractThrownPotion.onHit, ThrownExperienceBottle.onHit
func TestASplashPotionShatterSendsTheBreakSoundEvent(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")
	cases := []struct {
		name          string
		throw         func()
		splash, sound int32
	}{
		{"healing", func() {
			srv.ThrowPotion(entity.ThrownSplashPotion, registry.MustPotion("minecraft:healing"), 6.5, 66, 0.5, 0, 90, 0, 1)
		}, protocol.WorldEventPotionSplashInstant, protocol.WorldEventInstantPotionBreakSound},
		{"experience bottle", func() {
			srv.ThrowThrown(entity.ThrownExperienceBottle, -5.5, 66, 0.5, 0, 90, 0, 1)
		}, protocol.WorldEventPotionSplash, protocol.WorldEventPotionBreakSound},
	}
	for _, tc := range cases {
		tc.throw()
		var seen []worldEffect
		c.ReadTicks(srv, potionDeadlineTicks, func(id int32, data []byte) bool {
			if id != protocol.IDPlayWorldEvent {
				return false
			}
			ev := worldEffectFor(data)
			if ev.event == tc.splash || ev.event == tc.sound {
				seen = append(seen, ev)
			}
			return ev.event == tc.sound
		})
		if len(seen) != 2 || seen[0].event != tc.splash || seen[1].event != tc.sound {
			t.Fatalf("%s: world events %+v, want %d then %d", tc.name, seen, tc.splash, tc.sound)
		}
		a, b := seen[0], seen[1]
		if a.x != b.x || a.y != b.y || a.z != b.z || b.data != 0 {
			t.Errorf("%s: sound event %+v, want data 0 at the splash's block %+v", tc.name, b, a)
		}
	}
}
