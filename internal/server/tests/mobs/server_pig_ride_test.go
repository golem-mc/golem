package mobs

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"testing"
)

const pigSaddleTag = `{NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`

func pigTypeID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:pig")
	if !ok {
		t.Fatal("no pig entity type")
	}
	return id
}

func pigSteer(t *testing.T, held string) (*servertest.Client, *server.Server, int32) {
	t.Helper()
	srv, c := striderWorld(t)
	if held != "" {
		srv.GiveSlot("Rider", player.HotbarSlot, servertest.ItemStack(t, held, 1))
		c.Settle()
	}
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 " + pigSaddleTag)
	spawn := c.ExpectMobSpawn(pigTypeID(t))
	c.UseOn(spawn.EntityID)
	c.Settle()
	return c, srv, spawn.EntityID
}

func TestASaddledPigFollowsARiderWithACarrotStick(t *testing.T) {
	t.Parallel()
	c, srv, id := pigSteer(t, "minecraft:carrot_on_a_stick")
	if got := srv.MobVehicleID(id); got != 0 {
		t.Fatalf("pig has vehicle %d", got)
	}
	if ids := srv.MobPassengerIDs(id); len(ids) != 1 {
		t.Fatalf("passengers %v, want the rider alone", ids)
	}
	for _, step := range []struct {
		x, z float64
		yaw  float32
	}{
		{7.5, 5.5, -90}, {7.5, 7.5, 0}, {5.5, 7.5, 90}, {5.5, 5.5, -180},
	} {
		moveVehicle(c, step.x, 64, step.z, step.yaw)
		m, ok := mobWithID(srv, id)
		if !ok || m.X != step.x || m.Z != step.z || m.Yaw != step.yaw || m.HeadYaw != step.yaw {
			t.Fatalf("rider report %+v: pig %+v", step, m)
		}
	}
}

func TestAPigIgnoresARiderWithoutACarrotStick(t *testing.T) {
	t.Parallel()
	c, srv, id := pigSteer(t, "")
	if ids := srv.MobPassengerIDs(id); len(ids) != 1 {
		t.Fatalf("passengers %v, want the rider alone", ids)
	}
	moveVehicle(c, 7.5, 64, 5.5, -90)
	if x, _, _, _ := srv.MobPos(id); x != 5.5 {
		t.Errorf("pig at x=%v, want it unmoved at 5.5 without the stick", x)
	}
}

func TestSaddlingAPigFitsAndSpendsTheSaddle(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	saddle, _ := registry.ItemID("minecraft:saddle")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: saddle, Count: 1})
	c.Settle()
	id := srv.SpawnMob("minecraft:pig", 5.5, 64, 5.5)
	srv.TickMobs(1)
	spawn := c.ExpectMobSpawn(pigTypeID(t))
	if spawn.EntityID != id {
		t.Fatalf("spawned %d, want %d", spawn.EntityID, id)
	}
	c.UseOn(id)
	told, sounded := false, false
	record := func(packet int32, data []byte) {
		if packet == protocol.IDPlaySoundEffect {
			var sound servertest.SoundSee
			sound.ReadSoundSee(t, data)
			sounded = sounded || sound.Sound == servertest.ConduitSoundID(t, "minecraft:entity.pig.saddle")
		}
		if packet != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id && r.ReadU8() == 7 {
			told = true
		}
	}
	c.SettleAndCollect(record)
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if !sounded {
		t.Error("no pig saddle sound")
	}
	if !told {
		t.Error("the saddle was not told in equipment slot 7")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 0 {
		t.Errorf("%d saddles left in hand, want 0", got.Count)
	}
}

