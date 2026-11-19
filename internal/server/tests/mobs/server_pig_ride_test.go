package mobs

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
