package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// mobDamage collects the mob EntityDamageEvents of one test. Its listener
// reads the entity, which deadlocks when a lock is still held around it.
type mobDamage struct {
	events []plugin.EntityDamageEvent
	cancel bool
	set    float32
	deaths int
	// during runs inside the listener, with the locks released.
	during func()
}

func mobDamageScene(t *testing.T) (*server.Server, *servertest.Client, *mobDamage) {
	t.Helper()
	d := &mobDamage{}
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDeathEvent) { d.deaths++ })
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			if e.Entity.Type() == registry.EntityTypePlayer {
				return
			}
			_ = e.Entity.(plugin.LivingEntity).Health()
			d.events = append(d.events, *e)
			if d.during != nil {
				d.during()
			}
			e.SetCancelled(d.cancel)
			if d.set != 0 {
				e.Amount = d.set
			}
		})
	})
	return srv, c, d
}

// tickScene joins a player to a survival server with one plugin and makes
// the test goroutine the tick goroutine, so a hurt it starts asks on-tick.
func tickScene(t *testing.T, listen func(plugin.Server), setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "tickscene", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		listen(s)
		return nil
	}}}}, setup...)
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Victim")
	c.Settle()
	srv.StopTick()
	srv.DriveTicksHere()
	return srv, c
}

// ticksUntil runs ticks here until cond holds.
func ticksUntil(t *testing.T, srv *server.Server, what string, cond func() bool) {
	t.Helper()
	for range 10000 {
		if cond() {
			return
		}
		srv.RunTick()
		// A connection goroutine's packet needs a moment between ticks.
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no %s within 10000 ticks", what)
}

func mobHealth(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	h, ok := srv.MobHealth(id)
	if !ok {
		t.Fatal("mob gone")
	}
	return h
}

// vanilla: Player.attack, LivingEntity.hurtServer
func TestPluginMobDamageMelee(t *testing.T) {
	t.Parallel()
	srv, c, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobNoAI(id)
	c.Attack(id)
	ticksUntil(t, srv, "the melee hurt", func() bool { return len(d.events) > 0 })
	if got := d.events[0]; got.Source.Type != registry.DamageTypePlayerAttack || got.Source.Causing == nil {
		t.Errorf("event = %v causing %v", got.Source.Type, got.Source.Causing)
	}
	// The connection goroutine lands the hurt once the listener is done.
	ticksUntil(t, srv, "the health loss", func() bool { return mobHealth(t, srv, id) < 10 })
}

// vanilla: ServerExplosion.hurtEntities
func TestPluginMobDamageExplosion(t *testing.T) {
	t.Parallel()
	srv, _, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	srv.RunExplosion(0.5, 64.5, 4.5, 3)
	if len(d.events) == 0 || d.events[0].Source.Type != registry.DamageTypeExplosion {
		t.Fatalf("events = %v", d.events)
	}
	if got := mobHealth(t, srv, id); got >= 10 {
		t.Errorf("health = %v", got)
	}
}

// A cancelled hurt leaves health and the hurt window as they were.
// vanilla: LivingEntity.hurtServer
func TestPluginMobDamageCancel(t *testing.T) {
	t.Parallel()
	srv, _, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	d.cancel = true
	srv.HurtMobByWorld(id, 4, "minecraft:magic")
	if got := mobHealth(t, srv, id); got != 10 {
		t.Fatalf("health after a cancelled hurt = %v", got)
	}
	d.cancel = false
	srv.HurtMobByWorld(id, 4, "minecraft:magic")
	if got := mobHealth(t, srv, id); got != 6 {
		t.Errorf("health after the retry = %v, want 6 (window untouched)", got)
	}
	d.set = -1
	srv.HurtMobByWorld(id, 9, "minecraft:magic")
	if len(d.events) != 3 || d.events[2].Amount != 5 {
		t.Errorf("the window's excess = %+v", d.events)
	}
}

// A player's fangs biting a mob name the player as cause and the fangs as the
// direct entity. vanilla: EvokerFangs.dealDamageTo, DamageSources.indirectMagic
func TestPluginMobDamagePlayerFangs(t *testing.T) {
	t.Parallel()
	var causing, direct registry.EntityType
	var distinct bool
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			if e.Entity.Type() == registry.EntityTypePlayer || e.Source.Causing == nil {
				return
			}
			causing = e.Source.Causing.Type()
			if e.Source.Direct != nil {
				direct = e.Source.Direct.Type()
				distinct = e.Source.Causing.UUID() != e.Source.Direct.UUID()
			}
		})
	})
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	srv.SetMobNoAI(id)
	tag := nbt.Stringify(nbt.Compound{"Owner": nbt.UUIDToIntArray(protocol.OfflineUUID("Victim"))})
	srv.RunLine("summon minecraft:evoker_fangs 0.5 64 4.5 " + tag)
	ticksUntil(t, srv, "the bite", func() bool { return causing != 0 })
	if causing != registry.EntityTypePlayer || direct != registry.EntityTypeEvokerFangs || !distinct {
		t.Errorf("source causing %v direct %v (distinct %v), want the player and the fangs", causing, direct, distinct)
	}
}
