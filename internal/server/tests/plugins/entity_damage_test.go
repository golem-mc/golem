package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// entityPlugin joins "Victim" to a server with one plugin and holds the loop.
func entityPlugin(t *testing.T, listen func(plugin.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "entity", Plugin: &testPlugin{enable: func(s plugin.Server) error { listen(s); return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Victim")
	servertest.HoldTicks(t, srv)
	c.Settle()
	return srv, c
}

// vanilla: LivingEntity.hurtServer, actuallyHurt
func TestPluginEntityDamageEvent(t *testing.T) {
	var seen []*plugin.EntityDamageEvent
	mode := "grow"
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			seen = append(seen, e)
			switch mode {
			case "grow":
				e.Amount = 4
			case "cancel":
				e.SetCancelled(true)
			}
		})
	})
	srv.SetHealth("Victim", 20)
	if !srv.Hurt("Victim", 2) {
		t.Fatal("hurt refused")
	}
	if e := seen[0]; e.Final != 2 || e.Source.Type != registry.DamageTypeMobAttack || e.Entity.UUID() != protocol.OfflineUUID("Victim") {
		t.Errorf("event = amount %v final %v type %v", e.Amount, e.Final, e.Source.Type)
	}
	if got := srv.PlayerHealth("Victim"); got != 16 {
		t.Errorf("health after a raised hit = %v, want 16", got)
	}
	mode = "cancel"
	srv.SetHealth("Victim", 20)
	if srv.Hurt("Victim", 5) {
		t.Error("cancelled hurt landed")
	}
	if got := srv.PlayerHealth("Victim"); got != 20 {
		t.Errorf("health after a cancelled hit = %v, want 20", got)
	}
	// Inside the hurt window the event carries only the excess.
	mode = "watch"
	srv.SetHealth("Victim", 20)
	srv.Hurt("Victim", 3)
	srv.Hurt("Victim", 5)
	if last := seen[len(seen)-1]; last.Amount != 2 {
		t.Errorf("window excess = %v, want 2", last.Amount)
	}
	if got := srv.PlayerHealth("Victim"); got != 15 {
		t.Errorf("health = %v, want 15", got)
	}
}

// vanilla: ServerPlayer.die, Player.dropEquipment
func TestPluginPlayerDeathEvent(t *testing.T) {
	var got *plugin.PlayerDeathEvent
	var edit func(*plugin.PlayerDeathEvent)
	srv, c := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerDeathEvent) {
			got = e
			edit(e)
		})
	})
	stick := protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1}
	kill := func() {
		srv.GiveSlot("Victim", 36, stick)
		srv.SetExperienceLevels("Victim", 10)
		srv.SetHealth("Victim", 5)
		got = nil
		if !srv.Hurt("Victim", 50) {
			t.Fatal("lethal hurt refused")
		}
		if got == nil {
			t.Fatal("no PlayerDeathEvent")
		}
	}
	respawn := func() {
		c.PerformRespawn()
		servertest.RunTicks(t, srv, 2)
		c.ExpectRespawnThroughTeleport()
		c.MarkLoaded()
	}

	// Edited drops and experience replace the defaults; no message means no
	// chat broadcast.
	items, orbs := srv.ItemCount(), srv.OrbCount()
	edit = func(e *plugin.PlayerDeathEvent) {
		if len(e.Drops) != 1 || e.Experience != 70 || e.Message == nil || e.Source.Type != registry.DamageTypeMobAttack ||
			e.Player.UUID() != protocol.OfflineUUID("Victim") || e.KeepInventory || e.KeepLevel {
			t.Errorf("event = %d drops, %d xp, message %v, type %v", len(e.Drops), e.Experience, e.Message, e.Source.Type)
		}
		e.Drops = append(e.Drops, e.Drops[0])
		e.Experience = 3
		e.Message = nil
	}
	kill()
	if !srv.InventorySlot("Victim", 36).Empty() {
		t.Error("inventory kept without KeepInventory")
	}
	if got := srv.ItemCount() - items; got != 2 {
		t.Errorf("dropped %d item entities, want the 2 the listener left", got)
	}
	if srv.OrbCount() == orbs {
		t.Error("no experience orbs dropped")
	}
	respawn()
	if lvl := srv.ExperienceLevels("Victim"); lvl != 0 {
		t.Errorf("level after respawn = %d, want 0", lvl)
	}

	// Keeping the inventory and level leaves both with the respawned body.
	items, orbs = srv.ItemCount(), srv.OrbCount()
	edit = func(e *plugin.PlayerDeathEvent) { e.KeepInventory, e.KeepLevel = true, true }
	kill()
	if srv.InventorySlot("Victim", 36).Empty() || srv.ItemCount() != items || srv.OrbCount() != orbs {
		t.Error("KeepInventory dropped something")
	}
	respawn()
	if lvl := srv.ExperienceLevels("Victim"); lvl != 10 {
		t.Errorf("level after KeepLevel respawn = %d, want 10", lvl)
	}
}

