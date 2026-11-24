package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

func TestCurledArmadilloRejectsGoldenDandelion(t *testing.T) {
	t.Parallel()
	const playerName = "Rancher"
	srv, c := servertest.BreedingPlayer(t, "survival", playerName, "minecraft:golden_dandelion", 2,
		servertest.OpsListing(t, playerName))
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 2)
	if srv.AttackTicks(playerName) == 0 {
		t.Fatal("attack cooldown has not advanced")
	}
	c.ChatCommand(`summon minecraft:armadillo 1.5 64 2.5 {NoAI:1b,Age:-24000,state:"scared"}`)
	spawn := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	id := spawn.ReadVarInt()

	useFlower := func(state string, wantCount int32, wantLocked bool, wantSounds int) {
		t.Helper()
		before := srv.AttackTicks(playerName)
		c.UseOn(id)
		heard, _, _ := servertest.CollectSoundsSettled(c)
		held := srv.InventorySlot(playerName, player.HotbarSlot)
		if registry.Item(held.ItemID) != registry.ItemGoldenDandelion || held.Count != wantCount {
			t.Errorf("%s armadillo: held item %d × %d, want %d golden dandelions", state, held.ItemID, held.Count, wantCount)
		}
		tag, ok := srv.MobSaveTag(id)
		if !ok {
			t.Fatal("missing summoned armadillo")
		}
		if got, _ := nbt.Get[nbt.String](tag, "state"); string(got) != state {
			t.Fatalf("armadillo state = %q, want %q", got, state)
		}
		if got := nbt.BoolField(tag, "AgeLocked"); got != wantLocked {
			t.Errorf("%s armadillo: AgeLocked = %t, want %t", state, got, wantLocked)
		}
		if got := servertest.CountSounds(heard, "minecraft:item.golden_dandelion.use", protocol.SoundCategoryPlayer); got != wantSounds {
			t.Errorf("%s armadillo: golden dandelion use sounds = %d, want %d", state, got, wantSounds)
		}
		if wantLocked {
			before = 0
		}
		if got := srv.AttackTicks(playerName); got != before {
			t.Errorf("%s armadillo: attack cooldown ticks = %d, want %d", state, got, before)
		}
	}
	useFlower("scared", 2, false, 0)
	c.ChatCommand(`data merge entity @e[type=minecraft:armadillo,limit=1] {state:"idle"}`)
	c.Settle()
	useFlower("idle", 1, true, 1)
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
