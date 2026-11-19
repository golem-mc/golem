package plugins_test

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// livingPlugin joins a survival player on a held loop; run executes f as a
// plugin task on the next tick.
func livingPlugin(t *testing.T) (*server.Server, *servertest.Client, plugin.Server, func(func())) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "living", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Victim")
	servertest.HoldTicks(t, srv)
	c.Settle()
	run := func(f func()) {
		t.Helper()
		ran := false
		host.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
	return srv, c, host, run
}

// callAll calls every method of v with zero arguments and reports the ones
// that panic; skip names methods that would end the session or store a nil
// callback.
func callAll(v any, skip ...string) (panics []string) {
	rv := reflect.ValueOf(v)
	for i := 0; i < rv.NumMethod(); i++ {
		name := rv.Type().Method(i).Name
		if slices.Contains(skip, name) {
			continue
		}
		m := rv.Method(i)
		args := make([]reflect.Value, m.Type().NumIn())
		for j := range args {
			args[j] = reflect.Zero(m.Type().In(j))
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					panics = append(panics, fmt.Sprintf("%s: %v", name, r))
				}
			}()
			if m.Type().IsVariadic() {
				m.CallSlice(args)
			} else {
				m.Call(args)
			}
		}()
	}
	return panics
}

// A Player handle answers every plugin.Player method without panicking, on a
// joined player and on one who left.
func TestPluginPlayerHandleEveryMethod(t *testing.T) {
	srv, c, host, run := livingPlugin(t)
	var p plugin.Player
	run(func() { p, _ = host.PlayerNamed("Victim") })
	skip := []string{"Kick", "Transfer", "RequestCookie"}
	run(func() {
		if got := callAll(p, skip...); len(got) > 0 {
			t.Errorf("joined player panics: %v", got)
		}
	})
	c.Disconnect()
	servertest.RunTicks(t, srv, 5)
	run(func() {
		if p.IsValid() {
			t.Error("a player who left is still valid")
		}
		if got := callAll(p, skip...); len(got) > 0 {
			t.Errorf("player who left panics: %v", got)
		}
	})
}

// vanilla: LivingEntity.setHealth, heal, hurtServer
func TestPluginPlayerHealthAndDamage(t *testing.T) {
	_, _, host, run := livingPlugin(t)
	run(func() {
		p, _ := host.PlayerNamed("Victim")
		if p.MaxHealth() != 20 || p.Health() != 20 {
			t.Fatalf("health %v/%v, want 20/20", p.Health(), p.MaxHealth())
		}
		p.SetHealth(8)
		if p.Health() != 8 {
			t.Errorf("SetHealth(8) left %v", p.Health())
		}
		p.Heal(4, plugin.RegainNatural)
		if p.Health() != 12 {
			t.Errorf("Heal(4) left %v, want 12", p.Health())
		}
		p.SetHealth(100)
		if p.Health() != 20 {
			t.Errorf("SetHealth(100) left %v, want clamped 20", p.Health())
		}
		if !p.Damage(5, plugin.DamageSource{Type: registry.DamageTypeGeneric}) || p.Health() != 15 {
			t.Errorf("Damage(5) left %v, want 15", p.Health())
		}
		p.SetInvulnerable(true)
		if !p.Invulnerable() || p.Damage(5, plugin.DamageSource{Type: registry.DamageTypeGeneric}) || p.Health() != 15 {
			t.Errorf("an invulnerable player took damage: health %v", p.Health())
		}
		p.Kill()
	})
}

// vanilla: LivingEntity.addEffect, removeEffect, removeAllEffects
func TestPluginPlayerEffects(t *testing.T) {
	_, _, host, run := livingPlugin(t)
	run(func() {
		p, _ := host.PlayerNamed("Victim")
		if !p.AddEffect(plugin.EffectInstance{Effect: registry.MobEffectSpeed, Duration: 600, Amplifier: 1, Particles: true, Icon: true}) {
			t.Fatal("AddEffect refused speed")
		}
		in, ok := p.Effect(registry.MobEffectSpeed)
		if !ok || in.Amplifier != 1 || in.Duration != 600 || len(p.Effects()) != 1 {
			t.Errorf("effect = %+v, %v; effects %v", in, ok, p.Effects())
		}
		if !p.RemoveEffect(registry.MobEffectSpeed) || len(p.Effects()) != 0 {
			t.Errorf("RemoveEffect left %v", p.Effects())
		}
		p.AddEffect(plugin.EffectInstance{Effect: registry.MobEffectInvisibility, Duration: 100})
		p.ClearEffects()
		if len(p.Effects()) != 0 {
			t.Errorf("ClearEffects left %v", p.Effects())
		}
	})
}

