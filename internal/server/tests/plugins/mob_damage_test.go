package plugins_test

import (
	"math"
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
	for range 1000 {
		if cond() {
			return
		}
		srv.RunTick()
		// A connection goroutine's packet needs a moment between ticks.
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no %s within 1000 ticks", what)
}

func mobHealth(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	h, ok := srv.MobHealth(id)
	if !ok {
		t.Fatal("mob gone")
	}
	return h
}

// vanilla: LivingEntity.hurtServer, Mob.tick burning
func TestPluginMobDamageFire(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:husk", 0.5, 64, 4.5)
	srv.SetMobFire(id, 100)
	ticksUntil(t, srv, "a burn hurt", func() bool { return len(d.events) > 0 })
	if e := d.events[0]; e.Source.Type != registry.DamageTypeOnFire || e.Entity.UUID().String() == "" {
		t.Errorf("event = %v", e.Source.Type)
	}
}

// vanilla: Player.attack, LivingEntity.hurtServer
func TestPluginMobDamageMelee(t *testing.T) {
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

// vanilla: AbstractArrow.onHitEntity
func TestPluginMobDamageArrow(t *testing.T) {
	srv, c, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeIronGolem)
	d.cancel = true
	srv.ShootArrow(10.5, 65, 4.5, -2, 0, 0)
	ticksUntil(t, srv, "the arrow's hurt", func() bool { return len(d.events) > 0 })
	if got := d.events[0].Source.Type; got != registry.DamageTypeArrow {
		t.Errorf("type = %v", got)
	}
	if got := mobHealth(t, srv, id); got != 100 {
		t.Errorf("a cancelled arrow took health: %v", got)
	}
}

// vanilla: ServerExplosion.hurtEntities
func TestPluginMobDamageExplosion(t *testing.T) {
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

// vanilla: DamageCommand.damage
func TestPluginMobDamageCommand(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	d.set = 3
	srv.RunLine("damage @e[type=minecraft:cow,limit=1,sort=nearest] 8 minecraft:magic")
	if len(d.events) != 1 || d.events[0].Amount != 8 || d.events[0].Source.Type != registry.DamageTypeMagic {
		t.Fatalf("events = %+v", d.events)
	}
	if got := mobHealth(t, srv, id); got != 7 {
		t.Errorf("an edited amount: health = %v, want 7", got)
	}
}

// A cancelled hurt leaves health and the hurt window as they were.
// vanilla: LivingEntity.hurtServer
func TestPluginMobDamageCancel(t *testing.T) {
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

// Final is the health an armoured hit takes.
// vanilla: LivingEntity.getDamageAfterArmorAbsorb, getDamageAfterMagicAbsorb
func TestPluginMobDamageFinalWithArmour(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	id := srv.SpawnMob("minecraft:husk", 0.5, 64, 4.5)
	for slot, item := range []registry.Item{registry.ItemIronBoots, registry.ItemIronLeggings, registry.ItemIronChestplate, registry.ItemIronHelmet} {
		srv.SetMobWorn(id, slot, protocol.ItemStack{ItemID: int32(item), Count: 1})
	}
	before := mobHealth(t, srv, id)
	srv.RunLine("damage @e[type=minecraft:husk,limit=1,sort=nearest] 10 minecraft:mob_attack")
	if len(d.events) != 1 {
		t.Fatalf("events = %d", len(d.events))
	}
	lost := before - mobHealth(t, srv, id)
	if lost <= 0 || lost >= 10 || math.Abs(float64(d.events[0].Final)-lost) > 1e-4 {
		t.Errorf("Final = %v, lost = %v", d.events[0].Final, lost)
	}
}

// vanilla: LivingEntity.die
func TestPluginMobDamageLethalStillDies(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	srv.RunLine("damage @e[type=minecraft:cow,limit=1,sort=nearest] 100 minecraft:magic")
	srv.RunTick()
	srv.RunTick()
	if len(d.events) != 1 || d.deaths != 1 {
		t.Fatalf("damage events = %d, deaths = %d", len(d.events), d.deaths)
	}
}

// A connection goroutine spawning and killing mobs while a listener is
// blocked in the ask leaves the explosion's remaining hurts firing.
// vanilla: ServerExplosion.hurtEntities
func TestPluginMobDamageHerdChangesDuringAsk(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	ids := []int32{
		srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5),
		srv.SpawnMob("minecraft:cow", 1.5, 64, 4.5),
		srv.SpawnMob("minecraft:cow", 2.5, 64, 4.5),
	}
	done := make(chan struct{})
	started := false
	d.during = func() {
		if started {
			return
		}
		started = true
		go func() {
			defer close(done)
			for i := range 5 {
				srv.SpawnMob("minecraft:cow", 3.5+float64(i), 64, 4.5)
			}
			// The kill's own hurt asks the tick goroutine, which is busy
			// in this listener until the explosion returns.
			srv.KillMob(ids[1])
		}()
		time.Sleep(50 * time.Millisecond)
	}
	srv.RunExplosion(1.5, 64.5, 4.5, 3)
	ticksUntil(t, srv, "the killer goroutine", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if len(d.events) == 0 {
		t.Fatal("no damage event")
	}
}

// A creeper's blast hurts a mob with the mob tick holding mobsMu; resolving
// the event's source must not take it again.
func TestPluginMobDamageCreeperBlastNoDeadlock(t *testing.T) {
	srv, _, d := mobDamageScene(t)
	srv.SpawnMob("minecraft:pig", 3.5, 64, 0.5)
	srv.SpawnMobTagged("minecraft:creeper", 4.5, 64, 0.5, nbt.Compound{
		"ignited": nbt.Byte(1), "Fuse": nbt.Short(1), "NoAI": nbt.Byte(1),
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			srv.RunTick()
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tick loop deadlocked")
	}
	if len(d.events) == 0 {
		t.Fatal("no EntityDamageEvent for the blast")
	}
}
