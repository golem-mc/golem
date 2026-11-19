package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Cat.mobInteract, Wolf.mobInteract, AbstractNautilus.mobInteract, TamableAnimal.feed
func TestTameAnimalHealingUsesStackFood(t *testing.T) {
	t.Parallel()
	for _, animal := range []struct {
		kind             string
		entityType       int32
		item             registry.Item
		factor, fallback float64
	}{
		{"cat", protocol.EntityTypeCat, registry.ItemCod, 1, 1},
		{"wolf", protocol.EntityTypeWolf, registry.ItemBeef, 2, 2},
		{"nautilus", protocol.EntityTypeNautilus, registry.ItemCod, 2, 1},
	} {
		t.Run(animal.kind, func(t *testing.T) {
			t.Parallel()
			const name = "Keeper"
			srv, c := servertest.BreedingPlayer(t, "survival", name, "", 0, servertest.OpsListing(t, name))
			servertest.HoldTicks(t, srv)
			srv.SpawnMob("minecraft:"+animal.kind, 2.5, 64, 0.5)
			mob := c.ExpectMobSpawn(animal.entityType)
			srv.SetMobOwner(mob.EntityID, protocol.OfflineUUID(name))
			// A raised cap lets the cat receive all ten points of the patched cod.
			c.ChatCommand("attribute @e[type=minecraft:" + animal.kind + ",limit=1] minecraft:max_health base set 40")
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			for _, patch := range []string{"default", "nutrition ten", "removed food", "zero nutrition"} {
				t.Run(patch, func(t *testing.T) {
					stack := protocol.ItemStack{ItemID: int32(animal.item), Count: 2}
					food, _ := registry.StackFood(stack)
					heal := animal.factor * float64(food.Nutrition)
					switch patch {
					case "nutrition ten":
						stack.SetFood(protocol.FoodComponent{Nutrition: 10, Saturation: 1})
						heal = animal.factor * 10
					case "removed food":
						stack.Remove(protocol.DataComponentFood)
						heal = animal.fallback
					case "zero nutrition":
						stack.SetFood(protocol.FoodComponent{Nutrition: 0, Saturation: 1})
						heal = 0
					}
					srv.SetMobHealth(mob.EntityID, 4)
					srv.GiveSlot(name, player.HotbarSlot, stack)
					c.UseOn(mob.EntityID)
					c.Settle()
					if got, _ := srv.MobHealth(mob.EntityID); got != 4+heal {
						t.Errorf("health = %v, want 4 + %v", got, heal)
					}
					if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 1 {
						t.Errorf("food left = %d, want one item spent", got)
					}
					if m, _ := mobWithID(srv, mob.EntityID); m.InLove != 0 {
						t.Error("hurt animal entered love instead of healing")
					}
				})
			}
		})
	}
}

// vanilla: Fox.isConsumableFood, Fox.canHoldItem, Fox.aiStep
func TestFoxFoodUsesStackComponents(t *testing.T) {
	t.Parallel()
	for _, patch := range []string{"default", "removed food", "removed consumable", "added food", "added food and consumable"} {
		t.Run(patch, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, false)
			id := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 1)
			feather := protocol.ItemStack{ItemID: int32(registry.ItemFeather), Count: 1}
			srv.SetMobHand(id, feather)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemSweetBerries), Count: 1}
			edible := patch == "default" || patch == "added food and consumable"
			switch patch {
			case "removed food":
				stack.Remove(protocol.DataComponentFood)
			case "removed consumable":
				stack.Remove(protocol.DataComponentConsumable)
			case "added food", "added food and consumable":
				stack.ItemID = int32(registry.ItemStick)
				stack.SetFood(protocol.FoodComponent{Nutrition: 10, Saturation: 1})
				if edible {
					stack.SetConsumable(protocol.ConsumableComponent{
						Seconds: 1.6, Animation: 1, HasParticles: true,
						Sound: protocol.SoundHolder{Name: "minecraft:entity.generic.eat"},
					})
				}
			}
			servertest.RunTicks(t, srv, 1) // A full mouth only swaps after TicksSinceEaten > 0.
			drop := srv.SpawnItemStack(foxX, 64, 0.5, stack)
			servertest.RunTicks(t, srv, 5)
			want := feather
			if edible {
				want = stack
			}
			if got := srv.MobHand(id); !got.Equal(want) {
				t.Errorf("mouth after pickup = %+v, want %+v", got, want)
			}
			srv.DiscardItem(drop)
			srv.SetMobHand(id, stack)
			servertest.RunTicks(t, srv, 600)
			got := srv.MobHand(id)
			if edible {
				if !got.Empty() {
					t.Errorf("meal was not eaten: %+v", got)
				}
			} else if !got.Equal(stack) {
				t.Errorf("inedible mouthful changed: %+v, want %+v", got, stack)
			}
		})
	}
}
