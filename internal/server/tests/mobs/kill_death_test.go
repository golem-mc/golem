package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LivingEntity.kill, LivingEntity.die, LivingEntity.tickDeath
func TestKillMobLootIsImmediateWhileFrozen(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Farmer",
		servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	id := srv.SpawnMob("minecraft:chicken", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	srv.SetEggTimer(id, 1)
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	srv.RunLine("tick freeze")

	watch := newDeathWatch(id)
	chicken := false
	var events []string
	c.ChatCommand("kill @e[type=minecraft:chicken]")
	c.SettleAndCollect(func(pid int32, data []byte) {
		watch.record(pid, data)
		switch pid {
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			events = append(events, "damage")
			if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", "minecraft:generic_kill"); got != want {
				t.Errorf("kill damage type = %d, want %d", got, want)
			}
			cause, direct, position := r.ReadVarInt(), r.ReadVarInt(), r.ReadBool()
			if cause != 0 || direct != 0 || position || r.Err() != nil || r.Remaining() != 0 {
				t.Errorf("kill damage source: cause=%d, direct=%d, position=%t, err=%v, remaining=%d", cause, direct, position, r.Err(), r.Remaining())
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusDeath {
				events = append(events, "death")
			}
		}
		if pid == protocol.IDPlayEntityMetadata {
			stack := servertest.ItemStackMetadata(data)
			if registry.Item(stack.ItemID) == registry.ItemChicken && stack.Count == 1 {
				chicken = true
			}
		}
	})
	if !slices.Equal(events, []string{"damage", "death"}) {
		t.Errorf("before any tick: events=%v, want generic-kill damage before death", events)
	}
	if watch.death != 0 || !chicken {
		t.Fatalf("before any tick: death event at %d, raw chicken visible=%t; want immediate death and loot", watch.death, chicken)
	}
	drops := len(watch.drops)
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		if item.Age != 0 {
			t.Errorf("frozen item %d aged to %d", item.ID, item.Age)
		}
	}

	c.ChatCommand("kill @e[type=minecraft:item]")
	c.Settle()
	srv.RunLine("tick unfreeze")
	watch.tick = 19
	servertest.RunTicks(t, srv, 19)
	c.SettleAndCollect(watch.record)
	if watch.remove >= 0 {
		t.Fatalf("corpse removed after %d ticks, want twenty", watch.remove)
	}
	watch.tick = 20
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(watch.record)
	if watch.remove != 20 || watch.poof != 20 {
		t.Errorf("corpse removal=%d, poof=%d; want twenty ticks after /kill", watch.remove, watch.poof)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 || len(watch.drops) != drops {
		t.Errorf("loot returned after item cleanup: items=%+v, spawn ticks=%v", items, watch.drops)
	}
}

// vanilla: LivingEntity.getKillCredit, LivingEntity.dropExperience
func TestKillMobPaysRememberedPlayerWhileFrozen(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Farmer",
		servertest.OpsListing(t, "Farmer"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.RunLine("tick freeze")
	srv.RunLine("scoreboard objectives add kills totalKillCount")
	c.ChatCommand("damage @e[type=minecraft:cow,limit=1] 1 minecraft:player_attack by Farmer")
	c.Settle()
	if health, ok := srv.MobHealth(id); !ok || health != 9 {
		t.Fatalf("cow's health after the player's hit = %v, present=%t; want 9", health, ok)
	}

	watch := newDeathWatch(id)
	c.ChatCommand("kill @e[type=minecraft:cow]")
	c.SettleAndCollect(watch.record)
	if watch.death != 0 || len(watch.orbs) == 0 {
		t.Errorf("before any tick: death event at %d, experience spawns=%v; want immediate death and experience", watch.death, watch.orbs)
	}
	if score, ok := srv.ScoreOf("Farmer", "kills"); !ok || score != 1 {
		t.Errorf("remembered player's kill score = %d, present=%t; want 1", score, ok)
	}
}
