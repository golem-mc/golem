package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EvokerFangs.dealDamageTo, DamageSources.indirectMagic
func TestPluginFangBiteNamesFangAndEvoker(t *testing.T) {
	t.Parallel()
	for _, expire := range []bool{false, true} {
		name := "bite"
		if expire {
			name = "bite on the expiry tick"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			type seen struct {
				source plugin.DamageSource
				valid  bool
			}
			got := map[registry.EntityType]seen{}
			srv, c := entityPlugin(t, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
					if e.Source.Type == registry.DamageTypeIndirectMagic && e.Source.Direct != nil {
						got[e.Entity.Type()] = seen{e.Source, e.Source.Direct.IsValid()}
					}
				})
			})
			srv.SetHealth("Victim", 20)
			evoker := srv.SpawnMob("minecraft:evoker", 8.5, 64, 8.5)
			srv.SetMobNoAI(evoker)
			srv.SetMobNoAI(srv.SpawnMob("minecraft:cow", 0.5, 64, 0.8))
			c.MoveToGround(0.5, 64, 0.5, true)
			if expire {
				srv.SpawnExpiringFangs(0.5, 64, 0.5, evoker)
			} else {
				srv.SpawnFangs(0.5, 64, 0.5, evoker)
			}
			var fangs plugin.UUID
			// A zero-warmup fang bites on its eighth tick.
			c.ReadTicks(srv, 8, func(pid int32, data []byte) bool {
				if pid == protocol.IDPlaySpawnEntity {
					if kind, e, _, _ := servertest.ReadSpawnEntity(t, data); kind == int32(registry.EntityTypeEvokerFangs) {
						fangs = plugin.UUID(e.Uuid)
					}
				}
				return false
			})
			evokerID, _ := srv.MobUUID(evoker)
			for _, victim := range []registry.EntityType{registry.EntityTypeCow, registry.EntityTypePlayer} {
				ev, ok := got[victim]
				if !ok {
					t.Errorf("%v victim: no indirect magic damage event", victim)
					continue
				}
				d := ev.source.Direct
				if d.UUID() != fangs || d.Type() != registry.EntityTypeEvokerFangs {
					t.Errorf("%v victim: direct = %v type %v, want the fangs %v", victim, d.UUID(), d.Type(), fangs)
				}
				if ev.valid == expire {
					t.Errorf("%v victim: direct valid = %v, want %v", victim, ev.valid, !expire)
				}
				if ca := ev.source.Causing; ca == nil || ca.UUID().String() != evokerID || ca.Type() != registry.EntityTypeEvoker {
					t.Errorf("%v victim: causing = %v, want the evoker %s", victim, ca, evokerID)
				}
			}
		})
	}
}
