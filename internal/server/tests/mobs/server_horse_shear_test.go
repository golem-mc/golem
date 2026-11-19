package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// TestASneakingShearClickLeavesTheSaddle: with the sneak key held the shears
// take nothing and do not wear.
// vanilla: Mob.attemptToShearEquipment
func TestASneakingShearClickLeavesTheSaddle(t *testing.T) {
	t.Parallel()
	srv, c, id := saddledHorse(t)
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.1, 0.9, 0.2)
		b.PutBool(true)
	})
	watchWolf(t, c, srv, id, 2)
	if srv.MobEquipment(id, "saddle").Empty() {
		t.Error("a sneaking click sheared the saddle")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Damage != 0 {
		t.Errorf("shears damage = %d, want 0", got.Damage)
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

// TestShearsOnASaddledPigTakeTheSaddle: the saddle drops and the player is
// not seated.
// vanilla: Entity.interact, Mob.attemptToShearEquipment
func TestShearsOnASaddledPigTakeTheSaddle(t *testing.T) {
	t.Parallel()
	srv, c, id := shearScene(t, "minecraft:pig", "", "minecraft:saddle")
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if !srv.MobEquipment(id, "saddle").Empty() {
		t.Error("saddle still on the pig")
	}
	if !seen.heard("minecraft:item.saddle.unequip") {
		t.Errorf("no unequip sound in %v", seen.sounds)
	}
	if len(srv.MobPassengerIDs(id)) != 0 {
		t.Error("the player was seated on the pig")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("shears damage = %d, want 1", got.Damage)
	}
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

// TestShearsTakeALlamaCarpet: the carpet drops with the llama carpet sound.
// vanilla: Mob.attemptToShearEquipment
func TestShearsTakeALlamaCarpet(t *testing.T) {
	t.Parallel()
	srv, c, id := shearScene(t, "minecraft:llama", "minecraft:red_carpet", "")
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if !srv.MobEquipment(id, "body").Empty() {
		t.Error("carpet still on the llama")
	}
	if !seen.heard("minecraft:item.llama_carpet.unequip") {
		t.Errorf("no carpet unequip sound in %v", seen.sounds)
	}
}
