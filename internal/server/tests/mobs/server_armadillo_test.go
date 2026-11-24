package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const armadillo = "minecraft:armadillo"

func armadilloType() int32 { id, _ := registry.EntityTypeID(armadillo); return int32(id) }

func assertArmadilloScutes(t *testing.T, srv *server.Server, want int) {
	t.Helper()
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != want {
		t.Fatalf("drops = %v, want %d scute entities", items, want)
	}
	for _, it := range items {
		if it.Item != "minecraft:armadillo_scute" || it.Count != 1 {
			t.Fatalf("drop = %v, want one scute", it)
		}
	}
}

func TestBrushingAnAdultArmadilloCostsSixteenDurability(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:brush", 1)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(armadillo, 1.5, 64, 2.5)
	c.ExpectMobSpawn(armadilloType())
	srv.SetMobBaby(id, true)
	c.UseOn(id)
	c.Settle()
	assertArmadilloScutes(t, srv, 0)
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Damage; got != 0 {
		t.Fatalf("baby brush damage = %d", got)
	}
	srv.SetMobBaby(id, false)
	c.UseOn(id)
	c.Settle()
	assertArmadilloScutes(t, srv, 1)
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Damage; got != 16 {
		t.Fatalf("adult brush damage = %d, want 16", got)
	}
}

func TestArmadilloRollMetadataAndReducedDamage(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(armadillo, 3.5, 64, 0.5)
	c.ExpectMobSpawn(armadilloType())
	// The zombie starts outside the scare box and walks toward the player.
	srv.SpawnMob("minecraft:zombie", 12.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("data merge entity @e[type=minecraft:zombie,limit=1] {PersistenceRequired:1b}")
	c.Settle()
	next := int32(1)
	for range 80 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid != protocol.IDPlayEntityMetadata {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			entries, ok := servertest.ReadMetadata(r)
			state := servertest.MetaOf(entries, protocol.MetadataIndexArmadilloState)
			if ok && state != nil && state.VarintVal == next {
				next++
			}
		})
		if next == 3 {
			break
		}
	}
	if next != 3 {
		t.Fatalf("next expected state = %d; want rolling then scared metadata", next)
	}
	c.ChatCommand("damage @e[type=minecraft:armadillo,limit=1] 5 minecraft:generic")
	c.Settle()
	if hp, _ := srv.MobHealth(id); hp != 10 {
		t.Fatalf("rolled health=%v, want 10", hp)
	}
}

func TestSummonedScaredArmadilloSpawnsWithScaredState(t *testing.T) {
	t.Parallel()
	c, _, _ := polarWorld(t)
	c.ChatCommand(`summon minecraft:armadillo 3.5 64 0.5 {NoAI:1b,Age:-24000,state:"scared"}`)
	mob := c.ExpectMobSpawn(armadilloType())
	if !mob.HasArmadilloState || mob.ArmadilloState != 2 {
		t.Fatalf("armadillo state = %d (present %v), want 2 (scared)", mob.ArmadilloState, mob.HasArmadilloState)
	}
}
