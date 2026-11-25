package mobs

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// awaitMobAttribute reads Update Attributes packets until one for the
// entity carries the attribute with a state done accepts.
func awaitMobAttribute(t *testing.T, c *servertest.Client, entityID, attribute int32, done func(servertest.AttrEntry) bool) {
	t.Helper()
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id != protocol.IDPlayUpdateAttributes {
			continue
		}
		got, entries := servertest.ReadAttributes(t, data)
		if e, ok := entries[attribute]; got == entityID && ok && done(e) {
			return
		}
	}
	t.Fatalf("no update attributes for entity %d with attribute %d", entityID, attribute)
}

// TestAttributeCommandEditsAMob sets a cow's base and adds a modifier, which
// its watchers are told of the next tick, and refuses an attribute the kind lacks;
// the saved list keeps what the commands changed.
func TestAttributeCommandEditsAMob(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:cow 1.5 64 0.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("attribute @e[type=minecraft:cow,limit=1] minecraft:movement_speed base set 0.5")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMovementSpeed, func(e servertest.AttrEntry) bool { return e.Base == 0.5 })

	c.ChatCommand("attribute @e[type=minecraft:cow,limit=1] minecraft:max_health modifier add minecraft:test 5 add_value")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMaxHealth, func(e servertest.AttrEntry) bool {
		return e.Base == 10 && len(e.Modifiers) == 1 && e.Modifiers[0].Amount == 5
	})

	// The base of an attribute the cow's supplier lacks is refused: no
	// packet follows.
	c.ChatCommand("attribute @e[type=minecraft:cow,limit=1] minecraft:attack_damage base set 4")

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	compound := servertest.SavedMobsByUUID(t, cfg.LevelName)[cow.Uuid]
	list, _ := nbt.Get[nbt.List](compound, "attributes")
	saved := map[string]nbt.Compound{}
	for _, item := range list.Items {
		entry := item.(nbt.Compound)
		id, _ := nbt.Get[nbt.String](entry, "id")
		saved[string(id)] = entry
	}
	if speed, ok := saved["minecraft:movement_speed"]; !ok || speed["base"] != nbt.Double(0.5) {
		t.Errorf("saved movement_speed = %v, want base 0.5", speed)
	}
	if health, ok := saved["minecraft:max_health"]; !ok {
		t.Error("saved attributes lack the max_health modifier")
	} else if mods, _ := nbt.Get[nbt.List](health, "modifiers"); len(mods.Items) != 1 {
		t.Errorf("saved max_health modifiers = %v, want the one the command added", mods.Items)
	}
	if _, ok := saved["minecraft:attack_damage"]; ok {
		t.Error("saved attributes hold attack_damage, which a cow does not carry")
	}
}

// TestAttributeSpeedDrivesTheChase has a zombie pace its movement_speed
// attribute: slowed to 0.02 it closes less than half the distance, and at
// 1.0 it is on the player within the same window. The pairings alone would
// leave the hunt running at the kind's own base.
func TestAttributeSpeedDrivesTheChase(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
	srv.SetDayTime(18000) // midnight keeps the daylight burn out of the hunt
	c := connect()
	c.JoinAs("Prey")
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Prey")

	srv.SpawnMob("minecraft:zombie", pos.X+8, pos.Y, pos.Z)
	z := c.ExpectMobSpawn(protocol.EntityTypeZombie)

	zombie := "@e[type=minecraft:zombie,limit=1]"
	c.ChatCommand("attribute " + zombie + " minecraft:movement_speed base set 0.02")
	c.SettleKeeping()
	servertest.RunTicks(t, srv, 1)
	awaitMobAttribute(t, c, z.EntityID, protocol.AttributeMovementSpeed,
		func(e servertest.AttrEntry) bool { return e.Base == 0.02 })
	c.PumpTicks(srv, 150, func(int32, []byte) bool { return false })
	x, _, _, ok := srv.MobPos(z.EntityID)
	if !ok {
		t.Fatal("the slowed zombie left the world")
	}
	if moved := math.Abs(x - (pos.X + 8)); moved > 4 {
		t.Errorf("a zombie slowed to 0.02 moved %v of 8 blocks in 150 ticks", moved)
	}

	c.ChatCommand("attribute " + zombie + " minecraft:movement_speed base set 1.0")
	c.SettleKeeping()
	servertest.RunTicks(t, srv, 1)
	awaitMobAttribute(t, c, z.EntityID, protocol.AttributeMovementSpeed,
		func(e servertest.AttrEntry) bool { return e.Base == 1 })
	c.PumpTicks(srv, 250, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetHealth {
			return false
		}
		// Keep the player alive so the hunt can go on.
		if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
			srv.SetHealth("Prey", 20)
		}
		return false
	})
	p := srv.PlayerPosition("Prey")
	zx, _, zz, ok := srv.MobPos(z.EntityID)
	if !ok {
		t.Fatal("the quickened zombie left the world")
	}
	if d := math.Hypot(zx-p.X, zz-p.Z); d > 3 {
		t.Errorf("the zombie quickened to 1.0 stands %v blocks away, on the far side of its hunt", d)
	}
}

// TestFrozenZombieListsMovementSpeed has a zombie summoned under a frozen
// loop list its default movement_speed before any tick.
// vanilla: Zombie.setBaby
func TestFrozenZombieListsMovementSpeed(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	srv.RunLine("tick freeze")

	c.ChatCommand("summon minecraft:zombie 1.5 64 0.5 {NoAI:1b}")
	c.Settle()
	c.ChatCommand(`data get entity @e[type=minecraft:zombie,limit=1] attributes[{id:"minecraft:movement_speed"}].base`)
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
		t.Fatalf("feedback = %+v, want commands.data.entity.query", line)
	}
	if got := line.With[1].Plain(); got != "0.23000000417232513d" {
		t.Errorf("movement_speed base = %s, want 0.23000000417232513d", got)
	}
}