// vanilla: Entity.setDeltaMovement, setRemainingFireTicks, setTicksFrozen, setAirSupply
func TestPluginPlayerMotionFireAir(t *testing.T) {
	srv, c, host, run := livingPlugin(t)
	run(func() {
		p, _ := host.PlayerNamed("Victim")
		p.SetVelocity(plugin.Vec3{X: 0.25, Y: 0.5, Z: -0.125})
		if v := p.Velocity(); v.X != 0.25 || v.Y != 0.5 || v.Z != -0.125 {
			t.Errorf("velocity = %v", v)
		}
		p.SetFireTicks(100)
		p.SetFrozenTicks(40)
		p.SetAir(120)
		p.SetNoDamageTicks(7)
		p.SetAbsorption(4)
		if p.FireTicks() != 100 || p.FrozenTicks() != 40 || p.Air() != 120 || p.NoDamageTicks() != 7 || p.Absorption() != 4 {
			t.Errorf("fire %d frozen %d air %d nodamage %d absorption %v", p.FireTicks(), p.FrozenTicks(), p.Air(), p.NoDamageTicks(), p.Absorption())
		}
		p.SetFireTicks(0)
		if p.FireTicks() != 0 {
			t.Errorf("SetFireTicks(0) left %d", p.FireTicks())
		}
	})
	sawMotion := false
	c.ReadTicks(srv, 2, func(id int32, _ []byte) bool {
		sawMotion = sawMotion || id == protocol.IDPlaySetEntityMotion
		return false
	})
	if !sawMotion {
		t.Error("SetVelocity sent the client no motion packet")
	}
}

// Each carried Entity field reads back what a plugin set.
func TestPluginPlayerCarriedFlags(t *testing.T) {
	_, _, host, run := livingPlugin(t)
	run(func() {
		p, _ := host.PlayerNamed("Victim")
		name := plugin.Plain("Hero")
		p.SetCustomName(&name)
		p.SetGlowing(true)
		p.SetSilent(true)
		p.SetNoGravity(true)
		p.SetNameVisible(true)
		p.SetCollidable(false)
		if n := p.CustomName(); n == nil || n.PlainString() != "Hero" || !p.Glowing() || !p.Silent() || !p.NoGravity() || !p.NameVisible() || p.Collidable() {
			t.Errorf("flags did not read back: %v", n)
		}
		p.SetCustomName(nil)
		if p.CustomName() != nil {
			t.Error("SetCustomName(nil) kept the name")
		}
	})
}

// A player whose saved file carries Invulnerable refuses damage until a
// plugin clears the flag.
// vanilla: Entity.isInvulnerableToBase
func TestPluginPlayerSavedInvulnerableRefusesDamage(t *testing.T) {
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Victim"), nbt.Compound{
		"DataVersion":  nbt.Int(4000),
		"Invulnerable": nbt.Byte(1),
	})
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "living", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Victim")
	servertest.HoldTicks(t, srv)
	c.Settle()
	ran := false
	host.Scheduler().RunTask(func() {
		p, _ := host.PlayerNamed("Victim")
		src := plugin.DamageSource{Type: registry.DamageTypeGeneric}
		if !p.Invulnerable() || p.Damage(5, src) || p.Health() != 20 {
			t.Errorf("saved Invulnerable: flag %v, health %v", p.Invulnerable(), p.Health())
		}
		p.SetInvulnerable(false)
		if !p.Damage(5, src) || p.Health() != 15 {
			t.Errorf("cleared flag: health %v, want 15", p.Health())
		}
		ran = true
	})
	servertest.RunTicks(t, srv, 1)
	if !ran {
		t.Fatal("plugin task did not complete")
	}
}

// A plugin's Glowing, Silent, NoGravity and custom name on a player reach
// every client as metadata, are saved, and are read back after a rejoin.
// vanilla: Entity.DATA_SHARED_FLAGS_ID, DATA_CUSTOM_NAME, DATA_SILENT, DATA_NO_GRAVITY, Entity.saveWithoutId
func TestPluginPlayerEntityFlagsSynchAndSave(t *testing.T) {
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "flags", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Flagged")
	servertest.HoldTicks(t, srv)
	c.Settle()
	host.Scheduler().RunTask(func() {
		p, _ := host.PlayerNamed("Flagged")
		name := plugin.Plain("Hero")
		p.SetCustomName(&name)
		p.SetNameVisible(true)
		p.SetGlowing(true)
		p.SetSilent(true)
		p.SetNoGravity(true)
	})
	var meta []byte
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntityMetadata && servertest.MetadataEntityID(data) == c.PlayerEntityID {
			meta = append(meta, data...)
		}
		return false
	})
	// Each entry is its index, its type and a true boolean.
	for _, index := range []uint8{protocol.MetadataIndexCustomNameVisible, protocol.MetadataIndexSilent, protocol.MetadataIndexNoGravity} {
		if !bytes.Contains(meta, []byte{index, byte(protocol.MetadataBoolean), 1}) {
			t.Errorf("no true boolean entry %d in % X", index, meta)
		}
	}
	if !bytes.Contains(meta, []byte{protocol.MetadataIndexCustomName, byte(protocol.MetadataOptionalText), 1}) {
		t.Errorf("no custom name entry in % X", meta)
	}
	if !bytes.Contains(meta, []byte{protocol.MetadataIndexEntityFlags, byte(protocol.MetadataByte), protocol.EntityFlagGlowing}) {
		t.Errorf("no glowing flags entry in % X", meta)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Flagged"))
	for _, key := range []string{"CustomName", "CustomNameVisible", "Silent", "NoGravity", "Glowing"} {
		if root[key] == nil {
			t.Errorf("saved player lacks %s", key)
		}
	}
	c = servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Flagged")
	servertest.HoldTicks(t, srv)
	c.Settle()
	host.Scheduler().RunTask(func() {
		p, _ := host.PlayerNamed("Flagged")
		if n := p.CustomName(); n == nil || n.PlainString() != "Hero" || !p.NameVisible() || !p.Glowing() || !p.Silent() || !p.NoGravity() {
			t.Errorf("flags after rejoin: name %v", n)
		}
	})
	servertest.RunTicks(t, srv, 1)
}
