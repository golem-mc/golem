package mobs

import (
	"math"
	"testing"
	"time"

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

// TestDataMergeAttributesOnEditedMob merges an attributes list over a mob
// whose map a command already made; the save holds the merged base.
func TestDataMergeAttributesOnEditedMob(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:cow 1.5 64 0.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand("attribute @e[type=minecraft:cow,limit=1] minecraft:movement_speed base set 0.5")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMovementSpeed, func(e servertest.AttrEntry) bool { return e.Base == 0.5 })
	c.ChatCommand("data merge entity @e[type=minecraft:cow,limit=1] {attributes:[{id:\"minecraft:movement_speed\",base:0.3d}]}")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMovementSpeed, func(e servertest.AttrEntry) bool { return e.Base == 0.3 })

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	list, _ := nbt.Get[nbt.List](servertest.SavedMobsByUUID(t, cfg.LevelName)[cow.Uuid], "attributes")
	for _, item := range list.Items {
		entry := item.(nbt.Compound)
		if id, _ := nbt.Get[nbt.String](entry, "id"); id == "minecraft:movement_speed" {
			if entry["base"] != nbt.Double(0.3) {
				t.Errorf("saved movement_speed base = %v, want 0.3", entry["base"])
			}
			return
		}
	}
	t.Error("saved attributes lack movement_speed")
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

// TestAttributeMaxHealthTrimsAndRaisesTheHeal reads a cow's max_health
// attribute where its health is read: trimming the base clips the health
// on the spot, and raising it lets a heal climb past the kind's own ten.
func TestAttributeMaxHealthTrimsAndRaisesTheHeal(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:cow 1.5 64 0.5 {NoAI:1b,Health:10f}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	byID := "@e[type=minecraft:cow,limit=1]"

	c.ChatCommand("attribute " + byID + " minecraft:max_health base set 4")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMaxHealth,
		func(e servertest.AttrEntry) bool { return e.Base == 4 })
	if h, ok := srv.MobHealth(cow.EntityID); !ok || h != 4 {
		t.Errorf("trimming max_health to 4 left the cow at %v health, want the clip to 4", h)
	}

	c.ChatCommand("attribute " + byID + " minecraft:max_health base set 40")
	awaitMobAttribute(t, c, cow.EntityID, protocol.AttributeMaxHealth,
		func(e servertest.AttrEntry) bool { return e.Base == 40 })
	c.ChatCommand("effect give " + byID + " minecraft:instant_health 1 5 true")
	c.PumpTicks(srv, 10, func(int32, []byte) bool { return false })
	h, _ := srv.MobHealth(cow.EntityID)
	if h <= 10 {
		t.Errorf("the heal landed at %v under a 40 cap, want past the model's 10", h)
	}
}

// TestZombieTraitsSaveThroughItsMap edits a zombie's attribute so it gets a
// map, then checks the save holds the edit with the spawn bonus still on
// follow_range.
func TestZombieTraitsSaveThroughItsMap(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:zombie 1.5 64 0.5 {NoAI:1b,attributes:[{id:\"minecraft:follow_range\",base:35d,modifiers:[{id:\"minecraft:zombie_random_spawn_bonus\",amount:0.5d,operation:\"add_multiplied_total\"}]}]}")
	z := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:follow_range base set 20")
	c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:movement_speed base set 0.4")
	awaitMobAttribute(t, c, z.EntityID, protocol.AttributeMovementSpeed, func(e servertest.AttrEntry) bool { return e.Base == 0.4 })

	c.Disconnect()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	list, _ := nbt.Get[nbt.List](servertest.SavedMobsByUUID(t, cfg.LevelName)[z.Uuid], "attributes")
	for _, item := range list.Items {
		entry := item.(nbt.Compound)
		if id, _ := nbt.Get[nbt.String](entry, "id"); id == "minecraft:follow_range" {
			mods, _ := nbt.Get[nbt.List](entry, "modifiers")
			if entry["base"] != nbt.Double(20) || len(mods.Items) != 1 {
				t.Errorf("saved follow_range = %v, want base 20 and the spawn bonus", entry)
			}
			return
		}
	}
	t.Error("saved attributes lack follow_range")
}