func TestTheCarrotStickBoostsThePigOnceAtATime(t *testing.T) {
	t.Parallel()
	c, srv, id := pigSteer(t, "minecraft:carrot_on_a_stick")
	var boost int32
	record := striderBoostTime(c, id, &boost)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if boost < 140 || boost > 980 {
		t.Fatalf("boost length %d, want 140 to 980", boost)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 7 {
		t.Fatalf("stick wear %d after one use, want 7", got)
	}
	if got := srv.StatValue("Rider", registry.StatUsed, "minecraft:carrot_on_a_stick"); got != 0 {
		t.Errorf("boosting use counted %d, want 0", got)
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 7 {
		t.Errorf("stick wear %d after a use during the boost, want 7", got)
	}
	if got := srv.StatValue("Rider", registry.StatUsed, "minecraft:carrot_on_a_stick"); got != 1 {
		t.Errorf("failed boost counted %d, want 1", got)
	}
}

func TestABrokenCarrotStickBecomesAFishingRod(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	stick, _ := registry.ItemID("minecraft:carrot_on_a_stick")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: stick, Count: 1, Damage: 24, CustomName: &protocol.Text{Text: "Goad"}})
	c.Settle()
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 " + pigSaddleTag)
	spawn := c.ExpectMobSpawn(pigTypeID(t))
	c.UseOn(spawn.EntityID)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	rod, _ := registry.ItemID("minecraft:fishing_rod")
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.ItemID != rod || got.Count != 1 {
		t.Errorf("hand holds item %d x%d, want one fishing rod %d", got.ItemID, got.Count, rod)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.CustomName == nil || got.CustomName.Text != "Goad" {
		t.Errorf("the rod lost the stick's custom name: %v", got.CustomName)
	}
}

func TestABabyPigRefusesASaddle(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	srv.GiveSlot("Rider", player.HotbarSlot, servertest.ItemStack(t, "minecraft:saddle", 1))
	c.Settle()
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 {Age:-24000,NoAI:1b}")
	id := c.ExpectMobSpawn(pigTypeID(t)).EntityID
	c.UseOn(id)
	c.Settle()
	if !srv.MobEquipment(id, "saddle").Empty() {
		t.Fatal("baby took saddle")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 1 {
		t.Fatalf("hand = %+v", got)
	}
}

func TestAPigMountsUnlessThePlayerSneaks(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 " + pigSaddleTag)
	id := c.ExpectMobSpawn(pigTypeID(t)).EntityID
	c.SendSneak(true)
	c.Settle()
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.1, 0.9, 0.2)
		b.PutBool(true)
	})
	c.Settle()
	if ids := srv.MobPassengerIDs(id); len(ids) != 0 {
		t.Fatalf("sneaking mounted: %v", ids)
	}
	c.SendSneak(false)
	c.Settle()
	c.UseOn(id)
	passengers := c.ExpectPlay(protocol.IDPlaySetPassengers)
	r := protocol.NewReader(passengers)
	if got, count := r.ReadVarInt(), r.ReadVarInt(); got != id || count != 1 {
		t.Fatalf("passenger packet: id %d count %d", got, count)
	}
	c.Settle()
	if ids := srv.MobPassengerIDs(id); len(ids) != 1 {
		t.Fatalf("passengers = %v", ids)
	}
}

func TestASaddledPigDropsItsSaddle(t *testing.T) {
	t.Parallel()
	c, srv, id := pigSteer(t, "")
	c.SendSneak(true)
	c.Settle()
	if !srv.KillMob(id) {
		t.Fatal("pig did not die")
	}
	srv.TickMobs(21)
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		if registry.Item(item.Stack.ItemID) == registry.ItemSaddle {
			return
		}
	}
	t.Fatal("pig dropped no saddle")
}

func TestAPigsSpeedIsSentToItsRider(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 " + pigSaddleTag)
	spawn := c.ExpectMobSpawn(pigTypeID(t))
	id := spawn.EntityID
	if spawn.SpeedAttribute != 0.25 {
		t.Fatalf("pig speed = %v, want 0.25", spawn.SpeedAttribute)
	}
	c.UseOn(id)
	c.Settle()
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:movement_speed base set 0.5")
	c.Settle()
	srv.TickMobs(1)
	told := false
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet != protocol.IDPlayUpdateAttributes {
			return
		}
		got, attrs := servertest.ReadAttributes(t, data)
		if got != id {
			return
		}
		speed, ok := attrs[protocol.AttributeMovementSpeed]
		if ok {
			told = true
			if speed.Base != 0.5 {
				t.Errorf("pig speed = %+v", speed)
			}
		}
	})
	if !told {
		t.Fatal("pig movement speed not sent")
	}
}

func TestAPigsFoodFeedsInsteadOfMounting(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	srv.GiveSlot("Rider", player.HotbarSlot, servertest.ItemStack(t, "minecraft:carrot", 2))
	c.Settle()
	c.ChatCommand("summon minecraft:pig 5.5 64 5.5 " + pigSaddleTag)
	id := c.ExpectMobSpawn(pigTypeID(t)).EntityID
	c.UseOn(id)
	c.Settle()
	if ids := srv.MobPassengerIDs(id); len(ids) != 0 {
		t.Fatalf("food mounted rider: %v", ids)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Count; got != 1 {
		t.Fatalf("carrots left = %d, want 1", got)
	}
}

func TestAPigNeedsItsOwnStickInEitherHand(t *testing.T) {
	t.Parallel()
	c, srv, id := pigSteer(t, "minecraft:warped_fungus_on_a_stick")
	moveVehicle(c, 7.5, 64, 5.5, -90)
	if x, _, _, _ := srv.MobPos(id); x != 5.5 {
		t.Fatalf("fungus stick steered pig to %v", x)
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 0 {
		t.Fatalf("wrong stick wore by %d", got)
	}
	srv.GiveSlot("Rider", player.OffHandSlot, servertest.ItemStack(t, "minecraft:carrot_on_a_stick", 1))
	c.Settle()
	moveVehicle(c, 7.5, 64, 5.5, -90)
	if x, _, _, _ := srv.MobPos(id); x != 7.5 {
		t.Fatalf("offhand carrot stick left pig at %v", x)
	}
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	if got := srv.InventorySlot("Rider", player.OffHandSlot).Damage; got != 7 {
		t.Fatalf("offhand wear = %d, want 7", got)
	}
}
