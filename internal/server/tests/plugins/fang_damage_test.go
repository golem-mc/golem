package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EvokerFangs.dealDamageTo, DamageSources.indirectMagic
func TestPluginFangBiteNamesFangAndEvoker(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		victim string
		expire bool
	}{{"mob", false}, {"player", false}, {"mob", true}, {"player", true}} {
		victim := tc.victim
		if tc.expire {
			victim += " on the expiry tick"
		}
		var got *plugin.EntityDamageEvent
		var directID plugin.UUID
		var directType registry.EntityType = -2
		srv, c := entityPlugin(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
				if e.Source.Type == registry.DamageTypeIndirectMagic && got == nil {
					got = e
					if e.Source.Direct != nil {
						directID, directType = e.Source.Direct.UUID(), e.Source.Direct.Type()
					}
				}
			})
		})
		srv.SetHealth("Victim", 20)
		evoker := srv.SpawnMob("minecraft:evoker", 8.5, 64, 8.5)
		srv.SetMobNoAI(evoker)
		x, z := 0.5, 0.5
		if tc.victim == "mob" {
			z = 4.5
			srv.SpawnMob("minecraft:cow", x, 64, z)
			c.MoveToGround(2.5, 64, 12.5, true)
		} else {
			c.MoveToGround(x, 64, z, true)
		}
		if tc.expire {
			srv.SpawnExpiringFangs(x, 64, z, evoker)
		} else {
			srv.SpawnFangs(x, 64, z, evoker)
		}
		fangs := srv.FangsUUIDs()
		servertest.UntilTicks(t, srv, 8, "the bite's damage event", func() bool { return got != nil })
		if got == nil {
			t.Fatalf("%s victim: no indirect magic damage event", victim)
		}
		if len(fangs) != 1 || got.Source.Direct == nil || directID != plugin.UUID(fangs[0]) ||
			directType != registry.EntityTypeEvokerFangs {
			t.Errorf("%s victim: direct = %v type %v, want the fangs %v", victim, directID, directType, fangs)
		}
		uuidStr, _ := srv.MobUUID(evoker)
		if got.Source.Causing == nil || got.Source.Causing.UUID().String() != uuidStr ||
			got.Source.Causing.Type() != registry.EntityTypeEvoker {
			t.Errorf("%s victim: causing = %v, want the evoker %s", victim, got.Source.Causing, uuidStr)
		}
	}
}
