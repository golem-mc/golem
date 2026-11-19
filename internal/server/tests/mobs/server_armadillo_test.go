package mobs

import (
	"math"
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

func TestAnArmadilloSpawnsAsItsOwnType(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(armadillo, 3.5, 64, 0.5)
	m := c.ExpectMobSpawn(armadilloType())
	if m.EntityID != id || m.Health != 12 {
		t.Fatalf("spawn = %d, health %v; want %d, 12", m.EntityID, m.Health, id)
	}
}

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

func TestBabyArmadillosShedOnlyAfterGrowingUp(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(armadillo, 3.5, 64, 0.5)
	c.ExpectMobSpawn(armadilloType())
	srv.SetMobBaby(id, true)
	srv.SetScuteTimer(id, 1)
	srv.TickMobs(2)
	assertArmadilloScutes(t, srv, 0)
	srv.SetMobBaby(id, false)
	srv.TickMobs(1)
	c.AwaitSoundNamed("minecraft:entity.armadillo.scute_drop")
	c.AwaitSpawn(protocol.EntityTypeItem)
	assertArmadilloScutes(t, srv, 1)
}

func TestArmadillosWaitForMobDropsToShed(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(armadillo, 3.5, 64, 0.5)
	c.ExpectMobSpawn(armadilloType())
	srv.SetScuteTimer(id, 1)
	srv.SetGameRule(world.RuleMobDrops.String(), false)
	srv.TickMobs(1)
	assertArmadilloScutes(t, srv, 0)
	srv.SetGameRule(world.RuleMobDrops.String(), true)
	srv.TickMobs(1)
	c.AwaitSoundNamed("minecraft:entity.armadillo.scute_drop")
	c.AwaitSpawn(protocol.EntityTypeItem)
	assertArmadilloScutes(t, srv, 1)
	srv.TickMobs(1)
	assertArmadilloScutes(t, srv, 1)
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

func TestTwoArmadillosFedSpiderEyesBreed(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:spider_eye", 2)
	servertest.HoldTicks(t, srv)
	first := srv.SpawnMob(armadillo, 1.5, 64, 2.5)
	second := srv.SpawnMob(armadillo, 2.5, 64, 2.5)
	c.ExpectMobSpawn(armadilloType())
	c.ExpectMobSpawn(armadilloType())
	c.UseOn(first)
	c.AwaitEntityEvent(first, loveHearts)
	c.UseOn(second)
	c.AwaitEntityEvent(second, loveHearts)
	servertest.UntilTicks(t, srv, 200, "armadillo baby", func() bool { return srv.MobCount() == 3 })
	baby := c.AwaitSpawn(armadilloType())
	if baby.EntityID == first || baby.EntityID == second {
		t.Fatal("baby reused a parent ID")
	}
	if flag := servertest.MetaOf(c.AwaitMetadataFor(baby.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Fatal("bred armadillo is not a baby")
	}
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Count; got != 0 {
		t.Fatalf("spider eyes left = %d, want 0", got)
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

func TestDispenserBrushesOnlyAdultArmadillos(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	c.ChatCommand("setblock 5 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	id := srv.SpawnMob(armadillo, 5.5, 64, 5.5)
	c.ExpectMobSpawn(armadilloType())
	if !srv.SetInventoryAt(5, 64, 4, 0, servertest.ItemStack(t, "minecraft:brush", 1)) {
		t.Fatal("missing dispenser")
	}
	srv.SetMobBaby(id, true)
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	assertArmadilloScutes(t, srv, 0)
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Empty() || got.Damage != 0 {
		t.Fatalf("baby brush=%+v", got)
	}
	srv.SetMobBaby(id, false)
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	assertArmadilloScutes(t, srv, 1)
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Empty() || got.Damage != 16 {
		t.Fatalf("adult brush=%+v", got)
	}
}

// TestAnArmadilloWillNotWalkThroughFireToATempter: a fire cell is
// impassable to an animal, so a row of fire across a walled corridor stops
// the armadillo short of the player holding its food.
// vanilla: Animal.<init> setPathfindingMalus FIRE -1, FIRE_IN_NEIGHBOR 16
func TestAnArmadilloWillNotWalkThroughFireToATempter(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:spider_eye", 1)
	servertest.HoldTicks(t, srv)
	stone := servertest.BlockState(t, "minecraft:stone")
	netherrack := servertest.BlockState(t, "minecraft:netherrack")
	fire := servertest.BlockState(t, "minecraft:fire")
	for x := int32(-3); x <= 11; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(x, y, z, 0)
			}
			srv.SetBlock(x, 63, z, netherrack)
			if x == -3 || x == 11 || z == -2 || z == 2 {
				for y := int32(64); y <= 66; y++ {
					srv.SetBlock(x, y, z, stone)
				}
			} else if x == 4 {
				srv.SetBlock(x, 64, z, fire)
			}
		}
	}
	c.MoveToGround(8.5, 64, 0.5, true)
	c.Settle()
	id := srv.SpawnMob(armadillo, 0.5, 64, 0.5)
	for tick := range 300 {
		servertest.RunTicks(t, srv, 1)
		x, _, _, _ := srv.MobPos(id)
		if math.Floor(x) >= 4 {
			t.Fatalf("tick %d: the armadillo reached x=%.2f, into the fire row", tick, x)
		}
		if f := srv.MobFireTicks(id); f != 0 {
			t.Fatalf("tick %d: the armadillo burns (%d fire ticks)", tick, f)
		}
	}
}
