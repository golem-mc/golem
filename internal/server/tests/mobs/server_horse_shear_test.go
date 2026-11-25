package mobs

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// saddledHorse is a still tame horse beside a survival player, saddled by a
// click, with the player holding shears.
func saddledHorse(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := wolfScene(t, "Rider")
	id := srv.SpawnMob("minecraft:horse", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameHorse(id)
	giveHand(t, srv, "Rider", "minecraft:saddle", 1)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 2)
	if srv.MobEquipment(id, "saddle").Empty() {
		t.Fatal("the horse took no saddle")
	}
	giveHand(t, srv, "Rider", "minecraft:shears", 1)
	return srv, c, id
}

// TestShearsTakeTheSaddleOffAHorse: the saddle drops, the shears wear a
// point and the unequip sound plays.
// vanilla: Mob.attemptToShearEquipment
func TestShearsTakeTheSaddleOffAHorse(t *testing.T) {
	t.Parallel()
	srv, c, id := saddledHorse(t)
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if got := srv.MobEquipment(id, "saddle"); !got.Empty() {
		t.Fatalf("saddle still on after shearing: %v", got)
	}
	if !seen.heard("minecraft:item.saddle.unequip") {
		t.Errorf("no unequip sound in %v", seen.sounds)
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || it.Item == "minecraft:saddle"
	}
	if !found {
		t.Error("the sheared saddle did not drop")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("shears damage = %d, want 1", got.Damage)
	}
}

// shearScene is a still mob of the kind beside a survival player holding
// shears, wearing the named items in its body and saddle slots.
func shearScene(t *testing.T, kind, body, saddle string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := wolfScene(t, "Rider")
	id := srv.SpawnMob(kind, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameHorse(id)
	for slot, name := range map[string]string{"body": body, "saddle": saddle} {
		if name != "" {
			srv.SetMobEquipment(id, slot, protocol.ItemStack{ItemID: servertest.MustItemID(t, name), Count: 1})
		}
	}
	giveHand(t, srv, "Rider", "minecraft:shears", 1)
	return srv, c, id
}

// TestShearsTakeHorseArmourBeforeTheSaddle: the body slot goes first.
// vanilla: Mob.attemptToShearEquipment walks EquipmentSlot.VALUES
func TestShearsTakeHorseArmourBeforeTheSaddle(t *testing.T) {
	t.Parallel()
	srv, c, id := shearScene(t, "minecraft:horse", "minecraft:iron_horse_armor", "minecraft:saddle")
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if !srv.MobEquipment(id, "body").Empty() {
		t.Error("armour still on the horse")
	}
	if srv.MobEquipment(id, "saddle").Empty() {
		t.Error("the saddle came off with the armour")
	}
	if !seen.heard("minecraft:item.horse_armor.unequip") {
		t.Errorf("no armour unequip sound in %v", seen.sounds)
	}
}

// vanilla: Mob.attemptToShearEquipment checks slots in declaration order.
func TestShearsCheckEveryEquipmentSlot(t *testing.T) {
	t.Parallel()
	slots := []struct{ name, commandSlot, stack string }{
		{"mainhand", "weapon.mainhand", "saddle"},
		{"offhand", "weapon.offhand", "iron_horse_armor"},
		{"feet", "armor.feet", `iron_boots[equippable={slot:"feet",can_be_sheared:true}]`},
		{"legs", "armor.legs", `iron_leggings[equippable={slot:"legs",can_be_sheared:true}]`},
		{"chest", "armor.chest", `iron_chestplate[equippable={slot:"chest",can_be_sheared:true}]`},
		{"head", "armor.head", `iron_helmet[equippable={slot:"head",can_be_sheared:true}]`},
		{"body", "armor.body", "iron_horse_armor"},
	}
	for i, first := range slots[:len(slots)-1] {
		t.Run(first.name, func(t *testing.T) {
			const name = "Shearer"
			srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000)
			c.MoveToGround(0.5, 64, 0.5, true)
			id := srv.SpawnMob("minecraft:zombie", 1.5, 64, 0.5)
			if !srv.SetMobNoAI(id) {
				t.Fatal("the zombie did not spawn")
			}
			giveHand(t, srv, name, "minecraft:shears", 1)
			later := slots[i+1]
			selector, _ := srv.MobUUID(id)
			for _, piece := range slots[i : i+2] {
				c.ChatCommand("item replace entity " + selector + " " + piece.commandSlot + " with " + piece.stack)
			}
			firstItem, _, _ := strings.Cut(first.stack, "[")
			laterItem, _, _ := strings.Cut(later.stack, "[")
			checkSlots := func(wantFirst string) {
				t.Helper()
				equipment, _ := nbt.Get[nbt.Compound](entityCompound(t, c, selector), "equipment")
				for _, slot := range []struct{ name, want string }{{first.name, wantFirst}, {later.name, "minecraft:" + laterItem}} {
					stack, _ := nbt.Get[nbt.Compound](equipment, slot.name)
					if got, _ := nbt.Get[nbt.String](stack, "id"); string(got) != slot.want {
						t.Fatalf("%s held %q, want %q", slot.name, got, slot.want)
					}
				}
			}
			checkSlots("minecraft:" + firstItem)

			c.UseOn(id)
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			checkSlots("")
			if drops := srv.ItemsInDimension("minecraft:overworld"); len(drops) != 1 || drops[0].Item != "minecraft:"+firstItem || drops[0].Count != 1 {
				t.Errorf("drops = %v, want one %s", drops, firstItem)
			}
			if got := srv.InventorySlot(name, player.HotbarSlot); got.Damage != 1 {
				t.Errorf("shears damage = %d, want 1", got.Damage)
			}
		})
	}
}