// vanilla: LivingEntity.die, dropAllDeathLoot
func TestPluginEntityDeathEvent(t *testing.T) {
	var got *plugin.EntityDeathEvent
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDeathEvent) {
			got = e
			if len(e.Drops) == 0 {
				t.Error("a cow's loot was not offered")
			}
			e.Drops, e.Experience = nil, 7
		})
	})
	id := srv.SpawnMob("minecraft:cow", 0.5, 64, 4.5)
	items, orbs := srv.ItemCount(), srv.OrbCount()
	srv.HurtMobByWorld(id, 100, "minecraft:generic")
	servertest.RunTicks(t, srv, 2)
	if got == nil {
		t.Fatal("no EntityDeathEvent")
	}
	if got.Entity.Type() != registry.EntityTypeCow || got.Source.Type != registry.DamageTypeGeneric {
		t.Errorf("event = %v, %v", got.Entity.Type(), got.Source.Type)
	}
	if srv.ItemCount() != items {
		t.Error("cleared drops still spawned")
	}
	if srv.OrbCount() == orbs {
		t.Error("the listener's experience spawned no orbs")
	}
}

// vanilla: LivingEntity.checkTotemDeathProtection
func TestPluginEntityResurrectEvent(t *testing.T) {
	var got *plugin.EntityResurrectEvent
	cancel := false
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityResurrectEvent) {
			got = e
			e.SetCancelled(cancel)
		})
	})
	totem := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:totem_of_undying"), Count: 1}
	srv.GiveSlot("Victim", player.HotbarSlot, totem)
	srv.SetHealth("Victim", 5)
	srv.Hurt("Victim", 50)
	if got == nil || got.Hand != plugin.MainHand || srv.PlayerHealth("Victim") != 1 {
		t.Fatalf("event %+v, health %v: want the totem to save at half a heart", got, srv.PlayerHealth("Victim"))
	}
	cancel = true
	srv.GiveSlot("Victim", player.HotbarSlot, totem)
	srv.SetHealth("Victim", 5)
	srv.Hurt("Victim", 50)
	if got := srv.PlayerHealth("Victim"); got != 0 {
		t.Errorf("health after a cancelled resurrection = %v, want death", got)
	}
}

// vanilla: FoodData.tick
func TestPluginFoodAndRegainEvents(t *testing.T) {
	var regain *plugin.EntityRegainHealthEvent
	var food *plugin.FoodLevelChangeEvent
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityRegainHealthEvent) { regain = e; e.Amount *= 2 })
		plugin.Listen(s, func(e *plugin.FoodLevelChangeEvent) { food = e; e.SetCancelled(true) })
	})
	srv.SetFood("Victim", 18)
	srv.SetSaturation("Victim", 0)
	srv.SetHealth("Victim", 10)
	servertest.RunTicks(t, srv, 90)
	if regain == nil || regain.Reason != plugin.RegainNatural || regain.Amount != 2 {
		t.Fatalf("regain = %+v, want a natural 1 point doubled to 2", regain)
	}
	if got := srv.PlayerHealth("Victim"); got != 12 {
		t.Errorf("health = %v, want 12", got)
	}
	if food == nil || food.Level != 17 || srv.PlayerFood("Victim") != 18 {
		t.Errorf("food event %+v, food %d: want the drop to 17 cancelled", food, srv.PlayerFood("Victim"))
	}
}
